// 文件作用说明：使用真实PostgreSQL、HTTP消息和模拟供应商验证Eino工具、权限、自动发送及计费竞态；模拟结果不作为真实供应商验收。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// setupAITest 在隔离schema创建平台模型及余额，供应商使用显式本机测试开关。
func setupAITest(t *testing.T, f *fixture, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	f.s.cfg.AuthSecret = strings.Repeat("a", 40)
	f.s.cfg.CallbackAllowLocal = true
	id := newID()
	m := aiModel{ID: id, Name: "测试模型", BaseURL: server.URL + "/v1", Model: "fixture", Enabled: true, Version: 1, CostInput: 1000000, CostOutput: 4000000, SaleInput: 2000000, SaleOutput: 8000000, MaxInput: 64000, MaxOutput: 1024, SupportsImages: true}
	secret, e := f.s.aiSecret("fixture-key", id, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(context.Background(), "INSERT INTO ai_chat_models(id,name,enabled) VALUES($1,'fixture',true)", id); e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(context.Background(), "INSERT INTO ai_chat_model_versions(model_id,version,config,secret_ciphertext) VALUES($1,1,$2,$3)", id, jsonValue(m), secret); e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(context.Background(), "INSERT INTO ai_wallets(tenant_id,balance_micro) VALUES($1,100000000)", f.tenant); e != nil {
		t.Fatal(e)
	}
	return id
}

// aiFixtureResponse 返回明确标识为模拟的ChatCompletions结构，经过真实Eino适配器解析。
func aiFixtureResponse(w http.ResponseWriter, tool string, args any, content string, usage bool) {
	message := map[string]any{"role": "assistant", "content": content}
	if tool != "" {
		message["tool_calls"] = []any{map[string]any{"id": "call_fixture", "type": "function", "function": map[string]any{"name": tool, "arguments": string(jsonValue(args))}}}
	}
	response := map[string]any{"id": "fixture", "object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": "stop"}}}
	if usage {
		response["usage"] = map[string]any{"prompt_tokens": 200, "completion_tokens": 50, "total_tokens": 250}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// runAITestJob 驱动真实持久任务，不绕过预占、Eino调用和最终结算。
func runAITestJob(t *testing.T, f *fixture) {
	t.Helper()
	if _, e := f.admin.Exec(context.Background(), "UPDATE ai_jobs SET run_after=now() WHERE status='queued'"); e != nil {
		t.Fatal(e)
	}
	if e := f.s.aiTick(context.Background()); e != nil {
		t.Fatal(e)
	}
}

// TestAIKnowledgePublishAndPermissions 验证描述原子发布、成员边界和真实用量账本。
func TestAIKnowledgePublishAndPermissions(t *testing.T) {
	f := newFixture(t)
	setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		aiFixtureResponse(w, "", nil, "适用于产品购买及套餐咨询。", true)
	})
	status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.owner, "", map[string]any{"title": "售前", "body": "售价为100元。", "enabled": true})
	id := mustData(t, status, data)["id"].(string)
	if f.countRows("SELECT count(*) FROM ai_knowledge_bases WHERE id=$1 AND published_version=0 AND status='pending'", id) != 1 {
		t.Fatal("首次保存不应提前发布")
	}
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_knowledge_bases WHERE id=$1 AND published_version=1 AND published_body='售价为100元。' AND description<>'' AND status='ready'", id) != 1 {
		t.Fatal("描述及正文未原子发布")
	}
	if f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE tenant_id=$1 AND delta_micro=-800 AND kind='charge'", f.tenant) != 1 {
		t.Fatal("真实Token售价结算错误")
	}
	status, _ = f.request("PATCH", "/api/v1/tenant/ai/knowledge/"+id, f.agent, "", map[string]any{"title": "篡改", "body": "fake", "version": 1, "enabled": true})
	if status != 403 {
		t.Fatalf("他人内容写入边界错误:%d", status)
	}
	status, _ = f.request("PATCH", "/api/v1/tenant/ai/knowledge/"+id, f.otherOwner, "", map[string]any{"title": "篡改", "body": "fake", "version": 1, "enabled": true})
	if status != 404 {
		t.Fatalf("跨租户边界错误:%d", status)
	}
	status, data = f.request("GET", "/api/v1/tenant/ai/usage", f.owner, "", nil)
	mustData(t, status, data)
	encoded := string(jsonValue(data))
	if strings.Contains(encoded, "cost_micro") || strings.Contains(encoded, "fixture-key") {
		t.Fatal("租户看到了平台成本或密钥")
	}
}

// TestAIAutoScriptAndIdempotency 验证机器人话术工具经过白名单后发送，重复调度不重复消息或扣款。
func TestAIAutoScriptAndIdempotency(t *testing.T) {
	f := newFixture(t)
	var scriptID string
	var calls atomic.Int32
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		aiFixtureResponse(w, "submit_reply", map[string]any{"items": []any{map[string]any{"type": "script", "script_id": scriptID, "version": 1}}}, "", true)
	})
	status, data := f.request("POST", "/api/v1/tenant/ai/scripts", f.agent, "", map[string]any{"title": "售后", "description": "询问售后时使用", "language": "zh-CN", "enabled": true, "blocks": []aiBlock{{Type: "text", Text: "这是固定售后话术。"}}})
	scriptID = mustData(t, status, data)["id"].(string)
	b := defaultAIBot()
	b.Mode = "auto"
	b.ModelID = model
	b.ScriptIDs = []string{scriptID}
	status, data = f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", b)
	mustData(t, status, data)
	token, _, conversation := f.visitor("")
	f.online(f.agent)
	personalClaim(t, f, conversation, f.agent)
	// 正式接待后明确恢复机器人，人工领取本身保持人工控制。
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/ai", f.agent, "", map[string]any{"action": "resume"})
	mustData(t, status, data)
	client := newID()
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": client, "content": "请问售后？"})
	mustData(t, status, data)
	runAITestJob(t, f)
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": client, "content": "请问售后？"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if calls.Load() != 1 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai' AND content='这是固定售后话术。'", conversation) != 1 {
		t.Fatal("工具未发送或重复发送")
	}
	if f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE tenant_id=$1 AND kind='charge'", f.tenant) != 1 {
		t.Fatal("重复扣款")
	}
	status, data = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	result := mustData(t, status, data)
	if !strings.Contains(string(jsonValue(result)), "AI") {
		t.Fatal("访客消息未标记AI身份")
	}
}

// TestAIUnknownUsageKeepsReservation 验证供应商缺失用量时不猜测扣款，并暂停自动接待。
func TestAIUnknownUsageKeepsReservation(t *testing.T) {
	f := newFixture(t)
	setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) { aiFixtureResponse(w, "", nil, "描述", false) })
	status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.owner, "", map[string]any{"title": "售后", "body": "退款说明", "enabled": true})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE status='uncertain' AND reserved_micro>0") != 1 || f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE kind='charge'") != 0 {
		t.Fatal("缺少用量时应保留预占等待核对")
	}
}

// TestAIPreemptionAndBudget 验证人工抢先回复使生成结果失效，平台保留成本但不向租户扣费。
func TestAIPreemptionAndBudget(t *testing.T) {
	f := newFixture(t)
	var conversation string
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		status, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "人工抢先回复"})
		if status != 200 {
			t.Errorf("人工回复失败:%d", status)
		}
		aiFixtureResponse(w, "submit_reply", map[string]any{"items": []aiReplyItem{{Type: "text", Text: "迟到的AI回复"}}}, "", true)
	})
	b := defaultAIBot()
	b.Mode = "auto"
	b.ModelID = model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", b)
	mustData(t, status, data)
	token, _, id := f.visitor("")
	conversation = id
	f.online(f.agent)
	personalClaim(t, f, id, f.agent)
	status, data = f.request("POST", "/api/v1/conversations/"+id+"/ai", f.agent, "", map[string]any{"action": "resume"})
	mustData(t, status, data)
	status, data = f.request("POST", "/api/v1/conversations/"+id+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "问题"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM messages WHERE content='迟到的AI回复'") != 0 || f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE kind='charge'") != 0 {
		t.Fatal("人工抢先后不得发送或扣款")
	}
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE status='cancelled' AND cost_micro=400 AND reserved_micro=0") != 1 {
		t.Fatal("取消未释放预占或丢失成本")
	}
	if e := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "UPDATE ai_wallets SET balance_micro=0")
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
