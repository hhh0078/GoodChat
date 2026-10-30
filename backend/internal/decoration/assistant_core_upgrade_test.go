// 文件作用说明：验证AI二创只在新副本升级固定核心，旧私有及平台资源的脚本、SRI和素材不会被覆写。
package decoration

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// assistantOldCoreFixture 构造没有新留言评价方法的已冻结历史包，包含独立PNG素材供保真校验。
func assistantOldCoreFixture(t *testing.T, version string) (*staticPackage, TemplateScripts) {
	t.Helper()
	base, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	files, assets, err := assistantSplitBasePackage(base)
	if err != nil {
		t.Fatal(err)
	}
	for i := range files {
		if files[i].Path == "index.html" {
			start := strings.Index(files[i].Content, `<header class="gc-brandbar finance-brand"`)
			if start < 0 {
				t.Fatal("测试模板头部不存在")
			}
			end := strings.Index(files[i].Content[start:], `</header>`)
			if end < 0 {
				t.Fatal("测试模板头部没有闭合")
			}
			files[i].Content = files[i].Content[:start] + files[i].Content[start+end+len(`</header>`):]
		}
	}
	old := defaultTemplateScripts()
	old.Version = version
	old.SDK = strings.NewReplacer("openLeaveMessage", "legacyLeaveMessage", "openEvaluation", "legacyEvaluation").Replace(old.SDK)
	old.ChatUI = strings.NewReplacer("openLeaveMessage", "legacyLeaveMessage", "openEvaluation", "legacyEvaluation").Replace(old.ChatUI)
	image, err := checkStaticFile("static/brand.png", packageFixtureFiles(t)["site/logo.png"])
	if err != nil {
		t.Fatal(err)
	}
	// 历史文件按原已冻结字节构造，故意不经过只用于新保存foundation的必需新方法检查。
	contract := templateContract{SchemaVersion: 1, SDKVersion: old.Version, Files: map[string]string{templateSDKPath: digest([]byte(old.SDK)), templateUIPath: digest([]byte(old.ChatUI))}}
	all := append([]packageFile{image}, assets...)
	for _, source := range files {
		content := []byte(source.Content)
		if source.Path == "index.html" {
			content, err = freezeTemplateHTML(content, contract)
			if err != nil {
				t.Fatal(err)
			}
		}
		file, err := checkStaticFile(source.Path, content)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, file)
	}
	raw, _ := json.Marshal(contract)
	for path, content := range map[string][]byte{templateSDKPath: []byte(old.SDK), templateUIPath: []byte(old.ChatUI), templateContractPath: raw} {
		file, err := checkStaticFile(path, content)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, file)
	}
	pkg, err := finishPackage("index.html", all)
	if err != nil {
		t.Fatal(err)
	}
	return pkg, old
}

// TestAssistantCoreUpgradeKeepsOriginal 重新注入当前核心及SRI只产生新包，品牌文案、PNG字节和原始核心保持不变。
func TestAssistantCoreUpgradeKeepsOriginal(t *testing.T) {
	old, _ := assistantOldCoreFixture(t, "legacy-core-upgrade")
	before := templateFileMap(old)
	upgraded, err := assistantRebuildCorePackage(old, defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	after := templateFileMap(upgraded)
	contract, err := validateTemplatePackage(upgraded.Entrypoint, after, true)
	if err != nil || !templateContractMatches(contract, defaultTemplateScripts()) {
		t.Fatal("新副本未冻结当前核心", err)
	}
	if old == upgraded || old.SHA256 == upgraded.SHA256 || !bytes.Equal(before["static/brand.png"].Content, after["static/brand.png"].Content) {
		t.Fatal("新包必须独立并保留二进制素材")
	}
	for _, file := range old.Files {
		if !bytes.Equal(file.Content, before[file.Path].Content) {
			t.Fatal("原包字节被升级副本修改", file.Path)
		}
		if !strings.HasPrefix(file.ContentType, "text/") && file.ContentType != "application/json" && !bytes.Equal(file.Content, after[file.Path].Content) {
			t.Fatal("新副本必须保留原包的所有二进制素材", file.Path)
		}
	}
	if !strings.Contains(string(after["index.html"].Content), `data-goodchat-version="`+defaultTemplateScripts().Version+`"`) || strings.Contains(string(after["index.html"].Content), "legacy-core-upgrade") {
		t.Fatal("交给模型的HTML仍保留旧版本/SRI")
	}
	if _, err := assistantRebuildCorePackage(nil, defaultTemplateScripts()); err == nil {
		t.Fatal("空基础包不能被当作升级成功")
	}
	broken := *old
	broken.Files = append([]packageFile(nil), old.Files...)
	for i := range broken.Files {
		if broken.Files[i].Path == templateSDKPath {
			broken.Files[i].Content = append([]byte("tampered"), broken.Files[i].Content...)
		}
	}
	if _, err := assistantRebuildCorePackage(&broken, defaultTemplateScripts()); err == nil {
		t.Fatal("不能先剥离篡改的旧SDK来掩盖历史合约错误")
	}
}

// TestPostgresAssistantRemixUpgradesProtectedCore 从私有与已发布平台包发起真实二创API，副本和模型上下文使用当前核心且原包哈希不变。
func TestPostgresAssistantRemixUpgradesProtectedCore(t *testing.T) {
	f := newAssistantFixture(t)
	ctx := context.Background()
	unique, _ := newID()
	old, legacyScripts := assistantOldCoreFixture(t, "legacy-actions-"+unique)
	var versions []int64
	t.Cleanup(func() {
		if len(versions) == 0 {
			return
		}
		if err := pgx.BeginFunc(context.Background(), f.admin, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(), "SET LOCAL session_replication_role=replica"); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), "DELETE FROM assistant_contract_versions WHERE version=ANY($1::bigint[])", versions)
			return err
		}); err != nil {
			t.Error("独立测试创建的核心版本清理失败", err)
		}
	})
	for _, scripts := range []TemplateScripts{legacyScripts, defaultTemplateScripts()} {
		raw, _ := json.Marshal(scripts)
		var version int64
		if err := f.admin.QueryRow(ctx, "INSERT INTO assistant_contract_versions(prompt,scripts) VALUES($1,$2) RETURNING version", "仅供本测试的固定核心快照", raw).Scan(&version); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, version)
	}
	var privatePackageID string
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		stored, err := f.service.storePackage(ctx, tx, f.principals[f.member], "历史固定核心", old)
		if err != nil {
			return err
		}
		privatePackageID, err = storedPackageID(stored)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	private := f.mustRequest("POST", "/tenant/templates", f.member, map[string]any{"name": "历史核心私有模板", "render_mode": "code", "design": CodeDesign{PackageID: privatePackageID}})
	var publicID, publicPackageID string
	if err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		raw, err := savePlatformPackageTemplate(ctx, tx, "历史核心平台模板", "published", "", old)
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
		publicID, publicPackageID = item.ID, item.Design.PlatformPackageID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupPlatformFixture(t, f, []string{publicID}) })
	for _, source := range []string{private["id"].(string), publicID} {
		thread := f.mustRequest("POST", "/tenant/assistant/threads", f.member, map[string]any{"title": "固定核心升级副本", "source": "tenant", "template_id": source})
		if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			var design json.RawMessage
			if err := tx.QueryRow(ctx, "SELECT design FROM tenant_templates WHERE id=$1", thread["template_id"]).Scan(&design); err != nil {
				return err
			}
			var code CodeDesign
			_ = json.Unmarshal(design, &code)
			pkg, err := loadArchivePackage(ctx, tx, code)
			if err != nil {
				return err
			}
			contract, err := validateTemplatePackage(pkg.Entrypoint, templateFileMap(pkg), true)
			if err != nil || !templateContractMatches(contract, defaultTemplateScripts()) || code.PackageID == privatePackageID {
				t.Fatal("二创API没有创建当前核心的独立包", err)
			}
			files, _, err := assistantBaseFiles(ctx, tx, design, defaultTemplateScripts())
			if err != nil {
				return err
			}
			for _, file := range files {
				if strings.Contains(file.Content, legacyScripts.Version) || isProtectedTemplatePath(file.Path) {
					t.Fatal("模型上下文不能带旧核心版本或核心原始文件")
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []struct{ table, id string }{{"decoration_code_packages", privatePackageID}, {"platform_template_packages", publicPackageID}} {
		var hash string
		if err := f.admin.QueryRow(ctx, "SELECT sha256 FROM "+source.table+" WHERE id=$1", source.id).Scan(&hash); err != nil || hash != old.SHA256 {
			t.Fatal("二创不得修改任何原始包", err)
		}
	}
}
