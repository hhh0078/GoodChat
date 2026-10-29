// 文件作用说明：验证模板点击预览不会生成永久页面或资源包，临时静态文件受到租户、凭证和过期边界保护。
package decoration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"supportdesk/backend/internal/platform"
)

// templatePreviewTestAuth 只为本测试提供显式平台身份，仍保留正常租户管理员鉴权。
type templatePreviewTestAuth struct{ integrationAuth }

// RequirePlatform 在测试范围检查平台管理员标记，普通租户管理员不能预览隐藏公共模板。
func (a templatePreviewTestAuth) RequirePlatform(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireTenant(func(w http.ResponseWriter, r *http.Request) {
		if !platform.Current(r).PlatformAdmin {
			platform.ErrorJSON(w, r, platform.Fail(403, "platform_required", "需要平台管理员权限"))
			return
		}
		next(w, r)
	})
}

// TestPostgresTemplateDirectPreviews 验证行业、私有、公共模板可以直接隔离预览，且不会隐式复制或创建页面。
func TestPostgresTemplateDirectPreviews(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	private := f.mustRequest("POST", "/tenant/template-industries/finance/copy", f.member, nil)
	privateID := private["id"].(string)
	var initialTemplates, initialPages, initialPackages int
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_templates),(SELECT count(*) FROM decoration_pages),(SELECT count(*) FROM decoration_code_packages)`).Scan(&initialTemplates, &initialPages, &initialPackages)
	}); err != nil {
		t.Fatal(err)
	}
	var firstID, firstURL string
	for _, industry := range industryTemplateCatalog() {
		preview := f.mustRequest("POST", "/tenant/template-industries/"+industry.Key+"/preview", f.member, nil)
		previewURL := preview["preview_url"].(string)
		if preview["expires_at"] == nil {
			t.Fatal("预览应返回有效期")
		}
		entry := f.packageEntryURL(previewURL)
		response := f.renderRequest(entry)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "gc-industry-"+industry.Key) || !strings.Contains(response.Body.String(), `"preview":true`) {
			t.Fatalf("行业%s未使用真实静态模板和模拟SDK：%d", industry.Key, response.Code)
		}
		css := f.renderRequest(staticRelative(entry, "static/style.css"))
		if css.Code != http.StatusOK || !strings.Contains(css.Body.String(), ".gc-capsule") {
			t.Fatal("行业预览必须能读取同快照的样式")
		}
		if scripts := f.renderRequest(staticRelative(entry, templateSDKPath)); scripts.Code != http.StatusOK || !strings.Contains(scripts.Body.String(), "GoodChat") {
			t.Fatal("行业预览必须保留受保护SDK")
		}
		parsed, _ := url.Parse(entry)
		if strings.Contains(parsed.Path, "/preview-assets/") {
			parts := strings.SplitN(strings.TrimPrefix(parsed.Path, "/preview-assets/"), "/", 3)
			if len(parts) != 3 {
				t.Fatal("临时资源路径缺少版本和令牌")
			}
			parsed.Path = "/assets/" + parts[0] + "/" + parts[2]
			if f.renderRequest(parsed.String()).Code != http.StatusNotFound {
				t.Fatal("短期文件不能通过公开发布路径读取")
			}
		}
		if firstID == "" {
			firstID, firstURL = preview["release_id"].(string), entry
		}
	}
	for _, templateID := range []string{privateID, "10000000-0000-4000-8000-000000000001"} {
		preview := f.mustRequest("POST", "/tenant/templates/"+templateID+"/preview", f.member, nil)
		response := f.renderRequest(preview["preview_url"].(string))
		if response.Code == http.StatusFound {
			response = f.renderRequest(f.packageEntryURL(preview["preview_url"].(string)))
		}
		if response.Code != http.StatusOK {
			t.Fatalf("模板直接预览失败：%d", response.Code)
		}
	}
	if status, _ := f.request("POST", "/tenant/templates/"+privateID+"/preview", f.otherMember, nil); status != http.StatusNotFound {
		t.Fatalf("其他租户不能预览私有模板：%d", status)
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		var templates, pages, packages int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_templates),(SELECT count(*) FROM decoration_pages),(SELECT count(*) FROM decoration_code_packages)`).Scan(&templates, &pages, &packages); err != nil {
			return err
		}
		if templates != initialTemplates || pages != initialPages || packages != initialPackages {
			t.Fatal("点击预览不应产生永久模板、页面或资源包")
		}
		var foreignFiles int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM decoration_preview_files WHERE tenant_id<>$1", f.tenant).Scan(&foreignFiles); err != nil {
			return err
		}
		if foreignFiles != 0 {
			t.Fatal("临时文件必须经过租户RLS")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE decoration_previews SET expires_at=now()-interval '1 second' WHERE id=$1", firstID); err != nil {
		t.Fatal(err)
	}
	if f.renderRequest(firstURL).Code != http.StatusNotFound {
		t.Fatal("过期凭证必须同时阻止静态资源")
	}
	f.mustRequest("POST", "/tenant/template-industries/education/preview", f.member, nil)
	var filesAfter int
	if err := f.admin.QueryRow(ctx, "SELECT count(*) FROM decoration_preview_files WHERE preview_id=$1", firstID).Scan(&filesAfter); err != nil || filesAfter != 0 {
		t.Fatal("新建预览清理已过期快照时应级联删除临时字节")
	}
}

// TestPostgresHiddenPlatformTemplatePreview 验证隐藏模板只向平台管理员提供短期预览，普通客服不可创建预览。
func TestPostgresHiddenPlatformTemplatePreview(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	pkg, err := industryTemplatePackage("medical", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	var publicID, packageID string
	if err := pgx.BeginFunc(ctx, f.admin, func(tx pgx.Tx) error {
		raw, err := savePlatformPackageTemplate(ctx, tx, "隐藏预览验收", "hidden", "", pkg)
		if err != nil {
			return err
		}
		var result struct {
			ID     string     `json:"id"`
			Design CodeDesign `json:"design"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return err
		}
		publicID, packageID = result.ID, result.Design.PlatformPackageID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 测试只清理本用例新建的平台快照，不触碰任何已存在的公共模板。
	t.Cleanup(func() {
		if err := pgx.BeginFunc(ctx, f.admin, func(tx pgx.Tx) error {
			for _, statement := range []string{"SET LOCAL session_replication_role=replica", "DELETE FROM platform_template_versions WHERE template_id=$1", "DELETE FROM platform_templates WHERE id=$1", "DELETE FROM platform_template_package_files WHERE package_id=$1", "DELETE FROM platform_template_packages WHERE id=$1"} {
				var err error
				if strings.HasPrefix(statement, "SET ") {
					_, err = tx.Exec(ctx, statement)
				} else {
					id := publicID
					if strings.Contains(statement, "platform_template_package") {
						id = packageID
					}
					_, err = tx.Exec(ctx, statement, id)
				}
				if err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Errorf("无法清理本用例平台模板：%v", err)
		}
	})
	if status, _ := f.request("POST", "/tenant/templates/"+publicID+"/preview", f.member, nil); status != http.StatusNotFound {
		t.Fatal("租户模板列表外的隐藏公共模板不能被猜号预览")
	}
	f.server.Close()
	p := f.principals[f.member]
	p.PlatformAdmin = true
	f.principals[f.member] = p
	agent := p
	agent.Role = "agent"
	agent.PlatformAdmin = false
	f.principals["preview-agent"] = agent
	f.service.auth = templatePreviewTestAuth{integrationAuth{f.principals}}
	mux := http.NewServeMux()
	f.service.Register(mux)
	f.server = httptest.NewServer(mux)
	if status, _ := f.request("POST", "/platform/templates/"+publicID+"/preview", f.otherMember, nil); status != http.StatusForbidden {
		t.Fatal("租户管理员不能使用平台隐藏模板预览入口")
	}
	preview := f.mustRequest("POST", "/platform/templates/"+publicID+"/preview", f.member, nil)
	response := f.renderRequest(f.packageEntryURL(preview["preview_url"].(string)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "gc-industry-medical") {
		t.Fatal("平台管理员应能隔离预览隐藏模板")
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		var originalID string
		if err := tx.QueryRow(ctx, "SELECT COALESCE(design->>'platform_package_id','') FROM decoration_previews WHERE id=$1", preview["release_id"]).Scan(&originalID); err != nil {
			return err
		}
		if originalID != "" {
			t.Fatal("平台包预览必须冻结临时文件，不能持久引用公共包编号")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if status, _ := f.request("POST", "/tenant/template-industries/finance/preview", "preview-agent", nil); status != http.StatusForbidden {
		t.Fatal("客服角色不能创建行业预览")
	}
}

// TestPostgresLegacyTemplatePreviewUsesMockSDK 验证五套旧结构模板、内联与旧静态包都临时升级为同一可交互模拟SDK。
func TestPostgresLegacyTemplatePreviewUsesMockSDK(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	inline := f.mustRequest("POST", "/tenant/templates", f.member, map[string]any{
		"name": "内联旧样式预览", "render_mode": "code",
		"design": CodeDesign{HTML: `<h1 class="legacy-preview-title">保留内联旧样式</h1>`, CSS: `.legacy-preview-title{color:#785433}`, Javascript: `window.GoodChat;window.legacyPreviewMarker='must-not-run';`},
	})
	legacyFiles := packageFixtureFiles(t)
	legacyFiles["site/index.html"] = []byte(`<!doctype html><html><head><link rel="stylesheet" href="./css/main.css"></head><body><h1>资源包中文</h1>` + legacyAcceptanceControls + `<button class="old-option" data-period="6">分期6个月</button><img src="./logo.png" alt="Logo"><script type="module" src="./js/main.mjs"></script></body></html>`)
	legacy := f.mustPersistLegacyPackage(legacyFiles)
	legacyTemplate := f.mustRequest("POST", "/tenant/templates", f.member, map[string]any{"name": "既有旧资源预览", "render_mode": "code", "design": legacy["design"]})
	var templatesBefore, pagesBefore, packagesBefore int
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_templates),(SELECT count(*) FROM decoration_pages),(SELECT count(*) FROM decoration_code_packages)`).Scan(&templatesBefore, &pagesBefore, &packagesBefore)
	}); err != nil {
		t.Fatal(err)
	}
	ids := []string{inline["id"].(string), legacyTemplate["id"].(string)}
	for n := 1; n <= 5; n++ {
		ids = append(ids, fmt.Sprintf("10000000-0000-4000-8000-00000000000%d", n))
	}
	for _, id := range ids {
		preview := f.mustRequest("POST", "/tenant/templates/"+id+"/preview", f.member, nil)
		entry := f.packageEntryURL(preview["preview_url"].(string))
		html := f.renderRequest(entry)
		if html.Code != http.StatusOK || !strings.Contains(html.Body.String(), `"preview":true`) || !strings.Contains(html.Body.String(), templateSDKPath) || !strings.Contains(html.Body.String(), templateUIPath) {
			t.Fatalf("旧模板必须用受保护静态SDK模拟分支预览：%s %d", id, html.Code)
		}
		if strings.Contains(html.Body.String(), "布局预览 · 此页面不连接真实访客会话") || strings.Contains(html.Body.String(), "legacyPreviewMarker") {
			t.Fatal("旧预览禁用提示与旧聊天脚本不能继续用于交互预览")
		}
		sdk := f.renderRequest(staticRelative(entry, templateSDKPath))
		if sdk.Code != http.StatusOK || !strings.Contains(sdk.Body.String(), "preview-welcome") || !strings.Contains(sdk.Body.String(), "ready: () => Promise.resolve()") {
			t.Fatal("旧模板应使用包含模拟消息且立即就绪的同一固定SDK")
		}
		if id == inline["id"] {
			css := f.renderRequest(staticRelative(entry, "static/upgrade.css"))
			if !strings.Contains(html.Body.String(), "保留内联旧样式") || css.Code != http.StatusOK || !strings.Contains(css.Body.String(), ".legacy-preview-title{color:#785433}") {
				t.Fatal("临时升级应保留旧模板静态文案与样式")
			}
		}
		if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			var temporary bool
			if err := tx.QueryRow(ctx, "SELECT page_id IS NULL AND package_id IS NULL AND ephemeral_entrypoint IS NOT NULL FROM decoration_previews WHERE id=$1", preview["release_id"]).Scan(&temporary); err != nil {
				return err
			}
			if !temporary {
				t.Fatal("旧模板转换只能生成TTL临时快照")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		var templates, pages, packages int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM tenant_templates),(SELECT count(*) FROM decoration_pages),(SELECT count(*) FROM decoration_code_packages)`).Scan(&templates, &pages, &packages); err != nil {
			return err
		}
		if templates != templatesBefore || pages != pagesBefore || packages != packagesBefore {
			t.Fatal("旧模板交互预览不能创建永久模板、页面或包")
		}
		var originalJS string
		if err := tx.QueryRow(ctx, "SELECT design->>'javascript' FROM tenant_templates WHERE id=$1", inline["id"]).Scan(&originalJS); err != nil {
			return err
		}
		if !strings.Contains(originalJS, "legacyPreviewMarker") {
			t.Fatal("兼容预览不能覆盖旧模板原始设计")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPostgresTemplatePreviewDeletion 验证直接预览随来源模板删除，共享包仅在最后一个模板及其预览消失后回收。
func TestPostgresTemplatePreviewDeletion(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared_%t", shared), func(t *testing.T) {
			f := newIntegrationFixture(t, platform.Config{})
			ctx := context.Background()
			template := f.mustRequest("POST", "/tenant/template-industries/finance/copy", f.member, nil)
			id := template["id"].(string)
			packageID := template["design"].(map[string]any)["package_id"].(string)
			if !shared {
				err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
					_, err := tx.Exec(ctx, `INSERT INTO decoration_previews(tenant_id,render_mode,design,token_hash) VALUES($1,'code',jsonb_build_object('package_id',$2::text),$3)`, f.tenant, packageID, digest([]byte("missing-source-preview-fixture")))
					return err
				})
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "23514" || databaseError.ConstraintName != "preview_direct_private_source" {
					t.Fatal("应用角色不能插入无页面、无来源的私包直接预览")
				}
			}
			preview := f.mustRequest("POST", "/tenant/templates/"+id+"/preview", f.member, nil)
			entry := f.packageEntryURL(preview["preview_url"].(string))
			var sourceID string
			if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, "SELECT source_template_id::text FROM decoration_previews WHERE id=$1", preview["release_id"]).Scan(&sourceID)
			}); err != nil || sourceID != id {
				t.Fatal("私有直接预览必须绑定本租户来源模板")
			}
			var otherID, otherPreviewURL string
			if shared {
				other := f.mustRequest("POST", "/tenant/templates/"+id+"/duplicate", f.member, nil)
				otherID = other["id"].(string)
				otherPreview := f.mustRequest("POST", "/tenant/templates/"+otherID+"/preview", f.member, nil)
				otherPreviewURL = f.packageEntryURL(otherPreview["preview_url"].(string))
			}
			deleted := f.mustRequest("DELETE", "/tenant/templates/"+id, f.member, nil)
			if f.renderRequest(entry).Code != http.StatusNotFound || f.renderRequest(preview["preview_url"].(string)).Code != http.StatusNotFound {
				t.Fatal("删除来源模板后旧预览入口及静态资源必须立即失效")
			}
			if shared {
				if deleted["retained_packages"] != float64(1) || f.renderRequest(otherPreviewURL).Code != http.StatusOK {
					t.Fatal("删除来源1不能破坏来源2仍使用的共享包和预览")
				}
				deleted = f.mustRequest("DELETE", "/tenant/templates/"+otherID, f.member, nil)
				if f.renderRequest(otherPreviewURL).Code != http.StatusNotFound {
					t.Fatal("删除最后共享模板后其旧预览也必须撤销")
				}
			}
			if deleted["deleted_packages"] != float64(1) || deleted["deleted_files"].(float64) <= 0 {
				t.Fatal("来源预览不能阻止原有生命周期回收最后无引用私包")
			}
			if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
				var previews, packages, files int
				if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM decoration_previews WHERE source_template_id=ANY($1::uuid[])),(SELECT count(*) FROM decoration_code_packages WHERE id=$2),(SELECT count(*) FROM decoration_package_files WHERE package_id=$2)`, []string{id}, packageID).Scan(&previews, &packages, &files); err != nil {
					return err
				}
				if previews != 0 || packages != 0 || files != 0 {
					t.Fatal("删除最后来源后预览、私包与文件均应归零")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestPostgresConvertedPreviewDeletion 验证旧模板转换产生的临时文件也随来源删除，不等TTL造成残留。
func TestPostgresConvertedPreviewDeletion(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	template := f.mustRequest("POST", "/tenant/templates", f.member, map[string]any{"name": "转换预览删除", "render_mode": "schema", "design": defaultDesign("schema", "转换预览删除")})
	id := template["id"].(string)
	preview := f.mustRequest("POST", "/tenant/templates/"+id+"/preview", f.member, nil)
	entry := f.packageEntryURL(preview["preview_url"].(string))
	var before int
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM decoration_preview_files WHERE preview_id=$1", preview["release_id"]).Scan(&before)
	}); err != nil || before == 0 {
		t.Fatal("转换预览应先建立临时静态文件")
	}
	f.mustRequest("DELETE", "/tenant/templates/"+id, f.member, nil)
	if f.renderRequest(entry).Code != http.StatusNotFound {
		t.Fatal("转换预览的来源删除后旧URL必须失效")
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		var previews, files int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM decoration_previews WHERE id=$1),(SELECT count(*) FROM decoration_preview_files WHERE preview_id=$1)`, preview["release_id"]).Scan(&previews, &files); err != nil {
			return err
		}
		if previews != 0 || files != 0 {
			t.Fatal("转换预览与临时字节应在同一删除事务中级联回收")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
