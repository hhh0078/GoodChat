// 文件作用说明：回归知识缺口工具的租户隔离、重复合并、发布绑定和可审计链路，不作为真实供应商验收。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestAIGapSimilarityAndRedaction 覆盖格式相近问题、不同订单与图片密钥过滤边界。
func TestAIGapSimilarityAndRedaction(t *testing.T) {
	if !aiGapSimilar("请问，支持发票申请吗？", "请问支持发票申请吗") || aiGapSimilar("查询订单123456状态", "查询订单123457状态") || aiGapSimilar("发票", "退款") {
		t.Fatal("相似度边界错误")
	}
	raw := string(jsonValue(aiTraceSafe(map[string]any{"api_key": "secret-key", "parts": []any{map[string]any{"Base64Data": "secret-image", "content": "正常消息"}}, "reasoning_content": "private-thought"})))
	if strings.Contains(raw, "secret-key") || strings.Contains(raw, "secret-image") || strings.Contains(raw, "private-thought") || !strings.Contains(raw, "正常消息") {
		t.Fatal("链路脱敏未生效")
	}
}

// TestAIInboxAgentLifecycle 驱动完整模型工具、真实消息、补充发布和机器人授权流程，验证去重与跨租户权限。
func TestAIInboxAgentLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var summaryBroken atomic.Bool
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if strings.Contains(string(jsonValue(in.Messages[0].Content)), "知识库生成使用场景描述") {
			if summaryBroken.Load() {
				aiFixtureResponse(w, "", nil, "", true)
			} else {
				aiFixtureResponse(w, "", nil, "用于验收代号公开说明。", true)
			}
			return
		}
		last := in.Messages[len(in.Messages)-1]
		if last.Role == "tool" {
			aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "现有资料未说明，我已记录待补充。"}}}, "", true)
		} else {
			aiFixtureResponse(w, "report_knowledge_gap", aiGapInput{Reason: "缺少产品的公开验收代号"}, "", true)
		}
	})
	b := defaultAIBot()
	b.Mode = "auto"
	b.ModelID = model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	bot := mustData(t, status, data)["id"].(string)
	status, data = f.request("PUT", "/api/v1/tenant/ai/settings", f.owner, "", map[string]any{"default_bot_id": bot, "reception_mode": "ai_first"})
	mustData(t, status, data)
	question := "产品的公开验收代号是什么？"
	// ask 通过访客 HTTP 消息创建真实回复任务，不直接构造待办。
	ask := func() {
		token, _, conversation := f.visitor("")
		status, data := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": question})
		mustData(t, status, data)
	}
	ask()
	ask()
	if _, e := f.admin.Exec(ctx, "UPDATE ai_jobs SET run_after=now() WHERE status='queued'"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := f.s.aiTick(ctx); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if f.countRows("SELECT count(*) FROM ai_pending_items WHERE occurrences=2 AND status='pending'") != 1 {
		t.Fatal("工具未合并真实访客重复问题")
	}
	status, data = f.request("GET", "/api/v1/tenant/ai/inbox", f.owner, "", nil)
	items := mustData(t, status, data)["items"].([]any)
	item := items[0].(map[string]any)
	id := item["id"].(string)
	job := item["last_job_id"].(string)
	for _, token := range []string{f.agent, f.otherOwner} {
		status, _ = f.request("POST", "/api/v1/tenant/ai/inbox/"+id, token, "", map[string]any{"action": "ignore"})
		if status < 400 {
			t.Fatal("非管理员或其他租户修改待办")
		}
	}
	status, data = f.request("GET", "/api/v1/tenant/ai/traces/"+job, f.owner, "", nil)
	trace := mustData(t, status, data)
	if len(trace["events"].([]any)) < 8 || len(trace["sent_messages"].([]any)) != 1 {
		t.Fatal("模型、工具或真实发送链路未完整持久化")
	}
	status, _ = f.request("GET", "/api/v1/tenant/ai/traces/"+job, f.agent, "", nil)
	if status < 400 {
		t.Fatal("普通成员查看了其他成员链路")
	}
	status, _ = f.request("GET", "/api/v1/tenant/ai/traces/"+job, f.otherOwner, "", nil)
	if status < 400 {
		t.Fatal("链路跨租户泄露")
	}
	status, data = f.request("POST", "/api/v1/tenant/ai/inbox/"+id, f.owner, "", map[string]any{"action": "ignore"})
	mustData(t, status, data)
	ask()
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_pending_items WHERE occurrences=3 AND status='ignored'") != 1 {
		t.Fatal("忽略后重复新增了待办")
	}
	status, data = f.request("POST", "/api/v1/tenant/ai/inbox/"+id, f.owner, "", map[string]any{"action": "reopen"})
	mustData(t, status, data)
	status, data = f.request("POST", "/api/v1/tenant/ai/inbox/"+id, f.owner, "", map[string]any{"action": "supplement", "title": "公开验收代号", "answer": "公开验收代号为星桥。"})
	knowledge := mustData(t, status, data)["knowledge_id"].(string)
	if f.countRows("SELECT count(*) FROM ai_pending_items WHERE id=$1 AND status='supplementing'", id) != 1 {
		t.Fatal("描述生成前提前完成了待办")
	}
	summaryBroken.Store(true)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_pending_items WHERE id=$1 AND status='pending'", id) != 1 {
		t.Fatal("发布失败后未恢复待处理")
	}
	if f.countRows("SELECT count(*) FROM ai_bots WHERE id=$1 AND $2::uuid=ANY(knowledge_ids)", bot, knowledge) != 0 {
		t.Fatal("发布失败时提前授权了知识")
	}
	summaryBroken.Store(false)
	status, data = f.request("POST", "/api/v1/tenant/ai/inbox/"+id, f.owner, "", map[string]any{"action": "retry"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_pending_items WHERE id=$1 AND status='resolved'", id) != 1 || f.countRows("SELECT count(*) FROM ai_bots WHERE id=$1 AND $2::uuid=ANY(knowledge_ids)", bot, knowledge) != 1 {
		t.Fatal("发布后未完成待办并绑定机器人")
	}
	if f.countRows("SELECT count(*) FROM ai_knowledge_bases WHERE id=$1 AND status='ready' AND published_body LIKE '%星桥%'", knowledge) != 1 {
		t.Fatal("补充事实未发布")
	}
	if _, e := f.admin.Exec(ctx, "UPDATE ai_pending_items SET status='pending' WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
	status, data = f.request("POST", "/api/v1/tenant/ai/inbox/"+id, f.owner, "", map[string]any{"action": "delete"})
	mustData(t, status, data)
	ask()
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_pending_items WHERE status='deleted' AND occurrences=4") != 1 {
		t.Fatal("删除后没有保留去重标记")
	}
}
