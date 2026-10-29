// 文件作用说明：验证真实数据库中的行业模板导出重导入、绑定阻止删除和共用文件最后引用回收。
package decoration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestTemplateArchiveRoundTrip 验证ZIP保留二进制字节、核心脚本和模板清单，并能重新解析。
func TestTemplateArchiveRoundTrip(t *testing.T) {
	pkg, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encodeTemplateArchive("金融服务", pkg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := readStaticZIP(raw, pkg.Entrypoint)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][]byte{}
	for _, file := range parsed.Files {
		found[file.Path] = file.Content
	}
	for _, file := range pkg.Files {
		if !bytes.Equal(found[file.Path], file.Content) {
			t.Fatalf("导出改变了文件字节：%s", file.Path)
		}
	}
	var metadata templateArchiveMetadata
	if json.Unmarshal(found["template.json"], &metadata) != nil || metadata.Name != "金融服务" || metadata.Entrypoint != pkg.Entrypoint {
		t.Fatal("导出必须携带不含租户身份的模板清单")
	}
}

// TestPostgresTemplateLifecycle 验证链接解绑后原子清理页面，只有最后一个模板引用消失时才回收包字节。
func TestPostgresTemplateLifecycle(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	template := f.mustRequest("POST", "/tenant/template-industries/finance/copy", f.member, nil)
	id := template["id"].(string)
	packageID := template["design"].(map[string]any)["package_id"].(string)
	copy := f.mustRequest("POST", "/tenant/templates/"+id+"/duplicate", f.member, nil)
	copyID := copy["id"].(string)
	page := f.mustRequest("POST", "/tenant/pages", f.member, map[string]any{"name": "删除检查页面", "template_id": id})
	pageID := page["id"].(string)
	f.mustRequest("POST", "/tenant/pages/"+pageID+"/publish", f.member, map[string]any{"version": 1, "expected_release_id": "", "link_ids": []string{f.link}})
	usage := f.mustRequest("GET", "/tenant/templates/"+id+"/usage", f.member, nil)
	if usage["can_delete"] != false || len(usage["links"].([]any)) != 1 {
		t.Fatal("应显示所有仍绑定的链接")
	}
	if status, _ := f.request("DELETE", "/tenant/templates/"+id, f.otherMember, nil); status != 404 {
		t.Fatalf("跨租户删除应为404，收到%d", status)
	}
	if status, _ := f.request("DELETE", "/tenant/templates/"+id, f.member, nil); status != 409 {
		t.Fatalf("绑定模板应拒绝删除，收到%d", status)
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE access_links SET status='disabled' WHERE id=$1", f.link)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if status, _ := f.request("DELETE", "/tenant/templates/"+id, f.member, nil); status != 409 {
		t.Fatal("停用但仍绑定的入口也必须阻止删除")
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE access_links SET page_id=NULL,pinned_release_id=NULL,binding_mode='FOLLOW_PAGE' WHERE id=$1", f.link)
		if err != nil {
			return err
		}
		entry, err := f.service.EntryDecoration(ctx, tx, f.tenant, f.link)
		if err == nil && entry != nil {
			t.Fatal("未绑定时应返回nil并继续使用原来的默认页面")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	deleted := f.mustRequest("DELETE", "/tenant/templates/"+id, f.member, nil)
	if deleted["deleted"] != true || deleted["retained_packages"] != float64(1) {
		t.Fatalf("共用包应保留：%v", deleted)
	}
	var bytesBefore int
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM decoration_package_files WHERE package_id=$1", packageID).Scan(&bytesBefore)
	}); err != nil || bytesBefore == 0 {
		t.Fatal("副本仍使用的资源不能被删除")
	}
	deleted = f.mustRequest("DELETE", "/tenant/templates/"+copyID, f.member, nil)
	if deleted["deleted_packages"] != float64(1) || deleted["deleted_files"].(float64) <= 0 {
		t.Fatalf("最后引用删除后应回收全部包文件：%v", deleted)
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM decoration_package_files WHERE package_id=$1", packageID).Scan(&bytesBefore)
	}); err != nil || bytesBefore != 0 {
		t.Fatal("最后引用消失后不能留下文件字节")
	}
}

// TestPostgresTemplateExportImport 验证指定模板完整下载后可导入新版本，篡改SDK的ZIP被拒绝。
func TestPostgresTemplateExportImport(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	template := f.mustRequest("POST", "/tenant/template-industries/finance/copy", f.member, nil)
	id := template["id"].(string)
	request, _ := http.NewRequest("GET", f.server.URL+"/api/v1/tenant/templates/"+id+"/export", nil)
	request.Header.Set("X-Test-Member", f.member)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("模板导出失败：%d %s", response.StatusCode, raw)
	}
	status, payload := f.importTemplateZIP(raw)
	if status != 200 || payload["data"].(map[string]any)["id"] == id {
		t.Fatalf("导入必须创建新模板：%d %v", status, payload)
	}
	reader, _ := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	files := map[string][]byte{}
	for _, entry := range reader.File {
		stream, _ := entry.Open()
		content, _ := io.ReadAll(stream)
		stream.Close()
		files[entry.Name] = content
	}
	files["static/goodchat/sdk.js"] = append(files["static/goodchat/sdk.js"], []byte("\nwindow.tampered=true;")...)
	status, _ = f.importTemplateZIP(zipFixture(t, files, 0))
	if status != 400 {
		t.Fatalf("篡改核心SDK应拒绝，收到%d", status)
	}
}

// importTemplateZIP 通过真实multipart请求导入导出的模板文件。
func (f *integrationFixture) importTemplateZIP(raw []byte) (int, map[string]any) {
	f.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "template.zip")
	_, _ = file.Write(raw)
	_ = form.Close()
	request, _ := http.NewRequest("POST", f.server.URL+"/api/v1/tenant/templates/import", &body)
	request.Header.Set("X-Test-Member", f.member)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]any
	_ = json.NewDecoder(response.Body).Decode(&payload)
	return response.StatusCode, payload
}

// TestPostgresTemplateRevisionCleanup 验证模板更新包后仍能回收自己持有的旧修订文件。
func TestPostgresTemplateRevisionCleanup(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	template := f.mustRequest("POST", "/tenant/template-industries/finance/copy", f.member, nil)
	id := template["id"].(string)
	packageID := template["design"].(map[string]any)["package_id"].(string)
	revision := f.mustRequest("POST", "/tenant/code-packages/"+packageID+"/revisions", f.member, map[string]any{
		"files": []any{map[string]any{"path": "static/revision-note.txt", "content": "新的模板修订"}},
	})
	f.mustRequest("PATCH", "/tenant/templates/"+id, f.member, map[string]any{"name": "新的修订", "version": 1, "render_mode": "code", "design": revision["design"]})
	deleted := f.mustRequest("DELETE", "/tenant/templates/"+id, f.member, nil)
	if deleted["deleted_packages"] != float64(2) {
		t.Fatalf("旧修订文件应随最后拥有它的模板一起清理：%v", deleted)
	}
}

// TestPostgresTemplateDeleteBindingRace 验证删除等待页面外键锁后会重新发现刚提交的链接绑定。
func TestPostgresTemplateDeleteBindingRace(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	template := f.mustRequest("POST", "/tenant/template-industries/finance/copy", f.member, nil)
	id := template["id"].(string)
	page := f.mustRequest("POST", "/tenant/pages", f.member, map[string]any{"name": "并发绑定页面", "template_id": id})
	pageID := page["id"].(string)
	f.mustRequest("POST", "/tenant/pages/"+pageID+"/publish", f.member, map[string]any{"version": 1, "expected_release_id": "", "link_ids": []string{}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	locked, release := make(chan struct{}), make(chan struct{})
	bound := make(chan error, 1)
	go func() {
		bound <- platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT id FROM decoration_pages WHERE id=$1 FOR KEY SHARE", pageID); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			_, err := tx.Exec(ctx, "UPDATE access_links SET page_id=$2,binding_mode='FOLLOW_PAGE',pinned_release_id=NULL WHERE id=$1", f.link, pageID)
			return err
		})
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("未取得测试绑定锁")
	}
	deleted := make(chan int, 1)
	go func() { status, _ := f.request("DELETE", "/tenant/templates/"+id, f.member, nil); deleted <- status }()
	select {
	case status := <-deleted:
		close(release)
		t.Fatalf("删除不能越过页面引用锁：%d", status)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-bound; err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-deleted:
		if status != 409 {
			t.Fatalf("并发提交的绑定必须阻止删除，收到%d", status)
		}
	case <-ctx.Done():
		t.Fatal("删除未能在绑定提交后返回")
	}
}
