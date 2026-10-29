// 文件作用说明：在真实PostgreSQL验证私有代码包隔离、不可变文件、短期资源链、修订发布及回滚，不启动浏览器。
package decoration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"supportdesk/backend/internal/platform"
)

// uploadPackage 通过实际multipart HTTP调用上传无敏感静态测试包。
func (f *integrationFixture) uploadPackage(raw []byte, member string) (int, map[string]any) {
	f.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("name", "多文件测试包")
	_ = form.WriteField("entrypoint", "index.html")
	file, _ := form.CreateFormFile("file", "website.zip")
	_, _ = file.Write(raw)
	_ = form.Close()
	request, _ := http.NewRequest("POST", f.server.URL+"/api/v1/tenant/code-packages", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-Test-Member", member)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	rawResponse, _ := io.ReadAll(response.Body)
	var result map[string]any
	_ = json.Unmarshal(rawResponse, &result)
	return response.StatusCode, result
}

// mustPersistLegacyPackage 仅在测试内模拟数据库已存在的旧包，生产上传接口不允许此兼容旁路。
func (f *integrationFixture) mustPersistLegacyPackage(files map[string][]byte) map[string]any {
	f.t.Helper()
	pkg, err := readStaticZIP(zipFixture(f.t, files, 0), "site/index.html")
	if err != nil {
		f.t.Fatal(err)
	}
	var result any
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var err error
		result, err = f.service.storePackage(context.Background(), tx, f.principals[f.member], "既有旧静态包", pkg)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		f.t.Fatal(err)
	}
	return value
}

// staticRelative 按浏览器标准URL解析规则跟随实际静态包相对依赖。
func staticRelative(base, reference string) string {
	parsed, _ := url.Parse(base)
	next, _ := url.Parse(reference)
	return parsed.ResolveReference(next).String()
}

// packageEntryURL 跟随受限主入口重定向，得到真实资源基址。
func (f *integrationFixture) packageEntryURL(rawURL string) string {
	f.t.Helper()
	response := f.renderRequest(rawURL)
	if response.Code != 302 {
		f.t.Fatalf("资源包入口应重定向到绑定版本：%d %s", response.Code, response.Body.String())
	}
	return staticRelative(rawURL, response.Header().Get("Location"))
}

// TestPostgresPackageReleaseIsolation 验证源码私有、相对资源令牌链、不可变修订、发布与回滚的完整静态闭环。
func TestPostgresPackageReleaseIsolation(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	data := f.mustPersistLegacyPackage(packageFixtureFiles(t))
	pkg := data["package"].(map[string]any)
	id := pkg["id"].(string)
	design := data["design"].(map[string]any)
	if got := f.renderRequest(strings.Replace(f.service.renderURL(id, ""), "/render/"+id, "/assets/"+id+"/site/index.html", 1)); got.Code != 404 {
		t.Fatal("尚未发布的私有包不能通过独立站点直接访问")
	}
	if status, _ := f.request("GET", "/tenant/code-packages/"+id, f.otherMember, nil); status != 404 {
		t.Fatal("私有资源包不能跨租户读取")
	}
	if status, _ := f.request("GET", "/tenant/code-packages/"+id+"/files?path=site/js/main.mjs", f.otherMember, nil); status != 404 {
		t.Fatal("私有源码不能跨租户读取")
	}
	if status, _ := f.request("POST", "/tenant/templates", f.otherMember, map[string]any{"name": "越权包", "render_mode": "code", "design": design}); status != 404 {
		t.Fatalf("外租户包引用应返回不存在，收到%d", status)
	}
	if status, _ := f.uploadPackage([]byte("not zip"), f.member); status != 400 {
		t.Fatal("无效资源包不能进入私有列表")
	}
	for _, table := range []string{"decoration_code_packages", "decoration_package_files"} {
		var count int
		if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("没有租户事务时必须看不到私有资源表")
		}
	}
	template := f.mustRequest("POST", "/tenant/templates", f.member, map[string]any{"name": "资源包模板", "render_mode": "code", "design": design})
	page := f.mustRequest("POST", "/tenant/pages", f.member, map[string]any{"name": "资源包页面", "template_id": template["id"]})
	pageID := page["id"].(string)
	preview := f.mustRequest("POST", "/tenant/pages/"+pageID+"/preview", f.member, nil)
	previewURL := f.packageEntryURL(preview["preview_url"].(string))
	response := f.renderRequest(previewURL)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "goodchat:init") {
		t.Fatalf("真实包预览应注入SDK：%d %s", response.Code, response.Body.String())
	}
	for _, directive := range []string{"script-src 'self'", "connect-src 'none'", "font-src 'self' data:", "frame-src 'none'", "form-action 'none'", "sandbox allow-scripts allow-same-origin"} {
		if !strings.Contains(response.Header().Get("Content-Security-Policy"), directive) {
			t.Fatal("缺少受限静态资源CSP：" + directive)
		}
	}
	if strings.Contains(response.Header().Get("Content-Security-Policy"), "https:") || response.Header().Get("Set-Cookie") != "" {
		t.Fatal("静态包不能获得外网通配或管理Cookie")
	}
	cssURL := staticRelative(previewURL, "./css/main.css")
	css := f.renderRequest(cssURL)
	if css.Code != 200 || !strings.Contains(css.Body.String(), "/preview-assets/"+preview["release_id"].(string)+"/") {
		t.Fatal("CSS嵌套引用必须继承短凭证")
	}
	for _, resource := range []string{staticRelative(cssURL, "./nested/colors.css"), staticRelative(cssURL, "../logo.png"), staticRelative(previewURL, "./js/main.mjs"), staticRelative(staticRelative(previewURL, "./js/main.mjs"), "./helpers/words.mjs")} {
		if got := f.renderRequest(resource); got.Code != 200 {
			t.Fatalf("真实相对资源链无法读取：%d %s", got.Code, resource)
		}
	}
	if got := f.renderRequest(strings.Replace(previewURL, "/preview-assets/", "/assets/", 1)); got.Code != 404 {
		t.Fatal("预览不能去掉短令牌后读取")
	}
	for _, reference := range []string{"./%252e%252e/secret.txt", "./%2e%2e/%2e%2e/secret.txt", "./logo.png?secret=blocked"} {
		if got := f.renderRequest(staticRelative(previewURL, reference)); got.Code != 404 {
			t.Fatal("静态资源路径不能多轮解码、穿越或附查询参数")
		}
	}
	published := f.mustRequest("POST", "/tenant/pages/"+pageID+"/publish", f.member, map[string]any{"version": 1, "expected_release_id": "", "link_ids": []string{f.link}})
	firstID := published["release_id"].(string)
	firstURL := f.packageEntryURL(f.service.renderURL(firstID, ""))
	if body := f.renderRequest(staticRelative(firstURL, "./js/helpers/words.mjs")).Body.String(); !strings.Contains(body, "中文资源包") {
		t.Fatal("发布应保留原始包资源")
	}
	if status, _ := f.request("POST", "/tenant/code-packages/"+id+"/revisions", f.member, map[string]any{"name": "不能继续创建旧式脚本", "files": []any{map[string]any{"path": "site/new.txt", "content": "new-only"}}}); status != 400 {
		t.Fatal("旧包兼容仅限已有内容，新修订必须升级固定SDK")
	}
	legacy, err := readStaticZIP(zipFixture(t, packageFixtureFiles(t), 0), "site/index.html")
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := upgradeLegacyTemplatePackage(legacy, defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	upgradedFiles := map[string][]byte{}
	for _, file := range upgraded.Files {
		upgradedFiles[file.Path] = file.Content
	}
	status, imported := f.uploadPackage(zipFixture(t, upgradedFiles, 0), f.member)
	if status != 200 {
		t.Fatalf("升级后的完整静态ZIP应可导入：%d %v", status, imported)
	}
	strictID := imported["data"].(map[string]any)["package"].(map[string]any)["id"].(string)
	revision := f.mustRequest("POST", "/tenant/code-packages/"+strictID+"/revisions", f.member, map[string]any{"name": "修改副本", "files": []any{map[string]any{"path": "static/new.txt", "content": "new-only"}}})
	newDesign := revision["design"].(map[string]any)
	if newDesign["package_id"] == id {
		t.Fatal("修订必须产生新编号")
	}
	if got := f.renderRequest(staticRelative(firstURL, "./new.txt")); got.Code != 404 {
		t.Fatal("旧发布不能读取新包增加的文件")
	}
	if body := f.renderRequest(staticRelative(firstURL, "./js/helpers/words.mjs")).Body.String(); !strings.Contains(body, "中文资源包") {
		t.Fatal("修订不能改变旧发布字节")
	}
	f.mustRequest("PATCH", "/tenant/pages/"+pageID, f.member, map[string]any{"name": "资源包页面", "render_mode": "code", "draft": newDesign, "version": 1})
	second := f.mustRequest("POST", "/tenant/pages/"+pageID+"/publish", f.member, map[string]any{"version": 2, "expected_release_id": firstID, "link_ids": []string{}})
	secondID := second["release_id"].(string)
	secondURL := f.packageEntryURL(f.service.renderURL(secondID, ""))
	if body := f.renderRequest(staticRelative(secondURL, "./static/new.txt")).Body.String(); body != "new-only" {
		t.Fatal("新发布应读取新包资源")
	}
	attack, _ := url.Parse(staticRelative(secondURL, "./static/new.txt"))
	oldOrigin, _ := url.Parse(firstURL)
	attack.Host = oldOrigin.Host
	if got := f.renderRequest(attack.String()); got.Code != 421 {
		t.Fatal("旧发布来源不能读取另一发布资源")
	}
	f.mustRequest("POST", "/tenant/pages/"+pageID+"/rollback", f.member, map[string]any{"release_id": firstID, "expected_release_id": secondID})
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		entry, err := f.service.EntryDecoration(ctx, tx, f.tenant, f.link)
		if err != nil {
			return err
		}
		if entry["release_id"] != firstID {
			t.Fatal("回滚应恢复旧包绑定")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE decoration_previews SET expires_at=now()-interval '1 second' WHERE id=$1", preview["release_id"]); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{previewURL, cssURL, staticRelative(previewURL, "./js/main.mjs")} {
		if got := f.renderRequest(raw); got.Code != 404 {
			t.Fatal("过期预览的所有子资源必须失效")
		}
	}
	if got := f.renderRequest(firstURL); got.Code != 200 {
		t.Fatal("预览过期不影响已发布固定资源")
	}
}

// TestPostgresPackageDatabaseFreeze 验证数据库本身拒绝追加、替换、摘要伪造和跨租户复合引用。
func TestPostgresPackageDatabaseFreeze(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	data := f.mustPersistLegacyPackage(packageFixtureFiles(t))
	id := data["package"].(map[string]any)["id"].(string)
	for _, query := range []string{
		"INSERT INTO decoration_package_files(tenant_id,package_id,path,content_type,sha256,content) VALUES($1,$2,'site/later.js','text/javascript; charset=utf-8','x','x')",
		"UPDATE decoration_package_files SET content='changed' WHERE tenant_id=$1 AND package_id=$2",
		"DELETE FROM decoration_package_files WHERE tenant_id=$1 AND package_id=$2",
	} {
		err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, query, f.tenant, id); return err })
		if err == nil {
			t.Fatal("提交后追加、替换或删除私有包必须被数据库拒绝")
		}
	}
	err := platform.TenantTx(ctx, f.pool, f.other, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO tenant_templates(tenant_id,owner_membership_id,name,render_mode,design) VALUES($1,$2,'FK逃逸','code',$3)", f.other, f.otherMember, data["design"])
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("跨租户复合外键未拒绝引用：%v", err)
	}
	// 在同一创建事务内伪造hash，也必须被内容实际SHA256检查阻止。
	err = platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		var newID string
		manifest := `[{"path":"bad.js","content_type":"text/javascript; charset=utf-8","size":1,"sha256":"forged"}]`
		if err := tx.QueryRow(ctx, "INSERT INTO decoration_code_packages(tenant_id,owner_membership_id,name,entrypoint,manifest,file_count,total_bytes,sha256) VALUES($1,$2,'伪造摘要','bad.js',$3,1,1,'x') RETURNING id::text", f.tenant, f.member, manifest).Scan(&newID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO decoration_package_files(tenant_id,package_id,path,content_type,sha256,content) VALUES($1,$2,'bad.js','text/javascript; charset=utf-8','forged','x')", f.tenant, newID)
		return err
	})
	if err == nil {
		t.Fatal("manifest声明不能代替文件实际哈希")
	}
}

// TestPostgresNewPackageRoutesRequireFixedSDK 验证旧上传入口也不能用删除合约、伪造摘要或修改核心脚本创建新模板。
func TestPostgresNewPackageRoutesRequireFixedSDK(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	base := map[string][]byte{}
	for _, file := range pkg.Files {
		base[file.Path] = append([]byte(nil), file.Content...)
	}
	withoutContract := map[string][]byte{}
	for name, content := range base {
		if name != templateContractPath {
			withoutContract[name] = content
		}
	}
	if status, _ := f.uploadPackage(zipFixture(t, withoutContract, 0), f.member); status != 400 {
		t.Fatal("生产旧code-packages入口不能通过删除contract走legacy兼容")
	}
	forged := map[string][]byte{}
	for name, content := range base {
		forged[name] = append([]byte(nil), content...)
	}
	forged[templateSDKPath] = append(forged[templateSDKPath], []byte("\n// 伪造平台脚本")...)
	var contract templateContract
	if err := json.Unmarshal(forged[templateContractPath], &contract); err != nil {
		t.Fatal(err)
	}
	contract.Files[templateSDKPath] = digest(forged[templateSDKPath])
	forged[templateContractPath], _ = json.Marshal(contract)
	forged["index.html"], err = freezeTemplateHTML(forged["index.html"], contract)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := f.uploadPackage(zipFixture(t, forged, 0), f.member); status != 400 {
		t.Fatal("修改SDK并重算contract及HTML摘要仍不能冒充平台历史脚本")
	}
	status, result := f.uploadPackage(zipFixture(t, base, 0), f.member)
	if status != 200 {
		t.Fatalf("平台原版脚本应可导入：%d %v", status, result)
	}
	id := result["data"].(map[string]any)["package"].(map[string]any)["id"].(string)
	if status, _ := f.request("POST", "/tenant/code-packages/"+id+"/revisions", f.member, map[string]any{"name": "伪造核心修订", "files": []any{map[string]any{"path": templateSDKPath, "content": string(forged[templateSDKPath])}, map[string]any{"path": templateContractPath, "content": string(forged[templateContractPath])}, map[string]any{"path": "index.html", "content": string(forged["index.html"])}}}); status != 400 {
		t.Fatal("源码修订不能靠同步修改contract绕过原版SDK来源校验")
	}
	if status, _ := f.request("POST", "/tenant/code-packages/"+id+"/revisions", f.member, map[string]any{"name": "正常外观修订", "files": []any{map[string]any{"path": "static/style.css", "content": string(base["static/style.css"]) + "\n.gc-hero{border-radius:12px}"}}}); status != 200 {
		t.Fatal("正常外观修订应可创建新包")
	}
}

// TestPostgresOfficialExampleRoundTrip 通过真实HTTP下载官方示例，再通过严格生产入口导入完整资源。
func TestPostgresOfficialExampleRoundTrip(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	request, _ := http.NewRequest("GET", f.server.URL+"/api/v1/tenant/code-packages/example", nil)
	request.Header.Set("X-Test-Member", f.member)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, packageZIPLimit+1))
	if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/zip" {
		t.Fatal("官方示例必须返回可用的真实ZIP文件")
	}
	if status, result := f.uploadPackage(raw, f.member); status != 200 {
		t.Fatalf("平台自己提供的静态示例必须能严格导入：%d %v", status, result)
	}
}
