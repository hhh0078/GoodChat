// 文件作用说明：校验咨询链接的页面和发布版本绑定，解除绑定时明确恢复原有默认聊天页面。
package chat

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// prepareLinkTemplateBinding 在当前租户内验证绑定关系，页面外键锁阻止与删除并发导致失效链接。
func prepareLinkTemplateBinding(ctx context.Context, tx pgx.Tx, id string, values map[string]any) error {
	_, pageChanged := values["page_id"]
	_, modeChanged := values["binding_mode"]
	_, releaseChanged := values["pinned_release_id"]
	if !pageChanged && !modeChanged && !releaseChanged {
		return nil
	}
	var pageID, mode, releaseID string
	if id != "" {
		if err := tx.QueryRow(ctx, "SELECT coalesce(page_id::text,''),binding_mode,coalesce(pinned_release_id::text,'') FROM access_links WHERE id=$1 FOR UPDATE", id).Scan(&pageID, &mode, &releaseID); err != nil {
			return err
		}
	}
	if pageChanged {
		pageID, _ = values["page_id"].(string)
		pageID = strings.TrimSpace(pageID)
	}
	if pageID == "" {
		values["page_id"], values["pinned_release_id"], values["binding_mode"] = nil, nil, "FOLLOW_PAGE"
		return nil
	}
	if modeChanged {
		mode, _ = values["binding_mode"].(string)
	}
	if mode == "" {
		mode = "FOLLOW_PAGE"
	}
	if mode != "FOLLOW_PAGE" && mode != "PIN_RELEASE" {
		return platform.Fail(400, "invalid_template_binding", "模板更新方式只能为跟随发布或固定发布版本")
	}
	var currentRelease string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_release_id::text,'') FROM decoration_pages WHERE id=$1 FOR KEY SHARE", pageID).Scan(&currentRelease); err != nil {
		if err == pgx.ErrNoRows {
			return platform.Fail(404, "template_page_not_found", "未找到本租户的模板页面")
		}
		return err
	}
	if currentRelease == "" {
		return platform.Fail(400, "template_not_published", "请先发布模板页面，再绑定咨询链接")
	}
	values["page_id"], values["binding_mode"] = pageID, mode
	if mode == "FOLLOW_PAGE" {
		values["pinned_release_id"] = nil
		return nil
	}
	if releaseChanged {
		releaseID, _ = values["pinned_release_id"].(string)
	}
	if releaseID == "" {
		releaseID = currentRelease
	}
	var found string
	if err := tx.QueryRow(ctx, "SELECT id::text FROM decoration_releases WHERE id=$1 AND page_id=$2", releaseID, pageID).Scan(&found); err != nil {
		if err == pgx.ErrNoRows {
			return platform.Fail(400, "template_release_mismatch", "所选发布版本不属于当前模板页面")
		}
		return err
	}
	values["pinned_release_id"] = releaseID
	return nil
}
