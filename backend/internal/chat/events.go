// 文件作用说明：提交事务事件与outbox，提供绑定授权范围的可靠恢复游标。
package chat

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// emit 在同一事务中分配会话序号、保存事件并创建可靠投递任务。
func (s *Service) emit(ctx context.Context, tx pgx.Tx, tenant, conversation, kind, audience string, payload map[string]any) (int64, error) {
	var seq int64
	if err := tx.QueryRow(ctx, "UPDATE conversations SET next_event_seq=next_event_seq+1,updated_at=now() WHERE id=$1 RETURNING next_event_seq", conversation).Scan(&seq); err != nil {
		return 0, err
	}
	payload["seq"] = strconv.FormatInt(seq, 10)
	payload["conversation_id"] = conversation
	id := newID()
	if _, err := tx.Exec(ctx, "INSERT INTO conversation_events(id,tenant_id,conversation_id,seq,event_type,audience,payload) VALUES($1,$2,$3,$4,$5,$6,$7)", id, tenant, conversation, seq, kind, audience, jsonValue(payload)); err != nil {
		return 0, err
	}
	_, err := tx.Exec(ctx, "INSERT INTO outbox_jobs(tenant_id,event_id) VALUES($1,$2)", tenant, id)
	return seq, err
}

// syncCursor 将恢复位置绑定会话和当前参与者授权范围。
type syncCursor struct {
	Conversation string `json:"c"`
	Actor        string `json:"a"`
	Seq          int64  `json:"s"`
	Scope        string `json:"v,omitempty"`
}

// encodeCursor 签名游标，避免客户端自行拼接越过未完成扫描的位置。
func (s *Service) encodeCursor(a actor, conversation string, seq int64) string {
	return s.encodeScopedCursor(a, conversation, seq, "")
}

// encodeScopedCursor 将历史分享策略与接待段指纹纳入服务端签名，禁止旧范围缓存继续推进。
func (s *Service) encodeScopedCursor(a actor, conversation string, seq int64, scope string) string {
	raw := jsonValue(syncCursor{Conversation: conversation, Actor: a.TenantID + actorKind(a) + actorID(a) + a.Role, Seq: seq, Scope: scope})
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// decodeCursor 验证游标签名和授权范围，重启或变更身份时要求重新同步。
func (s *Service) decodeCursor(a actor, conversation, text string) (int64, error) {
	if text == "" {
		return 0, nil
	}
	parts := strings.Split(text, ".")
	if len(parts) != 2 {
		return 0, platform.Fail(409, "resync_required", "请重新读取会话快照")
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	sig, f := base64.RawURLEncoding.DecodeString(parts[1])
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write(raw)
	var c syncCursor
	if e != nil || f != nil || !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(raw, &c) != nil || c.Conversation != conversation || c.Actor != a.TenantID+actorKind(a)+actorID(a)+a.Role || c.Seq < 0 {
		return 0, platform.Fail(409, "resync_required", "同步授权范围已变化，请重新读取快照")
	}
	return c.Seq, nil
}

// events 扫描固定序号范围，游标可以安全跨过对访客不可见的内部事件。
func (s *Service) events(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	id := r.PathValue("id")
	var items []map[string]any
	var watermark, end int64
	scope := ""
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := canReadConversation(ctx, tx, a, id); err != nil {
			return err
		}
		var err error
		scope, err = historyScope(ctx, tx, a, id)
		if err != nil {
			return err
		}
		after, err := s.decodeHistoryCursor(a, id, r.URL.Query().Get("after_cursor"), scope)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT next_event_seq FROM conversations WHERE id=$1", id).Scan(&watermark); err != nil {
			return err
		}
		if after > watermark {
			return platform.Fail(409, "resync_required", "同步位置超出当前会话范围")
		}
		end = after + 200
		if end > watermark {
			end = watermark
		}
		rows, err := tx.Query(ctx, "SELECT to_jsonb(e)||jsonb_build_object('seq',e.seq::text,'protocol_version',1) FROM conversation_events e WHERE conversation_id=$1 AND seq>$2 AND seq<=$3 AND chat_event_visible(NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,e.id) ORDER BY seq", id, after, end, a.MemberID, a.ContactID, isAdmin(a))
		if err != nil {
			return err
		}
		items, err = jsonRows(rows)
		if err != nil {
			return err
		}
		messages := []map[string]any{}
		for _, event := range items {
			if payload, ok := event["payload"].(map[string]any); ok {
				messages = append(messages, payload)
			}
		}
		return redactMessageReplies(ctx, tx, a, messages)
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items, "next_cursor": s.encodeScopedCursor(a, id, end, scope), "watermark": strconv.FormatInt(watermark, 10), "has_more": end < watermark, "history_scope": scope})
}
