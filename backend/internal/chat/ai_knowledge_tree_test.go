// 文件作用说明：验证树形目录的租户权限、循环防护、旧知识兼容，以及每轮模型请求携带完整授权目录。
package chat

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestAIKnowledgeDirectoryAndEveryCall 验证目录组织不扩大正文权限，移动未改正文时不重复触发计费摘要。
func TestAIKnowledgeDirectoryAndEveryCall(t *testing.T) {
	f := newFixture(t)
	var first, second, hidden, root, child string
	calls := 0
	catalog := ""
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
			t.Error(e)
		}
		if strings.Contains(in.Messages[0].Content, "知识库生成使用场景描述") {
			aiFixtureResponse(w, "", nil, "用于产品安装及更新咨询。", true)
			return
		}
		prompt := in.Messages[0].Content
		marker := "Complete authorized knowledge directory tree (folders organize documents; only document IDs may be read): "
		parts := strings.SplitN(prompt, marker, 2)
		if len(parts) != 2 {
			t.Error("模型请求缺少完整目录")
			aiFixtureResponse(w, "request_handoff", map[string]any{"reason": "missing catalog"}, "", true)
			return
		}
		current := strings.SplitN(parts[1], "\nAllowed script catalog:", 2)[0]
		if catalog != "" && catalog != current {
			t.Error("后续模型调用没有携带同一份完整授权目录")
		}
		catalog = current
		for _, id := range []string{first, second, root, child} {
			if !strings.Contains(current, id) {
				t.Errorf("授权目录漏项 %s", id)
			}
		}
		if strings.Contains(current, hidden) || strings.Contains(current, "安装验收正文") {
			t.Error("未授权文档或正文提前泄露进目录")
		}
		calls++
		if calls == 1 {
			aiFixtureResponse(w, "read_knowledge_bases", map[string]any{"ids": []string{first}}, "", true)
		} else {
			aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "安装验收正文已读取。"}}}, "", true)
		}
	})
	status, data := f.request("POST", "/api/v1/tenant/ai/knowledge-folders", f.owner, "", map[string]any{"name": "产品帮助", "parent_id": ""})
	root = mustData(t, status, data)["id"].(string)
	status, data = f.request("POST", "/api/v1/tenant/ai/knowledge-folders", f.owner, "", map[string]any{"name": "安装与更新", "parent_id": root})
	child = mustData(t, status, data)["id"].(string)
	status, _ = f.request("PATCH", "/api/v1/tenant/ai/knowledge-folders/"+root, f.owner, "", map[string]any{"name": "循环", "parent_id": child, "version": 1})
	if status != 400 {
		t.Fatal("未拒绝父目录循环")
	}
	status, _ = f.request("POST", "/api/v1/tenant/ai/knowledge-folders", f.agent, "", map[string]any{"name": "越权目录"})
	if status != 403 {
		t.Fatal("非管理员维护共享目录")
	}
	status, _ = f.request("POST", "/api/v1/tenant/ai/knowledge", f.otherOwner, "", map[string]any{"title": "跨租户", "body": "不得进入其他租户目录", "enabled": true, "folder_id": child})
	if status < 400 {
		t.Fatal("跨租户目录绑定成功")
	}
	// create 通过真实内容发布接口创建文档，同时覆盖旧客户端不携带目录字段的路径。
	create := func(title string, folder *string) string {
		body := map[string]any{"title": title, "body": "安装验收正文", "enabled": true}
		if folder != nil {
			body["folder_id"] = *folder
		}
		status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.owner, "", body)
		id := mustData(t, status, data)["id"].(string)
		runAITestJob(t, f)
		return id
	}
	first = create("安装指南", &child)
	second = create("旧客户端根知识", nil)
	hidden = create("未授权文档", &child)
	before := f.countRows("SELECT count(*) FROM ai_jobs WHERE kind='summary'")
	for _, folder := range []string{"", child} {
		status, data = f.request("PATCH", "/api/v1/tenant/ai/knowledge/"+first, f.owner, "", map[string]any{"title": "安装指南", "body": "安装验收正文", "enabled": true, "folder_id": folder, "version": 1})
		mustData(t, status, data)
	}
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE kind='summary'") != before {
		t.Fatal("只移动目录重复调用了描述模型")
	}
	status, _ = f.request("DELETE", "/api/v1/tenant/ai/knowledge-folders/"+child, f.owner, "", nil)
	if status != 409 {
		t.Fatal("非空目录被删除")
	}
	bot := defaultAIBot()
	bot.Mode = "auto"
	bot.ModelID = model
	bot.KnowledgeIDs = []string{first, second}
	status, data = f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", bot)
	botID := mustData(t, status, data)["id"].(string)
	status, data = f.request("PUT", "/api/v1/tenant/ai/settings", f.owner, "", map[string]any{"default_bot_id": botID, "reception_mode": "ai_first"})
	mustData(t, status, data)
	token, _, conversation := f.visitor("")
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "怎么安装？"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if calls != 2 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai'", conversation) != 1 {
		t.Fatal("完整目录未在两次真实模型请求中验证")
	}
	if f.countRows(`SELECT count(*) FROM ai_job_events WHERE kind='model_request' AND payload::text LIKE '%安装与更新%' AND payload::text LIKE '%'||$1::text||'%'`, second) != 2 {
		t.Fatal("调用记录未保存每次完整目录快照")
	}
}
