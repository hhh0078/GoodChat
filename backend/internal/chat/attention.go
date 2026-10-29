// 文件作用说明：从持久未读和当前接待归属原子领取重复提醒，覆盖无消息的新咨询且合并多标签重复播放。
package chat

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// registerAttention 注册当前客服的提醒领取接口，不提供租户管理员代领他人提醒的入口。
func (s *Service) registerAttention(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/agent/attention-reminders/claim", s.auth.RequireTenant(s.claimAttention))
}

// reminderSettings 读取已保存的租户提醒参数，对升级前缺失值使用相同出厂配置。
func reminderSettings(settings map[string]any) (int, string) {
	seconds := 10
	if value, ok := settings["repeat_unread_seconds"].(float64); ok && value >= 1 && value <= 3600 && value == float64(int(value)) {
		seconds = int(value)
	}
	sound, _ := settings["notification_sound_id"].(string)
	if !inStrings(sound, "gentle", "glass", "wood", "bell", "bubble") {
		sound = "gentle"
	}
	return seconds, sound
}

// attentionViewed 在已锁定咨询的已读事务中确认当前接待段，不替其他客服消除未查看状态。
func attentionViewed(ctx context.Context, tx pgx.Tx, a actor, conversation string) error {
	_, err := tx.Exec(ctx, `INSERT INTO conversation_attention_state(tenant_id,conversation_id,membership_id,viewed_assignment_id)
 SELECT tenant_id,id,$2,current_assignment_id FROM conversations WHERE id=$1 AND current_agent_id=$2
 ON CONFLICT(tenant_id,conversation_id,membership_id) DO UPDATE SET viewed_assignment_id=EXCLUDED.viewed_assignment_id,updated_at=now()`, conversation, a.MemberID)
	return err
}

// attentionRestoreViewed 仅凭当前接待段与本人已读事件的精确序号恢复旧查看元数据，不使用026隐私锚点或事务时间猜测。
func attentionRestoreViewed(ctx context.Context, tx pgx.Tx, a actor, conversation, assignment string, cursor int64) (bool, error) {
	if assignment == "" || cursor <= 0 {
		return false, nil
	}
	var proven bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM conversations c
 JOIN conversation_assignments ass ON ass.id=c.current_assignment_id AND ass.tenant_id=c.tenant_id
 JOIN read_cursors rc ON rc.conversation_id=c.id AND rc.tenant_id=c.tenant_id AND rc.participant_kind='agent' AND rc.participant_id=ass.membership_id
 JOIN messages viewed_message ON viewed_message.conversation_id=c.id AND viewed_message.tenant_id=c.tenant_id AND viewed_message.event_seq=rc.last_read_seq
 JOIN LATERAL(
 SELECT max(e.seq) seq FROM conversation_events e WHERE e.conversation_id=c.id AND e.tenant_id=c.tenant_id AND e.event_type='assignment.changed'
 AND e.payload->>'agent_id'=ass.membership_id::text
 AND (e.payload->>'assignment_id'=ass.id::text OR EXISTS(
 SELECT 1 FROM agent_capacity_holds h WHERE h.tenant_id=c.tenant_id AND h.conversation_id=c.id AND h.assignment_id=ass.id AND h.transfer_request_id::text=e.payload->>'transfer_request_id'))
 ) started ON started.seq IS NOT NULL
 WHERE c.id=$1 AND c.current_agent_id=$2::uuid AND ass.membership_id=$2::uuid AND ass.id=$3::uuid AND ass.released_at IS NULL
 AND (ass.expires_at IS NULL OR ass.expires_at>statement_timestamp()) AND rc.last_read_seq=$4 AND viewed_message.event_seq>started.seq
 AND EXISTS(SELECT 1 FROM conversation_events read_event WHERE read_event.conversation_id=c.id AND read_event.tenant_id=c.tenant_id AND read_event.event_type='read.updated'
 AND read_event.seq>viewed_message.event_seq AND read_event.payload->>'participant_kind'='agent'
 AND read_event.payload->>'participant_id'=ass.membership_id::text AND read_event.payload->>'last_read_seq'=rc.last_read_seq::text)
 )`, conversation, a.MemberID, assignment, cursor).Scan(&proven)
	if err != nil || !proven {
		return false, err
	}
	// 与显式查看共用幂等写入；只补viewed_assignment_id，不推进已读游标或产生新的read.updated。
	return true, attentionViewed(ctx, tx, a, conversation)
}

// attentionCandidate 只保存当前事务已锁定的本人接待咨询，不向客户端泄露其他成员的信息。
type attentionCandidate struct {
	id, assignment string
	assignedAt     time.Time
}

// claimAttention 以最早未读或未查看接待段计算期限，成功领取后同成员的其他标签须等待下一周期。
func (s *Service) claimAttention(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var input struct {
		Initial bool `json:"initial"`
	}
	if r.ContentLength != 0 {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
	}
	items := []map[string]any{}
	pendingCount := 0
	seconds, sound := 10, "gentle"
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		settings, err := ensureSettings(ctx, tx, a.TenantID)
		if err != nil {
			return err
		}
		seconds, sound = reminderSettings(settings)
		if !a.CanServe {
			return nil
		}
		var now time.Time
		if err := tx.QueryRow(ctx, "SELECT statement_timestamp()").Scan(&now); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT c.id::text,coalesce(c.current_assignment_id::text,''),coalesce(ass.accepted_at,ass.created_at,c.created_at)
 FROM conversations c LEFT JOIN conversation_assignments ass ON ass.id=c.current_assignment_id AND ass.tenant_id=c.tenant_id
 WHERE c.current_agent_id=$1 AND c.status IN('assigned','active') ORDER BY c.id LIMIT 100 FOR UPDATE OF c`, a.MemberID)
		if err != nil {
			return err
		}
		candidates := []attentionCandidate{}
		for rows.Next() {
			var candidate attentionCandidate
			if err := rows.Scan(&candidate.id, &candidate.assignment, &candidate.assignedAt); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, candidate)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, candidate := range candidates {
			var cursor int64
			err := tx.QueryRow(ctx, `SELECT last_read_seq FROM read_cursors WHERE conversation_id=$1 AND participant_kind='agent' AND participant_id=$2`, candidate.id, a.MemberID).Scan(&cursor)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			key, preview, kind := "", "新咨询尚未查看", "new"
			pendingSince := candidate.assignedAt
			var unread int64
			err = tx.QueryRow(ctx, `SELECT id::text,created_at,left(content,100),(SELECT count(*) FROM messages n WHERE n.conversation_id=$1 AND n.sender_kind='visitor' AND n.audience='public' AND n.withdrawn_at IS NULL AND n.event_seq>$2 AND chat_message_visible($3::uuid,NULL,$4,n.id))
 FROM messages WHERE conversation_id=$1 AND sender_kind='visitor' AND audience='public' AND withdrawn_at IS NULL AND event_seq>$2 AND chat_message_visible($3::uuid,NULL,$4,messages.id) ORDER BY event_seq LIMIT 1`, candidate.id, cursor, a.MemberID, isAdmin(a)).Scan(&key, &pendingSince, &preview, &unread)
			if errors.Is(err, pgx.ErrNoRows) {
				var viewed string
				err = tx.QueryRow(ctx, `SELECT coalesce(viewed_assignment_id::text,'') FROM conversation_attention_state WHERE conversation_id=$1 AND membership_id=$2`, candidate.id, a.MemberID).Scan(&viewed)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if err == nil && viewed == candidate.assignment {
					continue
				}
				restored, err := attentionRestoreViewed(ctx, tx, a, candidate.id, candidate.assignment, cursor)
				if err != nil {
					return err
				}
				if restored {
					continue
				}
				key = "assignment:" + candidate.assignment + ":" + candidate.id
			} else if err != nil {
				return err
			} else {
				key = "message:" + key
				kind = "unread"
				if preview == "" {
					preview = "访客发送了附件消息"
				}
			}
			pendingCount++
			if !input.Initial && now.Before(pendingSince.Add(time.Duration(seconds)*time.Second)) {
				continue
			}
			var claimed bool
			err = tx.QueryRow(ctx, `INSERT INTO conversation_attention_state(tenant_id,conversation_id,membership_id,reminder_key,last_reminded_at)
 VALUES($1,$2,$3,$4,statement_timestamp()) ON CONFLICT(tenant_id,conversation_id,membership_id) DO UPDATE
 SET reminder_key=EXCLUDED.reminder_key,last_reminded_at=statement_timestamp(),updated_at=now()
 WHERE (conversation_attention_state.reminder_key<>EXCLUDED.reminder_key AND $5) OR coalesce(conversation_attention_state.last_reminded_at,$7)<=statement_timestamp()-$6::double precision*interval '1 second'
 RETURNING true`, a.TenantID, candidate.id, a.MemberID, key, input.Initial, seconds, pendingSince).Scan(&claimed)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if claimed {
				items = append(items, map[string]any{"conversation_id": candidate.id, "reminder_key": key, "pending_since": pendingSince, "unread_count": unread, "kind": kind, "preview": preview})
			}
		}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items, "pending_count": pendingCount, "repeat_unread_seconds": seconds, "notification_sound_id": sound})
}
