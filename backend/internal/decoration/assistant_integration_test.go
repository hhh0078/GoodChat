// 文件作用说明：在独立测试库以随机租户验证助手私有配置、文本和文件任务、分阶段图片重试及并发草稿保护。
package decoration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// newAssistantFixture 创建只使用固定测试密钥的随机租户，不访问真实供应商或服务私密配置。
func newAssistantFixture(t *testing.T) *integrationFixture {
	f := newIntegrationFixture(t, platform.Config{AuthSecret: strings.Repeat("test-only-assistant-secret-", 2)})
	ctx := context.Background()
	queue := pgx.Identifier{f.queueSchema, "assistant_queue"}.Sanitize()
	var role string
	if err := f.pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		t.Fatal("无法读取测试角色")
	}
	for _, statement := range []string{"CREATE TABLE " + queue + " (LIKE public.assistant_queue INCLUDING ALL)", "ALTER TABLE " + queue + " ADD FOREIGN KEY(tenant_id,job_id) REFERENCES public.assistant_jobs(tenant_id,id) ON DELETE CASCADE", "GRANT SELECT,INSERT,UPDATE,DELETE ON " + queue + " TO " + pgx.Identifier{role}.Sanitize()} {
		if _, err := f.admin.Exec(ctx, statement); err != nil {
			t.Fatal("无法建立助手隔离调度目录")
		}
	}
	f.mustRequest("PUT", "/tenant/assistant/configuration", f.member, map[string]any{"base_url": "https://model.example.com/v1", "model": "tenant-test-model", "api_key": "private-test-key", "enabled": true, "image_model": "image-test-model", "version": 0})
	return f
}

// assistantModelResponse 构造受控供应商响应，所有用量仅为测试协议数据。
func assistantModelResponse(output assistantOutput) *http.Response {
	raw, _ := json.Marshal(output)
	payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}}, "usage": map[string]int{"total_tokens": 20}})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}
}

// assistantFileOutput 复用真正可打包的固定控件HTML，只将普通页面源码交给假模型。
func assistantFileOutput(t *testing.T, withImage bool) assistantOutput {
	t.Helper()
	// 测试假模型只返回固定聊天动作，不夹带没有本地弹框与UI脚本实现的行业说明按钮。
	chat := strings.Replace(templateChatMarkup(), `<button type="button" data-dialog-open="gc-service-notice" class="gc-service-button">服务说明</button>`, "", 1)
	markup := `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>助手测试</title><link rel="stylesheet" href="static/style.css"></head><body>` + chat
	tasks := []assistantImageTask{}
	if withImage {
		markup += `<img src="static/hero.jpg" alt="生成素材">`
		tasks = append(tasks, assistantImageTask{ID: "hero", Path: "static/hero.jpg", Prompt: "绿色自然风格服务插画", Width: 1024, Height: 1024})
	}
	markup += templateScriptsMarkup(false) + `</body></html>`
	return assistantOutput{SchemaVersion: 1, Type: "files", Message: "测试静态页面已生成", Entrypoint: "index.html", Files: []TemplateFile{{Path: "index.html", Content: markup}, {Path: "static/style.css", Content: "/* 文件作用说明：测试静态页面样式。 */ body{color:#245443}"}}, ImageTasks: tasks}
}

// assistantSubmitFixture 原子创建对话和幂等消息，并返回可手动执行的任务编号。
func assistantSubmitFixture(t *testing.T, f *integrationFixture, template string) (string, string) {
	t.Helper()
	thread := f.mustRequest("POST", "/tenant/assistant/threads", f.member, map[string]any{"title": "模板助手集成测试", "source": "tenant", "template_id": template})
	threadID := thread["id"].(string)
	client, _ := newID()
	result := f.mustRequest("POST", "/tenant/assistant/threads/"+threadID+"/messages", f.member, map[string]any{"content": "请生成自然绿色静态咨询模板", "client_message_id": client})
	job := result["job"].(map[string]any)
	again := f.mustRequest("POST", "/tenant/assistant/threads/"+threadID+"/messages", f.member, map[string]any{"content": "请生成自然绿色静态咨询模板", "client_message_id": client})
	if again["job"].(map[string]any)["id"] != job["id"] {
		t.Fatal("相同客户端编号创建了多个任务")
	}
	return threadID, job["id"].(string)
}

// TestPostgresAssistantTextAndIsolation 验证配置不泄密、跨租户拒绝及纯文本答复不创建空模板。
func TestPostgresAssistantTextAndIsolation(t *testing.T) {
	f := newAssistantFixture(t)
	configuration := f.mustRequest("GET", "/tenant/assistant/configuration", f.member, nil)
	if configuration["has_api_key"] != true || configuration["api_key"] != nil {
		t.Fatal("模型配置不应返回密钥")
	}
	other := f.mustRequest("GET", "/tenant/assistant/configuration", f.otherMember, nil)
	if other["configured"] != false {
		t.Fatal("其他租户不应继承密钥")
	}
	if status, _ := f.request("POST", "/tenant/assistant/threads", f.member, map[string]any{"title": "越权使用平台", "source": "platform"}); status != 403 {
		t.Fatal("租户不得使用平台配置")
	}
	var calls atomic.Int32
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer private-test-key" {
			t.Error("私有模型密钥未正确使用")
		}
		return assistantModelResponse(assistantOutput{SchemaVersion: 1, Type: "text", Message: "请描述你希望的页面风格。"}), nil
	})}
	thread, job := assistantSubmitFixture(t, f, "")
	if status, _ := f.request("GET", "/tenant/assistant/threads/"+thread, f.otherMember, nil); status != 404 {
		t.Fatal("跨租户读取对话应返回404")
	}
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	result := f.mustRequest("GET", "/tenant/assistant/jobs/"+job, f.member, nil)
	if result["status"] != "succeeded" || result["design"] != nil || calls.Load() != 1 {
		t.Fatal("文本任务不应生成文件或重复调用")
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM tenant_templates").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatal("纯文本答复创建了空模板")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPostgresAssistantFilesAndImageRetry 验证图片失败只重试图片，成功文件含原版SDK并可真实预览。
func TestPostgresAssistantFilesAndImageRetry(t *testing.T) {
	f := newAssistantFixture(t)
	var textCalls, imageCalls atomic.Int32
	var picture bytes.Buffer
	_ = png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 32, 32)))
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "chat/completions") {
			textCalls.Add(1)
			return assistantModelResponse(assistantFileOutput(t, true)), nil
		}
		attempt := imageCalls.Add(1)
		if attempt == 1 {
			return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("private-provider-error")), Header: make(http.Header)}, nil
		}
		body, _ := json.Marshal(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(picture.Bytes())}}, "usage": map[string]int{"total_tokens": 10}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	thread, job := assistantSubmitFixture(t, f, "")
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	failed := f.mustRequest("GET", "/tenant/assistant/jobs/"+job, f.member, nil)
	if failed["status"] != "failed" || failed["phase"] != "images" {
		t.Fatal("图片失败应保留文本阶段")
	}
	retried := f.mustRequest("POST", "/tenant/assistant/jobs/"+job+"/retry", f.member, map[string]any{})
	oldJob := job
	job = retried["id"].(string)
	if job == oldJob {
		t.Fatal("重试必须使用新任务编号以隔离迟到旧调用")
	}
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	result := f.mustRequest("GET", "/tenant/assistant/threads/"+thread, f.member, nil)
	latest := result["latest_job"].(map[string]any)
	if latest["status"] != "succeeded" || latest["preview_url"] == nil || latest["page_id"] == nil || textCalls.Load() != 1 || imageCalls.Load() != 2 {
		t.Fatalf("阶段重试或预览生成错误：status=%v phase=%v text=%d image=%d", latest["status"], latest["phase"], textCalls.Load(), imageCalls.Load())
	}
	preview := f.renderRequest(latest["preview_url"].(string))
	if preview.Code == http.StatusFound {
		base, _ := url.Parse(latest["preview_url"].(string))
		target, _ := url.Parse(preview.Header().Get("Location"))
		preview = f.renderRequest(base.ResolveReference(target).String())
	}
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), "static/goodchat/sdk.js") {
		t.Fatal("实际静态预览应加载受保护SDK")
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var encrypted string
		if err := tx.QueryRow(context.Background(), "SELECT secret_ciphertext FROM assistant_tenant_ai_settings").Scan(&encrypted); err != nil {
			return err
		}
		if encrypted == "private-test-key" {
			t.Fatal("数据库不得保存明文密钥")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPostgresAssistantConcurrentEditAndCancel 验证模型迟到不会覆盖手工稿，取消排队任务不会调用模型。
func TestPostgresAssistantConcurrentEditAndCancel(t *testing.T) {
	f := newAssistantFixture(t)
	var calls atomic.Int32
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return assistantModelResponse(assistantFileOutput(t, false)), nil
	})}
	_, job := assistantSubmitFixture(t, f, "")
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	original := f.mustRequest("GET", "/tenant/assistant/jobs/"+job, f.member, nil)
	template := original["template_id"].(string)
	_, second := assistantSubmitFixture(t, f, template)
	secondJob := f.mustRequest("GET", "/tenant/assistant/jobs/"+second, f.member, nil)
	copyID := secondJob["base_template_id"].(string)
	if copyID == template {
		t.Fatal("二次创作必须先复制，不能修改原模板")
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE tenant_templates SET name='手工编辑保留',version=version+1 WHERE id=$1", copyID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.processAssistantJob(context.Background(), f.tenant, second); err != nil {
		t.Fatal(err)
	}
	candidate := f.mustRequest("GET", "/tenant/assistant/jobs/"+second, f.member, nil)
	if candidate["status"] != "succeeded" || candidate["applied"] != false || candidate["design"] == nil {
		t.Fatal("并发编辑必须保留候选且不覆盖模板")
	}
	thread, cancelled := assistantSubmitFixture(t, f, "")
	f.mustRequest("POST", "/tenant/assistant/threads/"+thread+"/cancel", f.member, map[string]any{})
	if err := f.service.processAssistantJob(context.Background(), f.tenant, cancelled); err != nil {
		t.Fatal(err)
	}
	result := f.mustRequest("GET", "/tenant/assistant/jobs/"+cancelled, f.member, nil)
	if result["status"] != "cancelled" || calls.Load() != 2 {
		t.Fatal("取消后不应再调用模型")
	}
}

// TestPostgresAssistantPublicSchemaRemix 验证公共旧组件模板可一键复制升级，首次预览可用且来源版本保留。
func TestPostgresAssistantPublicSchemaRemix(t *testing.T) {
	f := newAssistantFixture(t)
	source := "10000000-0000-4000-8000-000000000001"
	created := f.mustRequest("POST", "/tenant/assistant/threads", f.member, map[string]any{"title": "旧组件模板二次创作", "source": "tenant", "template_id": source})
	template := created["template_id"].(string)
	if template == source || created["page_id"] == nil {
		t.Fatal("应原子复制旧公共模板并创建独立预览草稿")
	}
	thread := f.mustRequest("GET", "/tenant/assistant/threads/"+created["id"].(string), f.member, nil)
	if thread["preview_url"] == nil {
		t.Fatal("二次创作开始前应有真实模板预览")
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var mode, sourceID string
		var version int64
		if err := tx.QueryRow(context.Background(), "SELECT render_mode,source_platform_template_id::text,source_platform_version FROM tenant_templates WHERE id=$1", template).Scan(&mode, &sourceID, &version); err != nil {
			return err
		}
		if mode != "code" || sourceID != source || version < 1 {
			t.Fatal("静态升级应保留平台来源版本")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
