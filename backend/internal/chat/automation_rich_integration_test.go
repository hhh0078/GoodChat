// 文件作用说明：在独立随机PostgreSQL测试schema验证欢迎继承、富内容、版本取消和评价修改历史。
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"testing"
)

// setAutomationPolicy 通过真实管理接口读取并更新完整策略，测试不能跳过并发版本。
func setAutomationPolicy(t *testing.T, f *fixture, changes map[string]any) map[string]any {
	t.Helper()
	status, reply := f.request("GET", "/api/v1/tenant/automation-settings", f.owner, "", nil)
	settings := mustData(t, status, reply)
	for key, value := range changes {
		settings[key] = value
	}
	status, reply = f.request("PUT", "/api/v1/tenant/automation-settings", f.owner, "", settings)
	return mustData(t, status, reply)
}

// uploadWelcomeImage 通过真实附件上传保存有效PNG，不将伪造URL当成图片能力验收。
func uploadWelcomeImage(t *testing.T, f *fixture, member string) string {
	t.Helper()
	var content, body bytes.Buffer
	if err := png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "welcome.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(content.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("POST", f.server.URL+"/api/v1/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-Test-Member", member)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("真实欢迎图片上传失败HTTP%d", response.StatusCode)
	}
	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatal("图片上传缺少实际附件结果")
	}
	return data["id"].(string)
}

// TestRichWelcomeInheritanceImageOptionsAndAgentPolicy 验证唯一覆盖、真实图片、固定选项编号和人工接入取消。
func TestRichWelcomeInheritanceImageOptionsAndAgentPolicy(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	imageID := uploadWelcomeImage(t, f, f.owner)
	status, reply := f.request("POST", "/api/v1/tenant/welcome-rules", f.owner, "", map[string]any{"name": "租户欢迎", "scope_type": "tenant", "scenario": "all", "contents": []any{map[string]any{"type": "text", "content": "租户欢迎"}}})
	mustData(t, status, reply)
	var link, space string
	if err := f.admin.QueryRow(ctx, "SELECT id::text,service_space_id::text FROM access_links WHERE entry_key=$1", f.entry).Scan(&link, &space); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"name": "链接覆盖", "scope_type": "link", "scope_id": link, "scenario": "all", "stop_on_agent": true, "contents": []any{map[string]any{"type": "image", "attachment_id": imageID, "content": "图文欢迎"}, map[string]any{"type": "options", "content": "请选择问题", "options": []any{map[string]any{"id": "pricing", "label": "查看价格", "value": "我想咨询价格"}}}, map[string]any{"type": "text", "content": "延迟内容", "delay_seconds": 60}}}
	status, reply = f.request("POST", "/api/v1/tenant/welcome-rules", f.owner, "", input)
	rule := mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/welcome-preview?access_link_id="+link, f.owner, "", nil)
	preview := mustData(t, status, reply)
	if preview["source"] != "link" {
		t.Fatal("链接覆盖应优先于租户欢迎")
	}
	token, _, conversation := f.visitor("")
	runWorkers(t, f.s, f.tenant)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	messages := mustData(t, status, reply)["items"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["message_type"] != "image" || messages[0].(map[string]any)["attachment"].(map[string]any)["id"] != imageID {
		t.Fatal("欢迎图片未按排序保存为真实授权附件消息")
	}
	options := messages[1].(map[string]any)["options"].([]any)
	option := options[0].(map[string]any)
	client := option["client_message_id"].(string)
	if !uuidPattern.MatchString(client) || option["label"] != "查看价格" || option["value"] != "我想咨询价格" {
		t.Fatal("欢迎选项缺少稳定消息编号或显示发送内容混淆")
	}
	for index := 0; index < 3; index++ {
		status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": client, "content": option["value"], "message_type": "text"})
		mustData(t, status, reply)
	}
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='visitor'", conversation) != 1 {
		t.Fatal("选项连点必须复用普通消息幂等")
	}
	f.online(f.agent)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1 AND status='queued'", conversation); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND content='延迟内容'", conversation) != 0 || f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='cancelled'", conversation) != 1 {
		t.Fatal("默认人工接入后应取消未发引导")
	}
	input["version"] = rule["version"]
	input["enabled"] = false
	status, reply = f.request("PATCH", "/api/v1/tenant/welcome-rules/"+rule["id"].(string), f.owner, "", input)
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/welcome-preview?access_link_id="+link, f.owner, "", nil)
	if mustData(t, status, reply)["source"] != "tenant" {
		t.Fatal("禁用具体规则后应回退有效上层")
	}
	status, _ = f.request("GET", "/api/v1/tenant/welcome-rules", f.agent, "", nil)
	if status != 403 {
		t.Fatal("普通客服不能改欢迎规则")
	}
	status, reply = f.request("POST", "/api/v1/tenant/welcome-rules", f.otherOwner, "", map[string]any{"name": "越权图片", "scope_type": "tenant", "scenario": "all", "contents": []any{map[string]any{"type": "image", "attachment_id": imageID}}})
	if status != 403 {
		t.Fatalf("跨租户图片不应被公开欢迎引用: %d %v", status, reply["code"])
	}
}

// TestEvaluationPolicyVersionsAndDeadline 验证真实截止、管理员禁用、改评并发版本和原始历史不可覆盖。
func TestEvaluationPolicyVersionsAndDeadline(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	path := "/api/v1/conversations/" + conversation + "/evaluation"
	status, reply = f.request("GET", path, "", token, nil)
	state := mustData(t, status, reply)
	if state["can_submit"] != true || state["can_edit"] != false {
		t.Fatal("首次评价状态不正确")
	}
	status, reply = f.request("POST", path, "", token, map[string]any{"score": 2, "comment": "初始意见"})
	first := mustData(t, status, reply)
	status, reply = f.request("POST", path, "", token, map[string]any{"score": 4, "comment": "改评意见", "version": 1})
	if status != 409 || reply["code"] != "evaluation_exists" {
		t.Fatal("默认必须禁止改评")
	}
	setAutomationPolicy(t, f, map[string]any{"evaluation_allow_edit": true, "evaluation_window_hours": 24, "evaluation_edit_window_hours": 2})
	status, reply = f.request("GET", path, "", token, nil)
	if mustData(t, status, reply)["can_edit"] != true {
		t.Fatal("开启改评后当前有效访客应能改评")
	}
	status, reply = f.request("POST", path, "", token, map[string]any{"score": 4, "comment": "改评意见", "version": 1})
	second := mustData(t, status, reply)
	if second["version"] != float64(2) {
		t.Fatal("改评必须增加版本")
	}
	status, reply = f.request("POST", path, "", token, map[string]any{"score": 5, "comment": "旧页覆盖", "version": 1})
	if status != 409 || reply["code"] != "evaluation_version_conflict" {
		t.Fatal("旧版本不得覆盖新评价")
	}
	status, reply = f.request("GET", "/api/v1/tenant/evaluations/"+first["id"].(string)+"/versions", f.owner, "", nil)
	versions := mustData(t, status, reply)["items"].([]any)
	if len(versions) != 2 || versions[1].(map[string]any)["score"] != float64(2) || versions[0].(map[string]any)["score"] != float64(4) {
		t.Fatal("初评和改评必须保留独立原始版本")
	}
	status, _ = f.request("GET", "/api/v1/tenant/evaluations/"+first["id"].(string)+"/versions", f.otherOwner, "", nil)
	if status != 404 {
		t.Fatal("评价历史不得跨租户读取")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET closed_at=now()-interval '25 hours' WHERE id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("GET", path, "", token, nil)
	if mustData(t, status, reply)["can_edit"] != false {
		t.Fatal("结束日期超过提交窗口后必须停止改评")
	}
	status, reply = f.request("POST", path, "", token, map[string]any{"score": 5, "comment": "超期", "version": 2})
	if status != 410 {
		t.Fatal("过期改评必须拒绝")
	}
	status, reply = f.request("POST", path, "", token, map[string]any{"score": 4, "comment": "改评意见", "version": 2})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM evaluation_versions WHERE evaluation_id=$1", first["id"]) != 2 {
		t.Fatal("相同评价重试不能新增历史版本")
	}
}

// TestWelcomeBusinessVersionAndAbsoluteDelays 验证营业预览、空间覆盖、绝对延迟、旧版取消及明确继续人工后欢迎。
func TestWelcomeBusinessVersionAndAbsoluteDelays(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	setAutomationPolicy(t, f, map[string]any{"business_hours_enabled": true, "weekly_hours": []any{map[string]any{"day": 1, "start": "09:00", "end": "18:00"}}})
	var link, space string
	if err := f.admin.QueryRow(ctx, "SELECT id::text,service_space_id::text FROM access_links WHERE entry_key=$1", f.entry).Scan(&link, &space); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"business", "outside"} {
		status, reply := f.request("POST", "/api/v1/tenant/welcome-rules", f.owner, "", map[string]any{"name": scenario, "scope_type": "tenant", "scenario": scenario, "contents": []any{map[string]any{"type": "text", "content": scenario, "delay_seconds": 60}}})
		mustData(t, status, reply)
	}
	for _, test := range []struct{ at, scenario string }{{"2026-11-02T10:00:00%2B08:00", "business"}, {"2026-11-02T22:00:00%2B08:00", "outside"}} {
		status, reply := f.request("GET", "/api/v1/tenant/welcome-preview?access_link_id="+link+"&at="+test.at, f.owner, "", nil)
		data := mustData(t, status, reply)
		if data["rule"].(map[string]any)["scenario"] != test.scenario {
			t.Fatal("营业欢迎场景选择不准确")
		}
	}
	status, reply := f.request("POST", "/api/v1/tenant/welcome-rules", f.owner, "", map[string]any{"name": "空间覆盖", "scope_type": "space", "scope_id": space, "scenario": "all", "contents": []any{map[string]any{"type": "text", "content": "空间内容", "delay_seconds": 60}}})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/welcome-preview?access_link_id="+link+"&at=2026-11-02T22:00:00%2B08:00", f.owner, "", nil)
	if mustData(t, status, reply)["source"] != "space" {
		t.Fatal("服务空间应覆盖较低层级的具体场景")
	}
	setAutomationPolicy(t, f, map[string]any{"weekly_hours": []any{}})
	input := map[string]any{"name": "链接非营业", "scope_type": "link", "scope_id": link, "scenario": "outside", "stop_on_agent": false, "contents": []any{map[string]any{"type": "text", "content": "旧版一", "delay_seconds": 60}, map[string]any{"type": "text", "content": "旧版二", "delay_seconds": 120}}}
	status, reply = f.request("POST", "/api/v1/tenant/welcome-rules", f.owner, "", input)
	rule := mustData(t, status, reply)
	_, _, conversation := f.visitor("")
	if f.countRows("SELECT count(*) FROM chat_operation_jobs j JOIN conversations c ON c.id=j.conversation_id WHERE c.id=$1 AND extract(epoch FROM j.run_after-c.created_at)::int IN(60,120)", conversation) != 2 {
		t.Fatal("每条延迟应相对创建时刻，不累计成180秒")
	}
	input["version"] = rule["version"]
	input["contents"] = []any{map[string]any{"type": "text", "content": "新版一", "delay_seconds": 60}, map[string]any{"type": "text", "content": "新版二", "delay_seconds": 120}}
	status, reply = f.request("PATCH", "/api/v1/tenant/welcome-rules/"+rule["id"].(string), f.owner, "", input)
	updated := mustData(t, status, reply)
	status, _ = f.request("PATCH", "/api/v1/tenant/welcome-rules/"+rule["id"].(string), f.owner, "", input)
	if status != 409 {
		t.Fatal("过期规则版本不能覆盖新内容")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1", conversation) != 0 || f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='cancelled'", conversation) != 2 {
		t.Fatal("旧版本欢迎任务应取消，不能迟到发送")
	}
	f.online(f.agent)
	token, _, next := f.visitor("")
	setAutomationPolicy(t, f, map[string]any{"evaluation_allow_edit": true})
	if _, err := f.admin.Exec(ctx, "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1", next); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, f.s, f.tenant)
	status, reply = f.request("GET", "/api/v1/conversations/"+next+"/messages", "", token, nil)
	messages := mustData(t, status, reply)["items"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["content"] != "新版一" || messages[1].(map[string]any)["content"] != "新版二" {
		t.Fatal("明确继续策略应保持欢迎顺序且不被评价设置变更取消")
	}
	input["version"] = updated["version"]
	input["enabled"] = false
	status, reply = f.request("PATCH", "/api/v1/tenant/welcome-rules/"+rule["id"].(string), f.owner, "", input)
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/welcome-preview?access_link_id="+link, f.owner, "", nil)
	if mustData(t, status, reply)["source"] != "space" {
		t.Fatal("具体规则禁用后应回退空间规则")
	}
}
