// 文件作用说明：在真实PostgreSQL上验证装修租户隔离、不可变发布、并发切换和AI取消及迟到保护。
package decoration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

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

// TestPostgresAIResultCannotOverwrite 验证本地假供应商迟到结果在用户编辑和取消后均不能覆盖私有草稿。
func TestPostgresAIResultCannotOverwrite(t *testing.T) {
	for _, operation := range []string{"edit", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				once.Do(func() { close(started) })
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(defaultDesign("schema", "假模型候选结果"))}}}, "usage": map[string]int{"total_tokens": 120}})
			}))
			defer provider.Close()
			f := newIntegrationFixture(t, platform.Config{AIEndpoint: provider.URL, AIKey: "local-test-only", AIModel: "local-fake-provider"})
			job := f.mustRequest("POST", "/tenant/ai-design-jobs", f.member, map[string]any{"name": "私有AI测试", "prompt": "创建一个绿色的客服咨询页面", "render_mode": "schema"})
			id, templateID := job["id"].(string), job["template_id"].(string)
			tenant, claimed, err := f.service.claimAIJob(context.Background())
			if err != nil || claimed != id || tenant != f.tenant {
				t.Fatal("应从无载荷目录认领当前测试任务")
			}
			done := make(chan error, 1)
			go func() { done <- f.service.processAIJob(context.Background(), tenant, id) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("本地假供应商未收到真实HTTP请求")
			}
			if operation == "edit" {
				f.mustRequest("PATCH", "/tenant/templates/"+templateID, f.member, map[string]any{"name": "手工编辑保留", "render_mode": "schema", "design": defaultDesign("schema", "用户后续有效编辑"), "version": 1})
			} else {
				f.mustRequest("POST", "/tenant/ai-design-jobs/"+id+"/cancel", f.member, nil)
			}
			close(release)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("假供应商任务未完成保存")
			}
			result := f.mustRequest("GET", "/tenant/ai-design-jobs/"+id, f.member, nil)
			if operation == "edit" {
				if result["status"] != "succeeded" || result["applied"] != false || !containsString(result["result"], "假模型候选结果") {
					t.Fatal("编辑后应保留未应用候选而不是覆盖用户草稿")
				}
			} else if result["status"] != "cancelled" || result["applied"] != false {
				t.Fatal("取消任务迟到后必须保持取消终态")
			}
			var design json.RawMessage
			var version int64
			if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(context.Background(), "SELECT design,version FROM tenant_templates WHERE id=$1", templateID).Scan(&design, &version)
			}); err != nil {
				t.Fatal(err)
			}
			if operation == "edit" && (version != 2 || !strings.Contains(string(design), "用户后续有效编辑")) {
				t.Fatal("AI不得覆盖手工编辑")
			}
			if operation == "cancel" && strings.Contains(string(design), "假模型候选结果") {
				t.Fatal("取消后AI结果不得进入私有草稿")
			}
			status, _ := f.request("GET", "/tenant/ai-design-jobs/"+id, f.otherMember, nil)
			if status != 404 {
				t.Fatal("AI任务必须租户隔离")
			}
			if result["billing_status"] != "reported" {
				t.Fatal("取消和并发编辑仍需记录供应商已报告的真实测试用量")
			}
		})
	}
}
