// 文件作用说明：按当前客户接待权限分页读取跨咨询内部备注，并以稳定编号防止网络重试重复添加。
package chat

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// contactNotes 在每次读写时重验当前客户权限，历史接待关系不能继续授予客户内部资料访问权。
func (s *Service) contactNotes(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	contact := r.PathValue("id")
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := canSeeContact(r.Context(), tx, a, contact); err != nil {
			return err
		}
		if r.Method == http.MethodPost {
			var input struct {
				Content  string `json:"content"`
				ClientID string `json:"client_note_id"`
			}
			if err := platform.Decode(w, r, &input); err != nil {
				return err
			}
			input.Content = strings.TrimSpace(input.Content)
			if !uuidPattern.MatchString(input.ClientID) || input.Content == "" || len([]rune(input.Content)) > 5000 {
				return platform.Fail(400, "invalid_note", "备注需要稳定编号，内容为1至5000字")
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO contact_notes(tenant_id,contact_id,author_id,client_note_id,content) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,contact_id,author_id,client_note_id) DO NOTHING`, a.TenantID, contact, a.MemberID, input.ClientID, input.Content); err != nil {
				return err
			}
			var err error
			result, err = txJSON(r.Context(), tx, `SELECT to_jsonb(n)||jsonb_build_object('author_name',coalesce(nullif(m.display_name,''),u.display_name),'message_type','text','attachment',NULL) FROM contact_notes n JOIN tenant_memberships m ON m.id=n.author_id AND m.tenant_id=n.tenant_id JOIN users u ON u.id=m.user_id WHERE n.contact_id=$1 AND n.author_id=$2 AND n.client_note_id=$3`, contact, a.MemberID, input.ClientID)
			if err != nil {
				return err
			}
			if result["content"] != input.Content {
				return platform.Fail(409, "note_conflict", "该备注编号已保存其他内容，请刷新后重试")
			}
			return nil
		}
		limit := 30
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 100 {
				return platform.Fail(400, "invalid_limit", "备注每页数量需为1至100")
			}
			limit = parsed
		}
		before, beforeID := r.URL.Query().Get("before"), r.URL.Query().Get("before_id")
		if (before == "") != (beforeID == "") {
			return platform.Fail(400, "invalid_cursor", "备注分页游标不完整")
		}
		if before != "" {
			if _, err := time.Parse(time.RFC3339Nano, before); err != nil || !uuidPattern.MatchString(beforeID) {
				return platform.Fail(400, "invalid_cursor", "备注分页游标无效")
			}
		}
		rows, err := tx.Query(r.Context(), `SELECT jsonb_build_object('id',n.id,'content',n.content,'created_at',n.created_at,'author_name',n.author_name,'message_type',n.message_type,'conversation_id',n.conversation_id,'attachment',n.attachment) FROM (
 SELECT n.id,n.content,n.created_at,coalesce(nullif(m.display_name,''),u.display_name) author_name,'text'::text message_type,NULL::uuid conversation_id,NULL::jsonb attachment FROM contact_notes n JOIN tenant_memberships m ON m.id=n.author_id AND m.tenant_id=n.tenant_id JOIN users u ON u.id=m.user_id WHERE n.contact_id=$1
 UNION ALL
 SELECT m.id,m.content,m.created_at,coalesce(nullif(tm.display_name,''),u.display_name,'历史客服') author_name,m.message_type,c.id conversation_id,
 CASE WHEN f.id IS NOT NULL THEN jsonb_build_object('file_name',f.file_name,'content_type',f.content_type,'size_bytes',f.size_bytes,'download_path','/tenant/contacts/'||c.contact_id::text||'/notes/'||m.id::text||'/attachment') ELSE NULL END attachment
 FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.tenant_id=m.tenant_id LEFT JOIN tenant_memberships tm ON tm.id=m.sender_id AND tm.tenant_id=m.tenant_id LEFT JOIN users u ON u.id=tm.user_id LEFT JOIN attachments f ON f.id=m.attachment_id AND f.tenant_id=m.tenant_id WHERE c.contact_id=$1 AND m.audience='internal' AND m.withdrawn_at IS NULL AND chat_message_visible(NULLIF($5,'')::uuid,NULL,$6,m.id)
 ) n WHERE ($2='' OR (n.created_at,n.id)<(NULLIF($2,'')::timestamptz,NULLIF($3,'')::uuid)) ORDER BY n.created_at DESC,n.id DESC LIMIT $4`, contact, before, beforeID, limit+1, a.MemberID, isAdmin(a))
		if err != nil {
			return err
		}
		items, err := jsonRows(rows)
		if err != nil {
			return err
		}
		more := len(items) > limit
		if more {
			items = items[:limit]
		}
		result = map[string]any{"items": items, "has_more": more, "next_before": "", "next_before_id": ""}
		if more {
			last := items[len(items)-1]
			result["next_before"], result["next_before_id"] = last["created_at"], last["id"]
		}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}
