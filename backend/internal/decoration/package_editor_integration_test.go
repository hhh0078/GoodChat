// 文件作用说明：在独立空PostgreSQL验证源码权限、图片修订、编辑预览过期及公开发布不含编辑通道。
package decoration

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestPostgresEditorPackageSourceAndPreview 验证完整源码和图片只在本租户编辑，快照不创建永久包且通道不会进入公开发布。
func TestPostgresEditorPackageSourceAndPreview(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	template := f.mustRequest("POST", "/tenant/template-industries/finance/copy", f.member, nil)
	id := template["id"].(string)
	packageID := template["design"].(map[string]any)["package_id"].(string)
	source := f.mustRequest("GET", "/tenant/code-packages/"+packageID+"/source", f.member, nil)
	var markup string
	for _, raw := range source["files"].([]any) {
		file := raw.(map[string]any)
		if file["path"] == "index.html" {
			markup = file["content"].(string)
		}
		if file["path"] == templateSDKPath && file["protected"] != true {
			t.Fatal("完整源码须明确保护核心SDK")
		}
	}
	if markup == "" || source["package"] == nil {
		t.Fatal("源码必须返回入口原始字节及资源包元数据")
	}
	if status, _ := f.request("GET", "/tenant/code-packages/"+packageID+"/source", f.otherMember, nil); status != 404 {
		t.Fatal("跨租户不能读取源码")
	}
	agent := f.principals[f.member]
	agent.Role = "agent"
	f.principals["editor-agent"] = agent
	if status, _ := f.request("GET", "/tenant/code-packages/"+packageID+"/source", "editor-agent", nil); status != 403 {
		t.Fatal("客服角色不能进入源码编辑器")
	}
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 20, 12))); err != nil {
		t.Fatal(err)
	}
	markup = strings.Replace(markup, "<body", `<body data-gc-edit-id="edit-root"`, 1)
	changes := []any{map[string]any{"path": "index.html", "content": markup}, map[string]any{"path": "static/images/editor-test.png", "data_base64": base64.StdEncoding.EncodeToString(imageData.Bytes())}}
	nonce := "10000000-0000-4000-8000-000000000099"
	input := map[string]any{"editor_nonce": nonce, "files": changes}
	if status, _ := f.request("POST", "/tenant/code-packages/"+packageID+"/editor-preview", f.otherMember, input); status != 404 {
		t.Fatal("跨租户不能创建编辑快照")
	}
	if status, _ := f.request("POST", "/tenant/code-packages/"+packageID+"/editor-preview", "editor-agent", input); status != 403 {
		t.Fatal("客服角色不能创建编辑快照")
	}
	preview := f.mustRequest("POST", "/tenant/code-packages/"+packageID+"/editor-preview", f.member, input)
	entry := f.packageEntryURL(preview["preview_url"].(string))
	rendered := f.renderRequest(entry)
	if rendered.Code != http.StatusOK || !strings.Contains(rendered.Body.String(), "goodchat:editor-ready") || !strings.Contains(rendered.Body.String(), nonce) || !strings.Contains(rendered.Body.String(), "edit-root") || !strings.Contains(rendered.Body.String(), `data-gc-editor="true"`) {
		t.Fatal("编辑快照必须加载官方选择脚本与当前一次性标识")
	}
	imageResponse := f.renderRequest(staticRelative(entry, "static/images/editor-test.png"))
	if imageResponse.Code != http.StatusOK || !bytes.Equal(imageResponse.Body.Bytes(), imageData.Bytes()) {
		t.Fatal("编辑预览必须能够读取同快照的上传图片")
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		var packages, pages, templates int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM decoration_code_packages),(SELECT count(*) FROM decoration_pages),(SELECT count(*) FROM tenant_templates)`).Scan(&packages, &pages, &templates); err != nil {
			return err
		}
		if packages != 1 || pages != 0 || templates != 1 {
			t.Fatal("编辑预览不能生成永久资源或页面")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	revised := f.mustRequest("POST", "/tenant/code-packages/"+packageID+"/revisions", f.member, map[string]any{"files": changes})
	newID := revised["design"].(map[string]any)["package_id"].(string)
	if newID == packageID {
		t.Fatal("保存图片必须生成不可变新包")
	}
	updated := f.mustRequest("GET", "/tenant/code-packages/"+newID+"/source", f.member, nil)
	imageFound := false
	for _, raw := range updated["files"].([]any) {
		file := raw.(map[string]any)
		if file["path"] == "static/images/editor-test.png" {
			imageFound = file["data_base64"] == base64.StdEncoding.EncodeToString(imageData.Bytes())
		}
	}
	if !imageFound {
		t.Fatal("新包完整源码必须保留图片原始字节")
	}
	normal := f.mustRequest("POST", "/tenant/templates/"+id+"/preview", f.member, nil)
	if response := f.renderRequest(f.packageEntryURL(normal["preview_url"].(string))); strings.Contains(response.Body.String(), "goodchat:editor-ready") {
		t.Fatal("普通预览不能注入编辑选择通道")
	}
	page := f.mustRequest("POST", "/tenant/pages", f.member, map[string]any{"name": "编辑通道边界测试", "template_id": id})
	published := f.mustRequest("POST", "/tenant/pages/"+page["id"].(string)+"/publish", f.member, map[string]any{"version": 1, "expected_release_id": "", "link_ids": []string{}})
	public := f.renderRequest(f.packageEntryURL(f.service.renderURL(published["release_id"].(string), "")))
	if public.Code != http.StatusOK || strings.Contains(public.Body.String(), "goodchat:editor-ready") || strings.Contains(public.Body.String(), nonce) || strings.Contains(public.Body.String(), `data-gc-editor="true"`) {
		t.Fatal("公开发布不能包含编辑通道或一次性选择标识")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE decoration_previews SET expires_at=now()-interval '1 second' WHERE id=$1", preview["release_id"]); err != nil {
		t.Fatal(err)
	}
	if f.renderRequest(entry).Code != http.StatusNotFound {
		t.Fatal("编辑通道必须随短期预览过期")
	}
}

// TestPostgresPlatformEditorPreview 验证平台编辑预览同样创建租户内临时快照，不修改平台原始版本。
func TestPostgresPlatformEditorPreview(t *testing.T) {
	f := newAssistantFixture(t)
	id, _ := platformLifecycleTemplate(t, f)
	t.Cleanup(func() { cleanupPlatformFixture(t, f, []string{id}) })
	input := map[string]any{"editor_nonce": "10000000-0000-4000-8000-000000000088", "files": []any{map[string]any{"path": "static/editor-note.txt", "content": "平台编辑未保存快照"}}}
	if status, _ := f.request("POST", "/platform/templates/"+id+"/editor-preview", f.member, input); status != http.StatusForbidden {
		t.Fatal("普通租户管理员不能访问平台编辑快照")
	}
	principal := f.principals[f.member]
	principal.PlatformAdmin = true
	status, preview := platformFixtureCall(t, f, f.service.previewEditorPlatform, "/platform/templates/"+id+"/editor-preview", id, input, principal)
	if status != http.StatusOK {
		t.Fatalf("平台编辑快照失败：%d %v", status, preview)
	}
	rendered := f.renderRequest(f.packageEntryURL(preview["preview_url"].(string)))
	if rendered.Code != http.StatusOK || !strings.Contains(rendered.Body.String(), "goodchat:editor-ready") {
		t.Fatal("平台快照必须包含受控编辑脚本")
	}
	var version int
	if err := f.admin.QueryRow(context.Background(), "SELECT version FROM platform_templates WHERE id=$1", id).Scan(&version); err != nil || version != 1 {
		t.Fatal("未保存编辑预览不能修改平台版本")
	}
}
