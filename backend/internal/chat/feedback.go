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
		EntryKey       string   `json:"entry_key"`
		ConversationID string   `json:"conversation_id"`
		ClientID       string   `json:"client_request_id"`
		Name           string   `json:"display_name"`
		Phone          string   `json:"phone"`
		Content        string   `json:"content"`
		Attachments    []string `json:"attachment_ids"`
	}
	if err := platform.Decode(w, r, &in); err != nil {
		replyError(w, r, err)
		return
	}
	if !uuidPattern.MatchString(in.ClientID) || strings.TrimSpace(in.Content) == "" || len([]rune(in.Content)) > 5000 || len(in.Phone) > 40 || len([]rune(in.Name)) > 80 {
		returnError(w, r, "invalid_lead", "请填写有效留言及稳定请求编号")
		return
	}
	if in.Attachments == nil {
		in.Attachments = []string{}
	}
	if len(in.Attachments) > 3 {
		returnError(w, r, "lead_attachment_limit", "留言最多附带3个文件")
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
		if _, err := tx.Exec(ctx, "SELECT id FROM contacts WHERE id=$1 FOR UPDATE", a.ContactID); err != nil {
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
		var replay bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM offline_leads WHERE contact_id=$1 AND client_request_id=$2)", a.ContactID, in.ClientID).Scan(&replay); err != nil {
			return err
		}
		if !replay {
			if err := validateLeadAttachments(ctx, tx, a, in.ConversationID, in.Attachments); err != nil {
				return err
			}
			var count int
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM offline_leads WHERE contact_id=$1 AND created_at>now()-interval '1 minute'", a.ContactID).Scan(&count); err != nil {
				return err
			}
			if count >= 5 {
				return platform.Fail(429, "lead_rate_limited", "留言提交过于频繁，请稍后重试")
			}
		}
		created, err := tx.Exec(ctx, "INSERT INTO offline_leads(tenant_id,contact_id,access_link_id,conversation_id,client_request_id,display_name,phone,content) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8) ON CONFLICT(tenant_id,contact_id,client_request_id) DO NOTHING", a.TenantID, a.ContactID, link, in.ConversationID, in.ClientID, in.Name, in.Phone, in.Content)
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
		lead := result["id"].(string)
		if created.RowsAffected() == 1 {
			if err := attachLeadFiles(ctx, tx, a.TenantID, lead, in.Attachments); err != nil {
				return err
			}
			if err := s.enqueueLeadNotification(ctx, tx, a.TenantID, lead); err != nil {
				return err
			}
			if err := audit(ctx, tx, a, "lead.created", lead, map[string]any{"attachment_count": len(in.Attachments)}); err != nil {
				return err
			}
		} else {
			stored, err := leadAttachmentIDs(ctx, tx, lead)
			if err != nil {
				return err
			}
			if string(jsonValue(stored)) != string(jsonValue(in.Attachments)) {
				return platform.Fail(409, "idempotency_conflict", "同一留言编号不能替换或增加附件")
			}
		}
		result, err = txJSON(ctx, tx, "SELECT (to_jsonb(t)-'notes')||jsonb_build_object('attachments',"+leadAttachmentProjection+") FROM offline_leads t WHERE id=$1", lead)
		if err != nil {
			return err
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
		Status string  `json:"status"`
		Notes  *string `json:"notes"`
	}
	if r.Method == http.MethodPatch {
		if err := platform.Decode(w, r, &in); err != nil {
			replyError(w, r, err)
			return
		}
		if !inStrings(in.Status, "new", "following", "resolved") || in.Notes != nil && len([]rune(*in.Notes)) > 5000 {
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
		scope := leadReadScope
		if r.Method == http.MethodGet {
			rows, err := tx.Query(ctx, "SELECT to_jsonb(t)||jsonb_build_object('attachments',"+leadAttachmentProjection+") FROM offline_leads t WHERE "+scope+" ORDER BY created_at DESC LIMIT 200", isAdmin(a), a.MemberID)
			if err != nil {
				return err
			}
			items, err := jsonRows(rows)
			result = map[string]any{"items": items}
			return err
		}
		id := r.PathValue("id")
		item, err := txJSON(ctx, tx, "UPDATE offline_leads t SET status=$4,notes=COALESCE($5,notes),updated_at=now() WHERE "+scope+" AND id=$3 RETURNING to_jsonb(t)", isAdmin(a), a.MemberID, id, in.Status, in.Notes)
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

// evaluate 保持原评价提交接口，交由期限和版本策略处理真实访客提交。
func (s *Service) evaluate(w http.ResponseWriter, r *http.Request) {
	s.handleEvaluation(w, r)
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
