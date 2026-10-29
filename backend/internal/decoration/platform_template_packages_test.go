// 文件作用说明：验证公开静态包引用的结构限制及跨租户复制、隐藏版本、AI二创和显式上架的权限边界。
package decoration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestPlatformPackageDesignShape 禁止把平台包与私有包、内联覆盖混用，同时保留独立平台引用。
func TestPlatformPackageDesignShape(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	raw, _ := json.Marshal(CodeDesign{PlatformPackageID: id})
	if _, err := validateDesign("code", raw); err != nil {
		t.Fatal(err)
	}
	for _, design := range []CodeDesign{{PlatformPackageID: id, PackageID: id}, {PlatformPackageID: id, HTML: "override"}, {PlatformPackageID: "bad-id"}} {
		raw, _ := json.Marshal(design)
		if _, err := validateDesign("code", raw); err == nil {
			t.Fatal("平台包引用不能混入覆盖或错误编号")
		}
	}
}

// TestPlatformTemplateRouteRegistration 验证平台行业创建与统一预览路由可以同时注册，避免服务启动时出现通配模式冲突。
func TestPlatformTemplateRouteRegistration(t *testing.T) {
	service := &Service{auth: integrationAuth{principals: map[string]platform.Principal{}}}
	service.Register(http.NewServeMux())
}

// platformFixtureCall 仅在测试中为经过明确设置的平台身份直接调用处理器，生产鉴权路由仍单独验证拒绝普通租户。
func platformFixtureCall(t *testing.T, f *integrationFixture, handler http.HandlerFunc, path, id string, input any, principal platform.Principal) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(input)
	request := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	if id != "" {
		request.SetPathValue("id", id)
	}
	request = request.WithContext(platform.WithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	handler(response, request)
	var envelope map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &envelope)
	if data, ok := envelope["data"].(map[string]any); ok {
		return response.Code, data
	}
	return response.Code, envelope
}

// cleanupPlatformFixture 仅清理本测试明确记录的随机平台编号，不触及默认行业或用户模板。
func cleanupPlatformFixture(t *testing.T, f *integrationFixture, ids []string) {
	t.Helper()
	if len(ids) == 0 {
		return
	}
	ctx := context.Background()
	tx, err := f.admin.Begin(ctx)
	if err != nil {
		t.Error("平台测试资源清理连接失败")
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); err != nil {
		t.Error("平台测试清理需要独立测试管理员")
		return
	}
	var packageIDs []string
	rows, err := tx.Query(ctx, "SELECT DISTINCT platform_package_id::text FROM platform_templates WHERE id=ANY($1::uuid[]) AND platform_package_id IS NOT NULL", ids)
	if err != nil {
		t.Error("平台测试包清单读取失败")
		return
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Error("平台测试包清单解码失败")
			return
		}
		packageIDs = append(packageIDs, id)
	}
	rows.Close()
	for _, statement := range []string{"DELETE FROM platform_template_versions WHERE template_id=ANY($1::uuid[])", "DELETE FROM platform_templates WHERE id=ANY($1::uuid[])"} {
		if _, err := tx.Exec(ctx, statement, ids); err != nil {
			t.Error("平台测试版本清理失败")
			return
		}
	}
	if len(packageIDs) > 0 {
		for _, statement := range []string{"DELETE FROM platform_template_package_files WHERE package_id=ANY($1::uuid[])", "DELETE FROM platform_template_packages WHERE id=ANY($1::uuid[])"} {
			if _, err := tx.Exec(ctx, statement, packageIDs); err != nil {
				t.Error("平台测试文件清理失败")
				return
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Error("平台测试清理提交失败")
	}
}

// TestPostgresPlatformStaticTemplates 验证公开包物化为两个租户各自的私有资源，隐藏不影响既有副本，私有作品仅由授权管理员显式公开。
func TestPostgresPlatformStaticTemplates(t *testing.T) {
	f := newAssistantFixture(t)
	ctx := context.Background()
	var publicIDs []string
	t.Cleanup(func() { cleanupPlatformFixture(t, f, publicIDs) })
	p := f.principals[f.member]
	p.PlatformAdmin = true
	var publicID, globalPackageID string
	if err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		scripts, err := f.service.currentTemplateScripts(ctx, tx)
		if err != nil {
			return err
		}
		pkg, err := industryTemplatePackage("finance", scripts)
		if err != nil {
			return err
		}
		raw, err := savePlatformPackageTemplate(ctx, tx, "隔离测试平台模板", "published", "", pkg)
		if err != nil {
			return err
		}
		var item struct {
			ID     string     `json:"id"`
			Design CodeDesign `json:"design"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		publicID = item.ID
		globalPackageID = item.Design.PlatformPackageID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	publicIDs = append(publicIDs, publicID)
	if status, _ := f.request("POST", "/platform/template-industries/finance/copy", f.member, map[string]any{"status": "published"}); status != 403 {
		t.Fatal("普通租户不能创建平台模板")
	}
	first := f.mustRequest("POST", "/tenant/templates/"+publicID+"/duplicate", f.member, map[string]any{})
	second := f.mustRequest("POST", "/tenant/templates/"+publicID+"/duplicate", f.otherMember, map[string]any{})
	firstDesign := first["design"].(map[string]any)
	secondDesign := second["design"].(map[string]any)
	if firstDesign["package_id"] == nil || secondDesign["package_id"] == nil || firstDesign["platform_package_id"] != nil || firstDesign["package_id"] == secondDesign["package_id"] {
		t.Fatal("平台复制必须物化为不同租户的独立私有包")
	}
	if first["source_platform_template_id"] != publicID || first["source_platform_version"] != float64(1) {
		t.Fatal("复制必须固定平台来源版本")
	}
	thread := f.mustRequest("POST", "/tenant/assistant/threads", f.member, map[string]any{"title": "平台静态模板二创测试", "template_id": publicID, "source": "tenant"})
	if thread["template_id"] == publicID || thread["page_id"] == nil {
		t.Fatal("AI二创必须生成独立私有静态副本")
	}
	if status, _ := f.request("POST", "/tenant/templates", f.member, map[string]any{"name": "越界公共引用", "render_mode": "code", "design": map[string]any{"platform_package_id": globalPackageID}}); status != 400 {
		t.Fatal("租户不能直接保存公共包引用")
	}
	status, hidden := platformFixtureCall(t, f, f.service.platformTemplates, "/api/v1/platform/templates/"+publicID, publicID, map[string]any{"name": "隔离测试平台模板", "render_mode": "code", "design": map[string]any{"platform_package_id": globalPackageID}, "status": "hidden", "version": 1}, p)
	if status != 200 || hidden["version"] != float64(2) {
		t.Fatal("平台上架修改应创建新版本")
	}
	if status, _ := f.request("POST", "/tenant/templates/"+publicID+"/duplicate", f.otherMember, map[string]any{}); status != 404 {
		t.Fatal("隐藏平台模板不能继续复制")
	}
	if status, _ := f.request("POST", "/platform/templates/from-private", f.member, map[string]any{"source_template_id": first["id"], "name": "未授权公开", "status": "published"}); status != 403 {
		t.Fatal("普通租户不得公开私有作品")
	}
	status, promoted := platformFixtureCall(t, f, f.service.publishPrivatePlatformTemplate, "/api/v1/platform/templates/from-private", "", map[string]any{"source_template_id": first["id"], "name": "隔离测试显式上架", "status": "published"}, p)
	if status != 200 {
		t.Fatalf("授权平台上架失败：%v", promoted)
	}
	publicIDs = append(publicIDs, promoted["id"].(string))
	if promoted["design"].(map[string]any)["package_id"] != nil || promoted["design"].(map[string]any)["platform_package_id"] == nil {
		t.Fatal("显式上架不能引用原私有包")
	}
	if _, err := f.pool.Exec(ctx, "UPDATE platform_template_package_files SET content='changed' WHERE package_id=$1", globalPackageID); err == nil {
		t.Fatal("平台静态字节不可原地修改")
	}
}
