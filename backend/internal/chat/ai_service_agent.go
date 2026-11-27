// 文件作用说明：提供租户固定AI接待成员的资料编辑与既有机器人配置匹配，不创建登录账号或人工坐席。
package chat

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"supportdesk/backend/internal/platform"
)

// aiServiceAgent 返回默认AI成员并允许管理员编辑外观、启停和优先级；模型知识等继续使用现有配置。
func (s *Service) aiServiceAgent(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var in struct {
		Name        string `json:"name"`
		Avatar      string `json:"avatar_ref"`
		Enabled     bool   `json:"enabled"`
		Priority    int    `json:"priority"`
		Description string `json:"description"`
		Version     int64  `json:"version"`
	}
	if r.Method == "PUT" {
		if err := platform.Decode(w, r, &in); err != nil {
			replyError(w, r, err)
			return
		}
		if strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 60 || len([]rune(in.Description)) > 500 || in.Priority < 1 || in.Priority > 100 || platform.AvatarURL(in.Avatar) == "" {
			replyError(w, r, platform.Fail(400, "invalid_ai_agent", "请检查AI成员的名称、头像和优先级"))
			return
		}
	}
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if r.Method == "PUT" {
			if in.Avatar != "goodchat" {
				if err := validateAvatarRefs(ctx, tx, []string{in.Avatar}); err != nil {
					return err
				}
			}
			tag, err := tx.Exec(ctx, `UPDATE ai_service_agents SET name=$1,avatar_ref=$2,enabled=$3,priority=$4,description=$5,version=version+1,updated_at=now() WHERE version=$6`, strings.TrimSpace(in.Name), in.Avatar, in.Enabled, in.Priority, strings.TrimSpace(in.Description), in.Version)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return platform.Fail(409, "version_conflict", "内容已更新，请刷新后重试")
			}
			if !in.Enabled {
				rows, err := tx.Query(ctx, "UPDATE conversations SET ai_paused=true,ai_epoch=ai_epoch+1,context=context-'ai_receiving'-'ai_thinking' WHERE status='queued' AND context->>'ai_receiving'='true' AND NOT ai_paused RETURNING id::text")
				if err != nil {
					return err
				}
				ids := []string{}
				for rows.Next() {
					var id string
					if err = rows.Scan(&id); err != nil {
						rows.Close()
						return err
					}
					ids = append(ids, id)
				}
				rows.Close()
				if err = rows.Err(); err != nil {
					return err
				}
				for _, id := range ids {
					if _, err = s.emit(ctx, tx, a.TenantID, id, "conversation.updated", "public", map[string]any{"id": id, "ai_paused": true, "ai_reason": "ai_disabled"}); err != nil {
						return err
					}
				}
			}
		}
		var err error
		result, err = txJSON(ctx, tx, `SELECT to_jsonb(a)||jsonb_build_object('kind','ai','can_delete',false,'avatar_url',`+platform.AvatarURLSQL("a")+`,'configured',EXISTS(SELECT 1 FROM ai_bots b JOIN tenant_memberships m ON m.id=b.owner_membership_id AND NOT m.disabled AND m.removed_at IS NULL WHERE b.id=coalesce((SELECT default_bot_id FROM ai_tenant_settings), (SELECT b2.id FROM ai_bots b2 JOIN tenant_memberships m ON m.id=b2.owner_membership_id WHERE NOT m.disabled AND m.removed_at IS NULL ORDER BY CASE m.role WHEN 'owner' THEN 0 ELSE 1 END,b2.id LIMIT 1)) AND b.mode='auto' AND b.model_id IS NOT NULL)) FROM ai_service_agents a`)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// aiReceptionBot 以管理员显式选择AI授予当前入口接待资格；旧默认机器人仍继承原组池和人工坐席限制，转人工后不接回AI。
func aiReceptionBot(ctx context.Context, tx pgx.Tx, conversation string, first bool) (string, error) {
	var id string
	query := `SELECT b.id::text FROM conversations c JOIN access_links l ON l.id=c.created_entry_id AND l.tenant_id=c.tenant_id
 LEFT JOIN ai_tenant_settings t ON t.tenant_id=c.tenant_id JOIN ai_service_agents a ON a.tenant_id=c.tenant_id
 JOIN ai_bots b ON b.id=coalesce(l.ai_bot_id,CASE WHEN l.ai_agent_id IS NOT NULL THEN coalesce(t.default_bot_id,(SELECT b2.id FROM ai_bots b2 JOIN tenant_memberships m2 ON m2.id=b2.owner_membership_id WHERE b2.tenant_id=c.tenant_id AND NOT m2.disabled AND m2.removed_at IS NULL ORDER BY CASE m2.role WHEN 'owner' THEN 0 ELSE 1 END,b2.id LIMIT 1)) ELSE t.default_bot_id END)
 JOIN tenant_memberships m ON m.id=b.owner_membership_id AND m.tenant_id=c.tenant_id
 WHERE c.id=$1 AND NOT c.ai_paused AND c.status='queued' AND a.enabled AND (l.ai_agent_id IS NULL OR l.ai_agent_id=a.id)
 AND (NOT $2 OR l.ai_agent_id IS NOT NULL OR l.ai_bot_id IS NOT NULL OR t.reception_mode='ai_first') AND b.mode='auto' AND NOT m.disabled AND m.removed_at IS NULL AND (l.ai_agent_id IS NOT NULL OR l.ai_bot_id IS NOT NULL OR m.can_serve)
 AND (l.ai_agent_id IS NOT NULL OR l.ai_bot_id IS NOT NULL OR (c.desired_agent_id IS NULL OR c.desired_agent_id=b.owner_membership_id))
 AND (l.ai_agent_id IS NOT NULL OR l.ai_bot_id IS NOT NULL OR ` + aiBotGroupScopeSQL + `)`
	err := tx.QueryRow(ctx, query, conversation, first).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}
