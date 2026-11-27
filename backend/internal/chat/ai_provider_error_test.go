// 文件作用说明：回归供应商权益错误的严格封套识别、单次调用、原始用量审计及回复和摘要的费用保护。
package chat

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// TestAIProviderUnavailableReply 只识别已复现的短纯文本错误，不扩大普通回复或工具消息的处理范围。
func TestAIProviderUnavailableReply(t *testing.T) {
	for _, content := range []string{"[上游错误] 权益不足", "\n[上游错误] 权益不足 \n"} {
		if !aiProviderUnavailableReply(schema.AssistantMessage(content, nil)) {
			t.Fatal("未识别已知完整供应商错误")
		}
	}
	for _, content := range []string{"", "权益不足", "您遇到权益不足时请联系管理员。", "这是上游返回：[上游错误] 权益不足", "[上游错误] 权益不足，请联系管理员", "[上游错误] 未知故障", "```\n[上游错误] 权益不足\n```", strings.Repeat(" ", 256) + "[上游错误] 权益不足"} {
		if aiProviderUnavailableReply(schema.AssistantMessage(content, nil)) {
			t.Fatal("普通正文、未知封套或超长响应被误识别")
		}
	}
	for _, out := range []*schema.Message{
		nil,
		schema.UserMessage("[上游错误] 权益不足"),
		schema.AssistantMessage("[上游错误] 权益不足", []schema.ToolCall{{ID: "allowed", Function: schema.FunctionCall{Name: "submit_reply"}}}),
		{Role: schema.Assistant, Content: "[上游错误] 权益不足", MultiContent: []schema.ChatMessagePart{{}}},
		{Role: schema.Assistant, Content: "[上游错误] 权益不足", UserInputMultiContent: []schema.MessageInputPart{{}}},
		{Role: schema.Assistant, Content: "[上游错误] 权益不足", AssistantGenMultiContent: []schema.MessageOutputPart{{}}},
	} {
		if aiProviderUnavailableReply(out) {
			t.Fatal("非纯文本助手错误被误识别")
		}
	}
}

// TestAIProviderUnavailableJob 使用真实PostgreSQL和HTTP模拟供应商错误，验证回复与摘要仅调用一次、保留成本且不向租户收费。
func TestAIProviderUnavailableJob(t *testing.T) {
	for _, kind := range []string{"reply", "summary"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			var calls atomic.Int32
			raw := "\n[上游错误] 权益不足"
			model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				aiFixtureResponse(w, "", nil, raw, true)
			})
			var conversation, knowledge, assignment string
			if kind == "reply" {
				bot := defaultAIBot()
				bot.Mode, bot.ModelID = "auto", model
				status, data := f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", bot)
				mustData(t, status, data)
				f.online(f.agent)
				token, _, id := f.visitor("")
				conversation = id
				personalClaim(t, f, conversation, f.agent)
				status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/ai", f.agent, "", map[string]any{"action": "resume"})
				mustData(t, status, data)
				status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "会封号吗"})
				mustData(t, status, data)
				if err := f.admin.QueryRow(t.Context(), "SELECT current_assignment_id::text FROM conversations WHERE id=$1", conversation).Scan(&assignment); err != nil {
					t.Fatal(err)
				}
			} else {
				status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.owner, "", map[string]any{"title": "供应商错误摘要回归", "body": "产品使用既有接入链接提供客服服务。", "enabled": true})
				knowledge = mustData(t, status, data)["id"].(string)
			}
			runAITestJob(t, f)
			var job string
			if err := f.admin.QueryRow(t.Context(), "SELECT id::text FROM ai_jobs WHERE kind=$1", kind).Scan(&job); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || f.countRows("SELECT count(*) FROM ai_jobs WHERE id=$1 AND status='failed' AND error_code='ai_provider_unavailable' AND billing_status='reported' AND input_tokens=200 AND output_tokens=50 AND cost_micro=400 AND sale_micro=0 AND reserved_micro=0", job) != 1 {
				t.Fatal("供应商错误应只调用一次、明确失败并保留真实用量成本，释放预占且不向租户收费")
			}
			if f.countRows("SELECT count(*) FROM ai_call_attempts WHERE job_id=$1 AND call_index=1 AND status='reported' AND input_tokens=200 AND output_tokens=50 AND cost_micro=400 AND sale_micro=0 AND response_meta->>'platform_covered'='true'", job) != 1 || f.countRows("SELECT count(*) FROM ai_call_attempts WHERE job_id=$1", job) != 1 {
				t.Fatal("费用豁免不能丢失已报告用量、成本或保留未知调用状态")
			}
			if f.countRows("SELECT count(*) FROM ai_job_events WHERE job_id=$1 AND kind='model_response' AND payload#>>'{message,content}'=$2 AND (payload#>>'{usage,prompt_tokens}')::int=200 AND (payload#>>'{usage,completion_tokens}')::int=50", job, raw) != 1 {
				t.Fatal("原始供应商响应和用量没有完整保留")
			}
			if f.countRows("SELECT count(*) FROM ai_job_events WHERE job_id=$1 AND kind='tool_start'", job) != 0 || f.countRows("SELECT count(*) FROM ai_pending_items") != 0 || f.countRows("SELECT count(*) FROM ai_wallets WHERE tenant_id=$1 AND balance_micro=100000000 AND reserved_micro=0", f.tenant) != 1 || f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE job_id=$1", job) != 0 {
				t.Fatal("供应商故障不能调用业务工具、生成知识缺口或扣减租户余额")
			}
			if kind == "reply" {
				if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND content LIKE '%权益不足%'", conversation) != 0 || f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='active' AND current_agent_id=$2 AND current_assignment_id=$3 AND ai_paused", conversation, f.agent, assignment) != 1 {
					t.Fatal("不能把供应商错误正文发给访客，或改变原人工归属")
				}
				if f.countRows("SELECT count(*) FROM messages WHERE ai_job_id=$1 AND content='AI自动接待已暂停，请稍候或继续留言，等待人工客服处理。'", job) != 1 {
					t.Fatal("已有人工接待时不应宣称重新转接人工")
				}
				status, data := f.request("GET", "/api/v1/conversations/"+conversation+"/ai", f.agent, "", nil)
				if mustData(t, status, data)["control"].(map[string]any)["error_code"] != "ai_provider_unavailable" {
					t.Fatal("当前失败暂停应向有权限客服展示安全供应商错误分类")
				}
				for _, action := range []string{"resume", "pause"} {
					status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/ai", f.agent, "", map[string]any{"action": action})
					if mustData(t, status, data)["control"].(map[string]any)["error_code"] != "" {
						t.Fatal("恢复或手动暂停后不能继续显示旧任务的故障原因")
					}
				}
			} else if f.countRows("SELECT count(*) FROM ai_knowledge_bases WHERE id=$1 AND status='failed' AND error_code='ai_provider_unavailable' AND published_version=0 AND description=''", knowledge) != 1 {
				t.Fatal("供应商错误不能发布为知识描述")
			}
		})
	}
}
