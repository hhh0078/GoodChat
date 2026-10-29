// 文件作用说明：按参与者权限提供咨询列表、详情和聊天记录筛选。
package chat

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

var conversationProjection = "to_jsonb(c)||jsonb_build_object('next_event_seq',c.next_event_seq::text,'version',c.version::text,'last_message_seq',c.last_message_seq::text,'contact_online',u.online_until>statement_timestamp(),'contact',jsonb_build_object('id',u.id,'display_name',u.display_name,'phone',u.phone,'avatar_url',u.avatar_url),'agent_name',COALESCE(a.display_name,''),'agent_avatar_url'," + platform.AvatarURLSQL("m") + ")"

const conversationJoins = " FROM conversations c JOIN contacts u ON u.id=c.contact_id AND u.tenant_id=c.tenant_id LEFT JOIN tenant_memberships m ON m.id=c.current_agent_id AND m.tenant_id=c.tenant_id LEFT JOIN users a ON a.id=m.user_id "

// conversationJSON 返回带字符串序号和展示资料的当前会话快照。
func conversationJSON(ctx context.Context, tx pgx.Tx, id string) (map[string]any, error) {
	return txJSON(ctx, tx, "SELECT "+conversationProjection+conversationJoins+" WHERE c.id=$1", id)
}

// canReadConversation 对数据库与实时连接统一执行当前可见范围检查。
func canReadConversation(ctx context.Context, tx pgx.Tx, a actor, id string) error {
	if !uuidPattern.MatchString(id) {
		return platform.Fail(400, "invalid_id", "会话编号不正确")
	}
	if a.ContactID != "" {
		if err := blocked(ctx, tx, a.ContactID); err != nil {
			return err
		}
	} else {
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
	}
	var ok bool
	err := tx.QueryRow(ctx, "SELECT chat_conversation_visible(NULLIF($4,'')::uuid,NULLIF($2,'')::uuid,$3,$1)", id, a.ContactID, isAdmin(a), a.MemberID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return platform.Fail(404, "conversation_not_found", "咨询不存在或当前无权查看")
	}
	return nil
}

// conversations 列出当前身份可见咨询；访客POST继续使用统一入口初始化协议。
func (s *Service) conversations(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	if r.Method == http.MethodPost {
		if a.ContactID == "" {
			replyError(w, r, platform.Fail(400, "entry_required", "请通过访客入口创建咨询"))
			return
		}
		s.visitorSession(w, r)
		return
	}
	s.listConversations(w, r, a)
}

// chatRecords 提供租户历史记录页面，管理员看授权租户，客服看本人接待记录。
func (s *Service) chatRecords(w http.ResponseWriter, r *http.Request) {
	s.listConversations(w, r, memberActor(r))
}

// listConversations 以持久数据库结果构建筛选列表，不用模拟统计。
func (s *Service) listConversations(w http.ResponseWriter, r *http.Request, a actor) {
	var items []map[string]any
	nextCursor := ""
	hasMore := false
	timezone := ""
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if a.ContactID == "" {
			if err := liveMember(r.Context(), tx, a); err != nil {
				return err
			}
		} else {
			if err := blocked(r.Context(), tx, a.ContactID); err != nil {
				return err
			}
		}
		args, zone, err := conversationFilterArgs(r.Context(), tx, r, a)
		if err != nil {
			return err
		}
		timezone = zone
		limit, from, to, scope, err := conversationPageOptions(r, a, timezone)
		if err != nil {
			return err
		}
		before, beforeID, err := s.decodeConversationCursor(r.URL.Query().Get("cursor"), scope)
		if err != nil {
			return err
		}
		args = append(args, from, to, before, beforeID, limit+1)
		// 未读按当前成员自己的已读游标计算，不能由管理员、其他客服或访客的读取代清。
		unread := `||jsonb_build_object('unread_count',(SELECT count(*) FROM messages unread WHERE unread.tenant_id=c.tenant_id AND unread.conversation_id=c.id AND unread.audience='public' AND unread.sender_kind='visitor' AND unread.withdrawn_at IS NULL AND chat_message_visible(NULLIF($3,'')::uuid,NULLIF($1,'')::uuid,$2,unread.id) AND unread.event_seq>coalesce((SELECT rc.last_read_seq FROM read_cursors rc WHERE rc.tenant_id=c.tenant_id AND rc.conversation_id=c.id AND rc.participant_id=NULLIF($3,'')::uuid AND rc.participant_kind='agent'),0)))`
		query := "SELECT " + conversationProjection + unread + conversationJoins + conversationWhere + conversationActivityWhere + conversationPageWhere + " ORDER BY " + conversationActivity + " DESC,c.id LIMIT $15"
		rows, err := tx.Query(r.Context(), query, args...)
		if err != nil {
			return err
		}
		items, err = jsonRows(rows)
		if err != nil {
			return err
		}
		hasMore = len(items) > limit
		if hasMore {
			items = items[:limit]
			nextCursor, err = s.encodeConversationCursor(items[len(items)-1], scope)
			if err != nil {
				return err
			}
		}
		return addPendingTransfers(r.Context(), tx, items, a)
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items, "has_more": hasMore, "next_cursor": nextCursor, "timezone": timezone})
}

// filterTime 将日期筛选按租户时区解释，完整时间则遵循其显式时区。
func filterTime(value, timezone string, end bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	v, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return &v, nil
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, platform.Fail(400, "invalid_timezone", "租户时区配置不正确")
	}
	v, err = time.ParseInLocation("2006-01-02", value, loc)
	if err != nil {
		return nil, platform.Fail(400, "invalid_date", "时间筛选需使用日期或带时区的ISO时间")
	}
	if end {
		v = v.AddDate(0, 0, 1)
	}
	return &v, nil
}

// conversationDetail 在返回快照前检查最新授权和封禁。
func (s *Service) conversationDetail(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := canReadConversation(r.Context(), tx, a, r.PathValue("id")); err != nil {
			return err
		}
		var err error
		result, err = conversationJSONForActor(r.Context(), tx, r.PathValue("id"), a)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}
