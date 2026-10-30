// 文件作用说明：在独立PostgreSQL验证平台目录移除、来源保护、真实资源回收和人工源码修订的原子版本边界。
package decoration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// platformLifecycleTemplate 创建仅属于本测试的随机平台资源，不调用模型或覆盖默认模板。
func platformLifecycleTemplate(t *testing.T, f *integrationFixture) (string, string) {
	t.Helper()
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	var id, packageID string
	if err := pgx.BeginFunc(context.Background(), f.pool, func(tx pgx.Tx) error {
		raw, err := savePlatformPackageTemplate(context.Background(), tx, "人工编辑生命周期测试", "published", "", pkg)
		if err != nil {
			return err
		}
		var value struct {
			ID     string     `json:"id"`
			Design CodeDesign `json:"design"`
		}
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		id, packageID = value.ID, value.Design.PlatformPackageID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id, packageID
}

// TestPostgresPlatformEditorAndUnreferencedDelete 验证完整源码、受保护脚本、版本冲突及无来源平台全部历史文件的回收。
func TestPostgresPlatformEditorAndUnreferencedDelete(t *testing.T) {
	f := newAssistantFixture(t)
	id, originalPackage := platformLifecycleTemplate(t, f)
	t.Cleanup(func() { cleanupPlatformFixture(t, f, []string{id}) })
	p := f.principals[f.member]
	p.PlatformAdmin = true
	for _, item := range []struct{ method, suffix string }{{"GET", "/source"}, {"GET", "/usage"}, {"POST", "/revisions"}, {"DELETE", ""}} {
		if status, _ := f.request(item.method, "/platform/templates/"+id+item.suffix, f.member, map[string]any{"version": 1}); status != 403 {
			t.Fatal("普通租户不能读取或改写平台模板")
		}
	}
	status, source := platformFixtureCall(t, f, f.service.platformTemplateSource, "/platform/templates/"+id+"/source", id, nil, p)
	if status != 200 || source["version"] != float64(1) || source["entrypoint"] != "index.html" {
		t.Fatalf("源码读取失败：%d %v", status, source)
	}
	var cssPath, css string
	protected := 0
	for _, raw := range source["files"].([]any) {
		file := raw.(map[string]any)
		if file["protected"] == true {
			protected++
		}
		if strings.HasSuffix(file["path"].(string), ".css") && file["protected"] != true {
			cssPath, css = file["path"].(string), file["content"].(string)
		}
	}
	if cssPath == "" || protected < 3 {
		t.Fatal("源码必须同时提供可编辑样式与受保护核心")
	}
	input := map[string]any{"version": 1, "name": "人工修订后的平台模板", "files": []any{map[string]any{"path": cssPath, "content": css + "\n.platform-editor-proof{display:block}"}}}
	status, revised := platformFixtureCall(t, f, f.service.revisePlatformTemplate, "/platform/templates/"+id+"/revisions", id, input, p)
	if status != 200 || revised["version"] != float64(2) {
		t.Fatalf("平台修订失败：%d %v", status, revised)
	}
	newPackage := revised["design"].(map[string]any)["platform_package_id"].(string)
	if newPackage == originalPackage {
		t.Fatal("修订必须创建新平台包")
	}
	if status, _ := platformFixtureCall(t, f, f.service.revisePlatformTemplate, "/platform/templates/"+id+"/revisions", id, input, p); status != 409 {
		t.Fatal("旧版本修订不能覆盖新版本")
	}
	input["version"] = 2
	input["files"] = []any{map[string]any{"path": "static/goodchat/sdk.js", "content": "tampered"}}
	if status, _ := platformFixtureCall(t, f, f.service.revisePlatformTemplate, "/platform/templates/"+id+"/revisions", id, input, p); status != 400 {
		t.Fatal("平台编辑器不能修改固定SDK")
	}
	status, usage := platformFixtureCall(t, f, f.service.platformTemplateUsage, "/platform/templates/"+id+"/usage", id, nil, p)
	if status != 200 || usage["will_archive"] != false || usage["source_copies"] != float64(0) {
		t.Fatalf("未使用模板应真实删除：%d %v", status, usage)
	}
	if status, _ := platformFixtureCall(t, f, f.service.deletePlatformTemplate, "/platform/templates/"+id, id, map[string]any{"version": 1}, p); status != 409 {
		t.Fatal("删除必须检查当前版本")
	}
	status, removed := platformFixtureCall(t, f, f.service.deletePlatformTemplate, "/platform/templates/"+id, id, map[string]any{"version": 2}, p)
	if status != 200 || removed["archived"] != false || removed["deleted_packages"] != float64(2) || removed["deleted_files"].(float64) == 0 {
		t.Fatalf("应回收全部无引用历史文件：%d %v", status, removed)
	}
	var remaining int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM platform_template_package_files WHERE package_id=ANY($1::uuid[])", []string{originalPackage, newPackage}).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("删除后不能保留无引用字节")
	}
}

// TestPostgresPlatformArchivePreservesCopies 验证已复制的平台移出目录后旧链接仍可使用，且直接编号不能继续预览或复制。
func TestPostgresPlatformArchivePreservesCopies(t *testing.T) {
	f := newAssistantFixture(t)
	id, packageID := platformLifecycleTemplate(t, f)
	t.Cleanup(func() { cleanupPlatformFixture(t, f, []string{id}) })
	p := f.principals[f.member]
	p.PlatformAdmin = true
	copy := f.mustRequest("POST", "/tenant/templates/"+id+"/duplicate", f.member, nil)
	copyID := copy["id"].(string)
	otherCopy := f.mustRequest("POST", "/tenant/templates/"+id+"/duplicate", f.otherMember, nil)
	page := f.mustRequest("POST", "/tenant/pages", f.member, map[string]any{"name": "保留既有链接", "template_id": copyID})
	release := f.mustRequest("POST", "/tenant/pages/"+page["id"].(string)+"/publish", f.member, map[string]any{"version": 1, "expected_release_id": "", "link_ids": []string{f.link}})
	status, usage := platformFixtureCall(t, f, f.service.platformTemplateUsage, "/platform/templates/"+id+"/usage", id, nil, p)
	if status != 200 || usage["source_copies"] != float64(2) || usage["bound_links"] != float64(1) || usage["will_archive"] != true {
		t.Fatalf("应统计真实来源和入口：%d %v", status, usage)
	}
	if err := pgx.BeginFunc(context.Background(), f.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), "SELECT set_config('goodchat.platform_template_usage','on',true)"); err != nil {
			return err
		}
		var visible int
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM tenant_templates WHERE source_platform_template_id=$1", id).Scan(&visible); err != nil {
			return err
		}
		if visible != 0 {
			t.Fatal("应用自行设置平台统计标志不能越过RLS读取私有来源")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, removed := platformFixtureCall(t, f, f.service.deletePlatformTemplate, "/platform/templates/"+id, id, map[string]any{"version": 1}, p)
	if status != 200 || removed["archived"] != true || removed["deleted_packages"] != float64(0) {
		t.Fatalf("使用中的来源应移出目录并保留：%d %v", status, removed)
	}
	if status, _ := f.request("POST", "/tenant/templates/"+id+"/duplicate", f.member, nil); status != 404 {
		t.Fatal("归档平台模板不能再按编号复制")
	}
	if status, _ := f.request("POST", "/tenant/assistant/threads", f.member, map[string]any{"title": "不可再二创旧目录", "template_id": id, "source": "tenant"}); status != 404 {
		t.Fatal("归档平台模板不能再按编号AI二创")
	}
	if status, _ := f.request("POST", "/tenant/templates/"+id+"/preview", f.member, nil); status != 404 {
		t.Fatal("归档平台模板不能再公开预览")
	}
	for _, handler := range []struct {
		name string
		call func() (int, map[string]any)
	}{
		{"source", func() (int, map[string]any) {
			return platformFixtureCall(t, f, f.service.platformTemplateSource, "/platform/templates/"+id+"/source", id, nil, p)
		}},
		{"preview", func() (int, map[string]any) {
			return platformFixtureCall(t, f, f.service.previewPlatformTemplate, "/platform/templates/"+id+"/preview", id, nil, p)
		}},
	} {
		if status, _ := handler.call(); status != 404 {
			t.Fatal("归档平台模板不能再执行：" + handler.name)
		}
	}
	f.mustRequest("POST", "/tenant/templates/"+copyID+"/preview", f.member, nil)
	entry := f.packageEntryURL(f.service.renderURL(release["release_id"].(string), ""))
	if f.renderRequest(entry).Code != 200 {
		t.Fatal("平台目录移除不能破坏已发布副本")
	}
	var retained int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM platform_template_package_files WHERE package_id=$1", packageID).Scan(&retained); err != nil || retained == 0 {
		t.Fatal("已有来源引用的不可变平台历史必须保留")
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE access_links SET page_id=NULL,pinned_release_id=NULL,binding_mode='FOLLOW_PAGE' WHERE id=$1", f.link)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.mustRequest("DELETE", "/tenant/templates/"+copyID, f.member, nil)
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM platform_template_package_files WHERE package_id=$1", packageID).Scan(&retained); err != nil || retained == 0 {
		t.Fatal("另一个租户仍保留副本时不能回收平台来源")
	}
	f.mustRequest("DELETE", "/tenant/templates/"+otherCopy["id"].(string), f.otherMember, nil)
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM platform_template_package_files WHERE package_id=$1", packageID).Scan(&retained); err != nil || retained != 0 {
		t.Fatal("归档平台的最后来源消失后必须自动回收公共字节")
	}
}

// TestPostgresPlatformSharedPackageCleanup 验证两个公开作品共用包时仅最后一个来源清除才回收实际字节。
func TestPostgresPlatformSharedPackageCleanup(t *testing.T) {
	f := newAssistantFixture(t)
	id, packageID := platformLifecycleTemplate(t, f)
	var sharedID string
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "INSERT INTO platform_templates(name,render_mode,design,status) SELECT name||' 共享包',render_mode,design,status FROM platform_templates WHERE id=$1 RETURNING id::text", id).Scan(&sharedID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO platform_template_versions(template_id,version,name,render_mode,design) SELECT id,version,name,render_mode,design FROM platform_templates WHERE id=$1", sharedID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPlatformFixture(t, f, []string{id, sharedID}) })
	p := f.principals[f.member]
	p.PlatformAdmin = true
	status, first := platformFixtureCall(t, f, f.service.deletePlatformTemplate, "/platform/templates/"+id, id, map[string]any{"version": 1}, p)
	if status != 200 || first["retained_packages"] != float64(1) || first["deleted_packages"] != float64(0) {
		t.Fatalf("仍被另一个平台作品使用的文件必须保留：%d %v", status, first)
	}
	status, second := platformFixtureCall(t, f, f.service.deletePlatformTemplate, "/platform/templates/"+sharedID, sharedID, map[string]any{"version": 1}, p)
	if status != 200 || second["deleted_packages"] != float64(1) {
		t.Fatalf("最后一个引用移除后应清理文件：%d %v", status, second)
	}
	var remaining int
	if err := f.admin.QueryRow(ctx, "SELECT count(*) FROM platform_template_packages WHERE id=$1", packageID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("已清空引用的公共包不能残留")
	}
}
