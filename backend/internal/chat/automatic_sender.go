// 文件作用说明：为自动欢迎和关键词消息提供安全的客服展示身份，保留原始系统发送者和统计来源。
package chat

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// automaticAgentObjectSQL 仅投影公开客服姓名、成员编号和受控头像路径，禁止返回邮箱等账户资料。
var automaticAgentObjectSQL = `jsonb_build_object('id',tm.id,'name',coalesce(nullif(tm.display_name,''),u.display_name),'avatar_url',` + platform.AvatarURLSQL("tm") + `)`

// automaticAgentForMessageSQL 优先读取发送时冻结的身份，旧消息按发送时接待记录回补，接单前消息关联首位正式接待客服。
var automaticAgentForMessageSQL = `CASE WHEN m.sender_kind='system' AND m.source IN('welcome','keyword') THEN coalesce((SELECT nullif(e.payload->'automation_agent','null'::jsonb) FROM conversation_events e WHERE e.conversation_id=m.conversation_id AND e.tenant_id=m.tenant_id AND e.seq=m.event_seq),(SELECT ` + automaticAgentObjectSQL + ` FROM conversation_assignments ca JOIN tenant_memberships tm ON tm.id=ca.membership_id AND tm.tenant_id=ca.tenant_id JOIN users u ON u.id=tm.user_id WHERE ca.conversation_id=m.conversation_id AND ca.tenant_id=m.tenant_id AND ca.accepted_at IS NOT NULL ORDER BY (ca.accepted_at<=m.created_at) DESC,CASE WHEN ca.accepted_at<=m.created_at THEN ca.accepted_at END DESC,ca.accepted_at,ca.id LIMIT 1)) ELSE NULL END`

// firstConversationAgentSQL 为尚未接单就已显示的自动消息提供稳定首接身份，之后转接不将这些消息改标为其他客服。
var firstConversationAgentSQL = `(SELECT ` + automaticAgentObjectSQL + ` FROM conversation_assignments ca JOIN tenant_memberships tm ON tm.id=ca.membership_id AND tm.tenant_id=ca.tenant_id JOIN users u ON u.id=tm.user_id WHERE ca.conversation_id=c.id AND ca.tenant_id=c.tenant_id AND ca.accepted_at IS NOT NULL ORDER BY ca.accepted_at,ca.id LIMIT 1)`

// automaticAgentForConversation 在发送事务中冻结当前正式接待客服的公开身份，未接单时保持空值供首接后回补。
func automaticAgentForConversation(ctx context.Context, tx pgx.Tx, conversation string) (map[string]any, error) {
	item, err := txJSON(ctx, tx, `SELECT `+automaticAgentObjectSQL+` FROM conversations c JOIN conversation_assignments ca ON ca.id=c.current_assignment_id AND ca.tenant_id=c.tenant_id JOIN tenant_memberships tm ON tm.id=ca.membership_id AND tm.tenant_id=ca.tenant_id JOIN users u ON u.id=tm.user_id WHERE c.id=$1 AND ca.accepted_at IS NOT NULL`, conversation)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return item, err
}

// enrichAutomaticAgents 为旧持久事件和未接单时创建的事件回补展示身份，仍逐条验证当前消息可见权限。
func enrichAutomaticAgents(ctx context.Context, tx pgx.Tx, a actor, items []map[string]any) error {
	ids := []string{}
	for _, item := range items {
		id, _ := item["id"].(string)
		if item["sender_kind"] == "system" && (item["source"] == "welcome" || item["source"] == "keyword") && item["automation_agent"] == nil && uuidPattern.MatchString(id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT m.id::text,`+automaticAgentForMessageSQL+` FROM messages m WHERE m.id=ANY($1::uuid[]) AND chat_message_visible(NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,m.id)`, ids, a.MemberID, a.ContactID, isAdmin(a))
	if err != nil {
		return err
	}
	defer rows.Close()
	agents := map[string]map[string]any{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		if len(raw) == 0 {
			continue
		}
		var agent map[string]any
		if err := json.Unmarshal(raw, &agent); err != nil {
			return err
		}
		agents[id] = agent
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		if id, ok := item["id"].(string); ok && agents[id] != nil {
			item["automation_agent"] = agents[id]
		}
	}
	return nil
}
