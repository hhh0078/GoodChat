// 文件作用说明：保证全部装修集成夹具的调度目录物理隔离，不被运行中的真实AI worker认领。
package decoration

import (
	"context"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestPostgresAIQueuePhysicalIsolation 验证多条测试连接共用随机schema，正式公共调度目录始终没有测试任务。
func TestPostgresAIQueuePhysicalIsolation(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{AIEndpoint: "http://127.0.0.1:1", AIKey: "test-only", AIModel: "not-called-test"})
	ctx := context.Background()
	job := f.mustRequest("POST", "/tenant/ai-design-jobs", f.member, map[string]any{"name": "物理隔离任务", "prompt": "创建自然绿色客服页面", "render_mode": "schema"})
	var public, isolated int
	if err := f.admin.QueryRow(ctx, "SELECT count(*) FROM public.ai_design_queue WHERE tenant_id=$1", f.tenant).Scan(&public); err != nil || public != 0 {
		t.Fatal("真实公共worker不能看到任何测试调度任务")
	}
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM ai_design_queue WHERE tenant_id=$1", f.tenant).Scan(&isolated); err != nil || isolated != 1 {
		t.Fatal("隔离调度目录应持有真实提交的任务")
	}
	first, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatal("无法验证第一条测试连接")
	}
	defer first.Release()
	second, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatal("无法验证第二条测试连接")
	}
	defer second.Release()
	var one, two string
	query := "SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid='ai_design_queue'::regclass"
	if first.QueryRow(ctx, query).Scan(&one) != nil || second.QueryRow(ctx, query).Scan(&two) != nil || one != f.queueSchema || two != f.queueSchema {
		t.Fatal("每条测试连接都必须解析到同一隔离调度目录")
	}
	first.Release()
	second.Release()
	tenant, id, err := f.service.claimAIJob(ctx)
	if err != nil || tenant != f.tenant || id != job["id"] {
		t.Fatal("真实认领SQL应只处理当前夹具目录")
	}
}

// TestPostgresAIPackageEditIsExplicit 拒绝静默丢弃资源依赖的AI改稿，保留源码修订与另建内联草稿路径。
func TestPostgresAIPackageEditIsExplicit(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{AIEndpoint: "http://127.0.0.1:1", AIKey: "test-only", AIModel: "not-called-test"})
	data := f.mustUploadPackage(packageFixtureFiles(t))
	template := f.mustRequest("POST", "/tenant/templates", f.member, map[string]any{"name": "私有包模板", "render_mode": "code", "design": data["design"]})
	status, result := f.request("POST", "/tenant/ai-design-jobs", f.member, map[string]any{"name": "资源包改稿", "prompt": "修改当前资源包颜色", "render_mode": "code", "template_id": template["id"], "base_version": 1})
	if status != 409 || result["code"] != "ai_package_edit_requires_source" {
		t.Fatal("资源包AI改稿不应静默替换成丢依赖的内联设计")
	}
	configuration := f.mustRequest("GET", "/tenant/ai-configuration", f.member, nil)
	if configuration["used_today"] != float64(0) {
		t.Fatal("拒绝未支持的包改稿不能占AI次数")
	}
}
