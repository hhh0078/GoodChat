// 文件作用说明：在真实PostgreSQL上验证装修租户隔离、不可变发布、并发切换和AI取消及迟到保护。
package decoration

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestPostgresSameOriginPublishLifecycle 验证同域路径的草稿预览、发布与访客入口绑定，不依赖预置模板。
func TestPostgresSameOriginPublishLifecycle(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	f.service.cfg.PublicURL = "https://goodchat.58it.cn"
	f.service.cfg.CodeOrigin = f.service.cfg.PublicURL
	page := f.mustRequest("POST", "/tenant/pages", f.member, map[string]any{"name": "同域装修生命周期"})
	id := page["id"].(string)
	preview := f.mustRequest("POST", "/tenant/pages/"+id+"/preview", f.member, nil)
	if !strings.HasPrefix(preview["preview_url"].(string), "https://goodchat.58it.cn/render/") {
		t.Fatal(preview)
	}
	if got := f.renderRequest(preview["preview_url"].(string)); got.Code != 200 || strings.Contains(got.Header().Get("Content-Security-Policy"), "allow-same-origin") {
		t.Fatal("同域预览没有保持隔离", got.Code)
	}
	published := f.mustRequest("POST", "/tenant/pages/"+id+"/publish", f.member, map[string]any{"version": 1, "expected_release_id": "", "link_ids": []string{f.link}})
	url := f.service.renderURL(published["release_id"].(string), "")
	if got := f.renderRequest(url); got.Code != 200 || !strings.Contains(got.Body.String(), "GoodChat") {
		t.Fatal("同域发布无法访问", got.Code)
	}
	err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		entry, err := f.service.EntryDecoration(context.Background(), tx, f.tenant, f.link)
		if err == nil && entry["render_url"] != url {
			t.Fatal("访客入口未使用同域装修", entry)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPostgresTenantReleaseIsolation 验证真实RLS、链接复合外键、短预览和两次并行发布的互斥保护。
func TestPostgresTenantReleaseIsolation(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	page := f.mustRequest("POST", "/tenant/pages", f.member, map[string]any{"name": "首次装修", "template_id": "10000000-0000-4000-8000-000000000001"})
	id := page["id"].(string)
	status, _ := f.request("GET", "/tenant/pages/"+id, f.otherMember, nil)
	if status != 404 {
		t.Fatalf("其他租户不应读到页面，收到%d", status)
	}
	var unscoped int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM decoration_pages").Scan(&unscoped); err != nil || unscoped != 0 {
		t.Fatal("没有租户上下文时必须看不到装修业务表")
	}
	preview := f.mustRequest("POST", "/tenant/pages/"+id+"/preview", f.member, nil)
	rendered := f.renderRequest(preview["preview_url"].(string))
	if rendered.Code != 200 {
		t.Fatalf("合法预览读取失败：%d", rendered.Code)
	}
	csp := rendered.Header().Get("Content-Security-Policy")
	if policies := rendered.Header().Values("Content-Security-Policy"); len(policies) != 2 || policies[1] != "script-src 'unsafe-inline'" {
		t.Fatal("必须用第二条独立策略阻止带正确nonce的外部脚本")
	}
	for _, directive := range []string{"default-src 'none';", "connect-src 'none';", "img-src data: blob:;", "media-src blob:;", "font-src 'none';", "frame-src 'none';", "form-action 'none';", "frame-ancestors http://127.0.0.1:13000"} {
		if !strings.Contains(csp, directive) {
			t.Fatalf("缺少隔离策略：%s", directive)
		}
	}
	if _, err := f.admin.Exec(ctx, "UPDATE decoration_previews SET expires_at=now()-interval '1 second' WHERE id=$1", preview["release_id"]); err != nil {
		t.Fatal(err)
	}
	if f.renderRequest(preview["preview_url"].(string)).Code != 404 {
		t.Fatal("过期预览凭证必须失效")
	}
	published := f.mustRequest("POST", "/tenant/pages/"+id+"/publish", f.member, map[string]any{"version": 1, "expected_release_id": "", "link_ids": []string{f.link}})
	first := published["release_id"].(string)
	public := f.renderRequest(f.service.renderURL(first, ""))
	if public.Code != 200 {
		t.Fatal("发布静态页应公开读取")
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE decoration_releases SET design='{}' WHERE id=$1", first)
		return err
	}); err == nil {
		t.Fatal("已发布快照必须拒绝原地修改")
	}
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _ := f.request("POST", "/tenant/pages/"+id+"/publish", f.member, map[string]any{"version": 1, "expected_release_id": first, "link_ids": []string{f.link}})
			statuses <- status
		}()
	}
	wg.Wait()
	close(statuses)
	success, conflicts := 0, 0
	for status := range statuses {
		if status == 200 {
			success++
		} else if status == 409 {
			conflicts++
		} else {
			t.Fatalf("并行发布意外状态：%d", status)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("两个并行发布必须恰好成功一个")
	}
	current := f.mustRequest("GET", "/tenant/pages/"+id, f.member, nil)
	currentID := current["current_release_id"].(string)
	edited := f.mustRequest("PATCH", "/tenant/pages/"+id, f.member, map[string]any{"name": "编辑后的草稿", "render_mode": "schema", "draft": defaultDesign("schema", "仅草稿的新标题"), "version": 1})
	if edited["version"] != float64(2) {
		t.Fatal("保存必须增加草稿版本")
	}
	public = f.renderRequest(f.service.renderURL(first, ""))
	if strings.Contains(public.Body.String(), "仅草稿的新标题") {
		t.Fatal("编辑草稿不能改变旧发布快照")
	}
	f.mustRequest("POST", "/tenant/pages/"+id+"/rollback", f.member, map[string]any{"release_id": first, "expected_release_id": currentID})
	rolled := f.mustRequest("GET", "/tenant/pages/"+id, f.member, nil)
	if rolled["current_release_id"] != first || !containsString(rolled["draft"], "仅草稿的新标题") {
		t.Fatal("回滚应只切换线上版本并保留新草稿")
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		entry, err := f.service.EntryDecoration(ctx, tx, f.tenant, f.link)
		if err != nil {
			return err
		}
		if entry["release_id"] != first {
			t.Fatal("入口应使用回滚后的页面版本")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	otherPage := f.mustRequest("POST", "/tenant/pages", f.otherMember, map[string]any{"name": "其他租户页面"})
	otherID := otherPage["id"].(string)
	status, _ = f.request("POST", "/tenant/pages/"+otherID+"/publish", f.otherMember, map[string]any{"version": 1, "expected_release_id": "", "link_ids": []string{f.link}})
	if status != 404 {
		t.Fatalf("发布不能绑定其他租户链接，收到%d", status)
	}
	err := platform.TenantTx(ctx, f.pool, f.other, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE access_links SET page_id=$2 WHERE id=$1", f.otherLink, id)
		return err
	})
	if err == nil {
		t.Fatal("复合外键必须拒绝跨租户页面绑定")
	}
	var publishedCount int
	if err := platform.TenantTx(ctx, f.pool, f.other, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM decoration_releases WHERE page_id=$1", otherID).Scan(&publishedCount)
	}); err != nil || publishedCount != 0 {
		t.Fatal("绑定失败必须回滚整次发布，不能留下半发布")
	}
}

// TestPostgresAliasPreview 验证可信别名读取真实预览、入口链接和CSP父页面，主站会话隔离约束保持不变。
func TestPostgresAliasPreview(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	f.service.cfg.PublicURL = "https://primary.example"
	f.service.cfg.CodeOrigin = f.service.cfg.PublicURL
	f.service.cfg.GatewaySecret = "unit-test-alias-secret"
	page := f.mustRequest("POST", "/tenant/pages", f.member, map[string]any{"name": "多域名预览验收"})
	id := page["id"].(string)
	preview := f.mustRequest("POST", "/tenant/pages/"+id+"/preview", f.member, nil)
	raw := strings.Replace(preview["preview_url"].(string), "https://primary.example", "https://alias.example", 1)
	request := httptest.NewRequest("GET", raw, nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-GoodChat-Gateway-Token", f.service.cfg.GatewaySecret)
	request.Header.Set("X-GoodChat-Public-Origin", "https://alias.example")
	response := httptest.NewRecorder()
	f.service.CodeHandler().ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors https://alias.example") || strings.Contains(response.Header().Get("Content-Security-Policy"), "allow-same-origin") {
		t.Fatalf("别名预览隔离失败，状态%d", response.Code)
	}
}
