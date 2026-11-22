// 文件作用说明：显式使用已授权私有供应商验证默认AI员工、真实表单选项、知识缺口去重与补充后再回答，不使用模拟模型。
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"supportdesk/backend/internal/localization"
	"testing"
)

// configureRealEmployee 为隔离租户配置真实模型和默认AI，不接触生产业务数据或输出密钥。
func configureRealEmployee(t *testing.T, f *fixture) string {
	t.Helper()
	previous := f.server
	previous.Close()
	f.server = httptest.NewServer(localization.Middleware(previous.Config.Handler))
	t.Cleanup(f.server.Close)
	raw, err := os.ReadFile(os.Getenv("GOODCHAT_REAL_AI_CONFIG"))
	if err != nil {
		t.Fatal("无法读取私有配置")
	}
	var c struct{ Endpoint, Key, Model string }
	if json.Unmarshal(raw, &c) != nil || c.Key == "" {
		t.Fatal("私有配置无效")
	}
	ctx := context.Background()
	f.s.cfg.AuthSecret = strings.Repeat("real-employee-test-", 3)
	f.s.cfg.CallbackAllowLocal = true
	id := newID()
	m := aiModel{ID: id, Name: "真实模型隔离验收", BaseURL: strings.TrimSuffix(c.Endpoint, "/chat/completions"), Model: c.Model, Enabled: true, Version: 1, CostInput: 100000, CostOutput: 300000, SaleInput: 300000, SaleOutput: 600000, SupportsImages: true, MaxInput: 64000, MaxOutput: 4096}
	secret, err := f.s.aiSecret(c.Key, id, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		sql  string
		args []any
	}{{"INSERT INTO ai_chat_models(id,name,enabled) VALUES($1,'隔离真实模型',true)", []any{id}}, {"INSERT INTO ai_chat_model_versions(model_id,version,config,secret_ciphertext) VALUES($1,1,$2,$3)", []any{id, jsonValue(m), secret}}, {"INSERT INTO ai_wallets(tenant_id,balance_micro) VALUES($1,100000000)", []any{f.tenant}}} {
		if _, err = f.admin.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	b := defaultAIBot()
	b.ModelID = id
	b.Mode = "auto"
	b.Settings.MaxOutput = 4096
	b.Settings.MaxTurns = 30
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	bot := mustData(t, status, data)["id"].(string)
	status, data = f.request("PUT", "/api/v1/tenant/ai/settings", f.owner, "", map[string]any{"default_bot_id": bot, "reception_mode": "ai_first"})
	mustData(t, status, data)
	return bot
}

// TestAIRealEmployeeFormsAndKnowledgeGap 从未知联系方式到补充发布再提问，并验证AI发送表单和选项、提交后不重复发送。
func TestAIRealEmployeeFormsAndKnowledgeGap(t *testing.T) {
	if os.Getenv("GOODCHAT_REAL_AI_CONFIG") == "" {
		t.Skip("未提供真实模型私有配置")
	}
	f := newFixture(t)
	ctx := context.Background()
	configureRealEmployee(t, f)
	// 仅使用随机隔离业务代号，避免真实联系方式进入测试数据。
	question := "星桥验收产品的公开服务微信号是多少？"
	for i := 0; i < 2; i++ {
		token, _, conversation := f.visitor("")
		status, data := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": question})
		mustData(t, status, data)
		runAITestJob(t, f)
	}
	if f.countRows("SELECT count(*) FROM ai_pending_items WHERE status='pending' AND occurrences=2") != 1 {
		var details []byte
		_ = f.admin.QueryRow(ctx, "SELECT coalesce(jsonb_agg(jsonb_build_object('status',status,'error',error_code,'result',result)),'[]'::jsonb) FROM ai_jobs").Scan(&details)
		var events []byte
		_ = f.admin.QueryRow(ctx, "SELECT coalesce(jsonb_agg(jsonb_build_object('name',name,'kind',kind,'payload',payload)),'[]'::jsonb) FROM ai_job_events WHERE kind='tool_start'").Scan(&events)
		t.Fatalf("真实AI未记录或合并未知问题：任务%s 工具%s", string(details), string(events))
	}
	var pending string
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM ai_pending_items LIMIT 1").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	status, data := f.request("POST", "/api/v1/tenant/ai/inbox/"+pending, f.owner, "", map[string]any{"action": "supplement", "title": "星桥验收产品公开服务微信", "answer": "星桥验收产品的公开服务微信号是 StarBridge_QA_2026，仅用于本次隔离验收。"})
	mustData(t, status, data)
	runAITestJob(t, f)
	token, _, conversation := f.visitor("")
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": question})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai' AND content LIKE '%StarBridge_QA_2026%'", conversation) != 1 {
		t.Fatal("真实AI补充后仍未根据新知识回答")
	}
	fields := []formField{{ID: newID(), Label: "姓名", Required: true, Type: "text"}, {ID: newID(), Label: "需求", Type: "textarea"}}
	createQuickReply(t, f, f.owner, map[string]any{"title": "产品演示预约表单", "scope": "public", "message_type": "form", "form_fields": fields})
	createQuickReply(t, f, f.owner, map[string]any{"title": "选择咨询类型", "scope": "public", "message_type": "text", "content": "请选择需要的服务", "options": []any{map[string]any{"id": "product", "label": "产品咨询", "value": "我想了解产品"}, map[string]any{"id": "demo", "label": "预约演示", "value": "我要预约演示"}}})
	token, _, conversation = f.visitor("")
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "请发产品演示预约表单给我填写。"})
	mustData(t, status, data)
	runAITestJob(t, f)
	var form string
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM messages WHERE conversation_id=$1 AND source='ai' AND message_type='form'", conversation).Scan(&form); err != nil {
		t.Fatal("真实AI没有调用表单快捷回复")
	}
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "message_type": "form_result", "form_request_id": form, "form_values": map[string]string{fields[0].ID: "隔离验收", fields[1].ID: "仅登记，无需再次发送表单"}})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND message_type='form'", conversation) != 1 {
		t.Fatal("表单结果触发AI重复发送表单")
	}
	token, _, conversation = f.visitor("")
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "请发送选择咨询类型的选项让我点击。"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai' AND jsonb_array_length(options)>0", conversation) != 1 {
		t.Fatal("真实AI未发送可点击选项")
	}
	for _, example := range []struct{ locale, question, word string }{{"en", "What is the public WeChat ID for 星桥验收产品? Reply in English.", "wechat"}, {"fr", "Quel est l’identifiant WeChat public du produit 星桥验收产品 ? Réponds en français.", "est"}} {
		token, _, conversation = f.visitor("")
		req, err := http.NewRequest("POST", f.server.URL+"/api/v1/conversations/"+conversation+"/messages", bytes.NewReader(jsonValue(map[string]any{"client_message_id": newID(), "content": example.question})))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GoodChat-Locale", example.locale)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal("多语言真实请求失败")
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("多语言真实发送失败", response.StatusCode)
		}
		runAITestJob(t, f)
		var answer, locale string
		if err = f.admin.QueryRow(ctx, "SELECT coalesce((SELECT content FROM messages WHERE conversation_id=$1 AND source='ai' ORDER BY created_at DESC LIMIT 1),''),coalesce((SELECT snapshot->>'locale' FROM ai_jobs WHERE conversation_id=$1 ORDER BY created_at DESC LIMIT 1),'')", conversation).Scan(&answer, &locale); err != nil {
			t.Fatal(err)
		}
		if locale != example.locale || !strings.Contains(answer, "StarBridge_QA_2026") || !strings.Contains(strings.ToLower(answer), example.word) {
			t.Fatalf("多语言回答未使用发布知识或对应语言：%s %s", locale, answer)
		}
		t.Logf("真实%s回答：%s", example.locale, answer)
	}
	t.Log("真实供应商：未知问题记录和去重、补充发布后正确回答、AI发送表单、填写后不重发、AI发送选项均通过")
}
