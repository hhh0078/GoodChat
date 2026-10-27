// 文件作用说明：验证留言附件的访客归属与主动共享，在统一私有下载中保持租户及业务组权限。
package chat

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

const leadReadScope = "($1 OR EXISTS(SELECT 1 FROM access_links l JOIN agent_group_members gm ON gm.group_id=l.service_group_id AND gm.tenant_id=l.tenant_id JOIN tenant_memberships mm ON mm.id=gm.membership_id AND mm.tenant_id=gm.tenant_id WHERE l.id=t.access_link_id AND gm.membership_id=NULLIF($2,'')::uuid AND mm.can_serve AND NOT mm.disabled AND mm.removed_at IS NULL))"
const leadAttachmentProjection = "COALESCE((SELECT jsonb_agg(jsonb_build_object('id',a.id,'file_name',a.file_name,'content_type',a.content_type,'size_bytes',a.size_bytes) ORDER BY la.position) FROM offline_lead_attachments la JOIN attachments a ON a.id=la.attachment_id AND a.tenant_id=la.tenant_id WHERE la.lead_id=t.id AND la.tenant_id=t.tenant_id),'[]'::jsonb)"

// validateLeadAttachments 仅接受本访客上传的最多三件附件，撤回聊天内容不能被自动重新共享。
func validateLeadAttachments(ctx context.Context, tx pgx.Tx, a actor, conversation string, ids []string) error {
	if len(ids) > 3 {
		return platform.Fail(400, "lead_attachment_limit", "留言最多附带3个文件")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !uuidPattern.MatchString(id) || seen[id] {
			return platform.Fail(400, "invalid_lead_attachment", "附件编号无效或重复")
		}
		seen[id] = true
		var owned, linked, visible bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM attachments WHERE id=$1 AND uploader_kind='visitor' AND uploader_id=$2 AND size_bytes BETWEEN 1 AND 20971520),EXISTS(SELECT 1 FROM messages WHERE attachment_id=$1),EXISTS(SELECT 1 FROM messages WHERE attachment_id=$1 AND conversation_id=NULLIF($3,'')::uuid AND audience='public' AND withdrawn_at IS NULL)", id, a.ContactID, conversation).Scan(&owned, &linked, &visible); err != nil {
			return err
		}
		if !owned || linked && !visible {
			return platform.Fail(403, "lead_attachment_forbidden", "附件须由当前访客上传，已关联聊天的附件必须在本会话公开且未撤回")
		}
	}
	return nil
}

// attachLeadFiles 只在新留言事务内建立主动共享，后续聊天撤回不删除该独立共享。
func attachLeadFiles(ctx context.Context, tx pgx.Tx, tenant, lead string, ids []string) error {
	for index, id := range ids {
		if _, err := tx.Exec(ctx, "INSERT INTO offline_lead_attachments(tenant_id,lead_id,attachment_id,position) VALUES($1,$2,$3,$4)", tenant, lead, id, index); err != nil {
			return err
		}
	}
	return nil
}

// leadAttachmentIDs 返回已持久保存的原顺序，幂等重试不能偷偷添加或替换附件。
func leadAttachmentIDs(ctx context.Context, tx pgx.Tx, lead string) ([]string, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(attachment_id::text ORDER BY position),'[]'::jsonb) FROM offline_lead_attachments WHERE lead_id=$1", lead).Scan(&raw); err != nil {
		return nil, err
	}
	ids := []string{}
	err := json.Unmarshal(raw, &ids)
	return ids, err
}

// canReadLeadAttachment 在入口已完成租户与封禁成员检查后，按留言本人或当前业务组权限授权。
func canReadLeadAttachment(ctx context.Context, tx pgx.Tx, a actor, attachment string) (bool, error) {
	var allowed bool
	if a.ContactID != "" {
		err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM offline_lead_attachments la JOIN offline_leads t ON t.id=la.lead_id AND t.tenant_id=la.tenant_id WHERE la.attachment_id=$1 AND t.contact_id=$2)", attachment, a.ContactID).Scan(&allowed)
		if err != nil || !allowed {
			return allowed, err
		}
		return true, audit(ctx, tx, a, "lead_attachment.download_authorized", attachment, map[string]any{"authorization": "lead_share"})
	}
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM offline_lead_attachments la JOIN offline_leads t ON t.id=la.lead_id AND t.tenant_id=la.tenant_id WHERE "+leadReadScope+" AND la.attachment_id=$3)", isAdmin(a), a.MemberID, attachment).Scan(&allowed)
	if err != nil || !allowed {
		return allowed, err
	}
	return true, audit(ctx, tx, a, "lead_attachment.download_authorized", attachment, map[string]any{"authorization": "lead_share"})
}
