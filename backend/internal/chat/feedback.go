// 文件作用说明：保存访客离线留言和真实咨询评价，并提供租户跟进列表。
package chat

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// createLead 以独立访客凭证提交留言，不把联系电话当成身份验证。
func (s *Service) createLead(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	if a.ContactID == "" {
		replyError(w, r, platform.Fail(403, "visitor_required", "请使用访客身份提交留言"))
		return
	}
	var in struct {
		EntryKey       string `json:"entry_key"`
		ConversationID string `json:"conversation_id"`
		ClientID       string `json:"client_request_id"`
		Name           string `json:"display_name"`
		Phone          string `json:"phone"`
		Content        string `json:"content"`
	}
	if err := platform.Decode(w, r, &in); err != nil {
		replyError(w, r, err)
		return
	}
	if !uuidPattern.MatchString(in.ClientID) || strings.TrimSpace(in.Content) == "" || len([]rune(in.Content)) > 5000 || len(in.Phone) > 40 || len([]rune(in.Name)) > 80 {
		returnError(w, r, "invalid_lead", "请填写有效留言及稳定请求编号")
		return
	}
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tenantActive(ctx, tx, a.TenantID); err != nil {
			return err
		}
		if err := blocked(ctx, tx, a.ContactID); err != nil {
			return err
		}
		if in.ConversationID != "" {
			if err := canReadConversation(ctx, tx, a, in.ConversationID); err != nil {
				return err
			}
		}
		entry, err := entryJSON(ctx, tx, in.EntryKey)
		if err != nil {
			return err
		}
		link := entry["id"].(string)
		_, err = tx.Exec(ctx, "INSERT INTO offline_leads(tenant_id,contact_id,access_link_id,conversation_id,client_request_id,display_name,phone,content) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8) ON CONFLICT(tenant_id,contact_id,client_request_id) DO NOTHING", a.TenantID, a.ContactID, link, in.ConversationID, in.ClientID, in.Name, in.Phone, in.Content)
		if err != nil {
			return err
		}
		result, err = txJSON(ctx, tx, "SELECT to_jsonb(t)-'notes' FROM offline_leads t WHERE contact_id=$1 AND client_request_id=$2", a.ContactID, in.ClientID)
		if err != nil {
			return err
		}
		storedConversation, _ := result["conversation_id"].(string)
		if result["content"] != in.Content || result["phone"] != in.Phone || result["display_name"] != in.Name || result["access_link_id"] != link || storedConversation != in.ConversationID {
			return platform.Fail(409, "idempotency_conflict", "同一留言编号携带了不同内容")
		}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// leads 按接待组授权列出或更新留言，修改处理状态不会伪造咨询。
func (s *Service) leads(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var in struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	}
	if r.Method == http.MethodPatch {
		if err := platform.Decode(w, r, &in); err != nil {
			replyError(w, r, err)
			return
		}
		if !inStrings(in.Status, "new", "following", "resolved") || len([]rune(in.Notes)) > 5000 {
			returnError(w, r, "invalid_lead_status", "留言状态或跟进内容不正确")
			return
		}
	}
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		scope := "($1 OR EXISTS(SELECT 1 FROM access_links l JOIN agent_group_members gm ON gm.group_id=l.service_group_id AND gm.tenant_id=l.tenant_id JOIN tenant_memberships mm ON mm.id=gm.membership_id AND mm.tenant_id=gm.tenant_id WHERE l.id=t.access_link_id AND gm.membership_id=NULLIF($2,'')::uuid AND mm.can_serve AND NOT mm.disabled AND mm.removed_at IS NULL))"
		if r.Method == http.MethodGet {
			rows, err := tx.Query(ctx, "SELECT to_jsonb(t) FROM offline_leads t WHERE "+scope+" ORDER BY created_at DESC LIMIT 200", isAdmin(a), a.MemberID)
			if err != nil {
				return err
			}
			items, err := jsonRows(rows)
			result = map[string]any{"items": items}
			return err
		}
		id := r.PathValue("id")
		item, err := txJSON(ctx, tx, "UPDATE offline_leads t SET status=$4,notes=$5,updated_at=now() WHERE "+scope+" AND id=$3 RETURNING to_jsonb(t)", isAdmin(a), a.MemberID, id, in.Status, in.Notes)
		if err != nil {
			return err
		}
		result = item
		return audit(ctx, tx, a, "lead.updated", id, map[string]any{"status": in.Status})
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// evaluate 只允许本次咨询访客在咨询结束后提交一次评分。
func (s *Service) evaluate(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	if a.ContactID == "" {
		replyError(w, r, platform.Fail(403, "visitor_required", "只有咨询访客可以评价"))
		return
	}
	var in struct {
		Score   int    `json:"score"`
		Comment string `json:"comment"`
	}
	if err := platform.Decode(w, r, &in); err != nil {
		replyError(w, r, err)
		return
	}
	if in.Score < 1 || in.Score > 5 || len([]rune(in.Comment)) > 2000 {
		returnError(w, r, "invalid_evaluation", "请选择一至五星并填写不超过2000字的评价")
		return
	}
	id := r.PathValue("id")
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tenantActive(ctx, tx, a.TenantID); err != nil {
			return err
		}
		if err := canReadConversation(ctx, tx, a, id); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, "SELECT status FROM conversations WHERE id=$1 FOR UPDATE", id).Scan(&status); err != nil {
			return err
		}
		if status != "closed" {
			return platform.Fail(409, "conversation_not_closed", "咨询结束后才能评价")
		}
		_, err := tx.Exec(ctx, "INSERT INTO conversation_evaluations(tenant_id,conversation_id,contact_id,score,comment) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id) DO NOTHING", a.TenantID, id, a.ContactID, in.Score, in.Comment)
		if err != nil {
			return err
		}
		result, err = txJSON(ctx, tx, "SELECT to_jsonb(t) FROM conversation_evaluations t WHERE conversation_id=$1", id)
		if err != nil {
			return err
		}
		if result["score"] != float64(in.Score) || result["comment"] != in.Comment {
			return platform.Fail(409, "evaluation_exists", "本次咨询已经评价，不能重复修改")
		}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// evaluations 返回真实评价记录，评分统计由租户统计模块汇总。
func (s *Service) evaluations(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var items []map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(t) FROM conversation_evaluations t ORDER BY created_at DESC LIMIT 200")
		if err != nil {
			return err
		}
		items, err = jsonRows(rows)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items})
}
