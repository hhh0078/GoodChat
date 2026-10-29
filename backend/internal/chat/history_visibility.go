// 文件作用说明：统一读取历史策略与可见范围指纹，批量移除不可见引用，避免API和实时事件重新泄漏旧正文。
package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// historyPolicy 从当前租户配置取得窗口，零表示永久优先历史客服。
func historyPolicy(ctx context.Context, tx pgx.Tx, tenant string) (int, bool, error) {
	settings, err := ensureSettings(ctx, tx, tenant)
	if err != nil {
		return 0, true, err
	}
	minutes := 0
	if value, ok := settings["history_agent_minutes"].(float64); ok && value >= 0 && value <= 525600 && value == float64(int(value)) {
		minutes = int(value)
	}
	shared := true
	if value, ok := settings["share_previous_chats"].(bool); ok {
		shared = value
	}
	return minutes, shared, nil
}

// historyScope 返回经过认证身份和当前接待段共同确定的缓存范围指纹。
func historyScope(ctx context.Context, tx pgx.Tx, a actor, id string) (string, error) {
	var scope string
	err := tx.QueryRow(ctx, "SELECT chat_history_scope(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3,$4)", a.MemberID, a.ContactID, isAdmin(a), id).Scan(&scope)
	return scope, err
}

// messageVisible 检查单条消息，用于引用、撤回与附件边界，未知编号按不可见处理。
func messageVisible(ctx context.Context, tx pgx.Tx, a actor, id string) (bool, error) {
	if !uuidPattern.MatchString(id) {
		return false, nil
	}
	var visible bool
	err := tx.QueryRow(ctx, "SELECT chat_message_visible(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3,$4)", a.MemberID, a.ContactID, isAdmin(a), id).Scan(&visible)
	return visible, err
}

// redactMessageReplies 按当前消息状态重建可见引用，移除不可见内容并同步撤回标志，历史事件副本不能保留旧正文。
func redactMessageReplies(ctx context.Context, tx pgx.Tx, a actor, items []map[string]any) error {
	if err := enrichAutomaticAgents(ctx, tx, a, items); err != nil {
		return err
	}
	ids := []string{}
	for _, item := range items {
		if reply, ok := item["reply"].(map[string]any); ok {
			if id, ok := reply["id"].(string); ok && uuidPattern.MatchString(id) {
				ids = append(ids, id)
			} else {
				item["reply"] = nil
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, "SELECT id::text,CASE WHEN withdrawn_at IS NULL THEN left(content,200) ELSE '' END,withdrawn_at IS NOT NULL FROM messages WHERE id=ANY($1::uuid[]) AND chat_message_visible(NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,id)", ids, a.MemberID, a.ContactID, isAdmin(a))
	if err != nil {
		return err
	}
	allowed := map[string]map[string]any{}
	for rows.Next() {
		var id, content string
		var withdrawn bool
		if err := rows.Scan(&id, &content, &withdrawn); err != nil {
			rows.Close()
			return err
		}
		allowed[id] = map[string]any{"id": id, "content": content, "withdrawn": withdrawn}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if reply, ok := item["reply"].(map[string]any); ok {
			id, _ := reply["id"].(string)
			if canonical := allowed[id]; canonical != nil {
				item["reply"] = canonical
			} else {
				item["reply"] = nil
			}
		}
	}
	return nil
}

// decodeHistoryCursor 在验证原签名后核对当前历史范围，范围改变必须重建快照。
func (s *Service) decodeHistoryCursor(a actor, id, text, scope string) (int64, error) {
	seq, err := s.decodeCursor(a, id, text)
	if err != nil || text == "" {
		return seq, err
	}
	parts := strings.Split(text, ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, platform.Fail(409, "resync_required", "历史可见范围已变化，请重新读取快照")
	}
	var value syncCursor
	if json.Unmarshal(raw, &value) != nil || value.Scope != scope {
		return 0, platform.Fail(409, "resync_required", "历史可见范围已变化，请重新读取快照")
	}
	return seq, nil
}
