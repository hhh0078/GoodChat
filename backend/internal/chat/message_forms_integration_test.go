// 文件作用说明：使用真实会话、访客令牌和PostgreSQL验证表单消息、必填、幂等、越权及关键词和AI快捷回复。
package chat

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"supportdesk/backend/internal/platform"
	"sync"
	"testing"
)

// TestQuickReplyFormSubmission 验证历史字段冻结，提交作为双方可读消息，必填和并发重试由服务端保证。
func TestQuickReplyFormSubmission(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	first, second := newID(), newID()
	fields := []formField{{ID: first, Label: "姓名", Required: true, Type: "text"}, {ID: second, Label: "需求说明", Required: false, Type: "textarea"}}
	quick := createQuickReply(t, f, f.owner, map[string]any{"title": "预约需求", "scope": "public", "message_type": "form", "form_fields": fields})
	send := map[string]any{"client_message_id": newID(), "quick_reply_id": quick["id"], "quick_reply_version": quick["version"]}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", send)
	request := mustData(t, status, reply)
	if request["message_type"] != "form" || len(request["form_fields"].([]any)) != 2 {
		t.Fatal(request)
	}
	id := request["id"].(string)
	// 修改配置不改变已发送表单。
	fields[0].Label = "新的标题"
	status, reply = f.request("PATCH", "/api/v1/tenant/quick-replies/"+quick["id"].(string), f.owner, "", map[string]any{"version": quick["version"], "form_fields": fields})
	mustData(t, status, reply)
	body := map[string]any{"client_message_id": newID(), "content": "伪造显示内容", "message_type": "form_result", "form_request_id": id, "form_values": map[string]string{second: "说明"}}
	if c, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, body); c != 400 {
		t.Fatal("漏填", c)
	}
	body["form_values"] = map[string]string{first: "张三", "unknown": "注入"}
	if c, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, body); c != 400 {
		t.Fatal("未知字段", c)
	}
	body["form_values"] = map[string]string{first: "张三", second: "需要了解报价"}
	if c, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", body); c != 403 {
		t.Fatal("客服代填", c)
	}
	other, _, _ := f.visitor("")
	if c, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", other, body); c == 200 {
		t.Fatal("其他访客提交")
	}
	var wg sync.WaitGroup
	codes := make(chan int, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, body)
			codes <- c
		}()
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 200 {
			t.Fatal("并发重试", c)
		}
	}
	if f.countRows("SELECT count(*) FROM messages WHERE form_request_id=$1", id) != 1 {
		t.Fatal("重复提交")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, body)
	result := mustData(t, status, reply)
	if !strings.Contains(result["content"].(string), "姓名：张三") || strings.Contains(result["content"].(string), "伪造显示") {
		t.Fatal(result)
	}
	body["client_message_id"] = newID()
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, body)
	if mustData(t, status, reply)["id"] != result["id"] {
		t.Fatal("换编号重试重复")
	}
	body["form_values"] = map[string]string{first: "李四"}
	if c, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, body); c != 409 {
		t.Fatal("不可改写", c)
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	items := mustData(t, status, reply)["items"].([]any)
	found := false
	for _, v := range items {
		m := v.(map[string]any)
		if m["id"] == result["id"] {
			found = true
			if m["form_values"].(map[string]any)[first] != "张三" {
				t.Fatal(m)
			}
		}
	}
	if !found {
		t.Fatal("客服历史缺失")
	}
	_ = ctx
}

// TestKeywordAndAIQuickForms 验证公共关键词可发送表单，AI目录含可见各类型快捷回复且最终重验版本。
func TestKeywordAndAIQuickForms(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	fields := []formField{{ID: newID(), Label: "联系方式", Required: true, Type: "text"}}
	q := createQuickReply(t, f, f.owner, map[string]any{"title": "预约联系表单", "scope": "public", "message_type": "form", "form_fields": fields})
	status, reply := f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"keywords_enabled": true})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/tenant/keyword-rules", f.owner, "", map[string]any{"name": "预约表单", "pattern": "预约", "reply": "", "quick_reply_id": q["id"], "enabled": true})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "我要预约"})
	mustData(t, status, reply)
	if e := f.s.runAutomatic(ctx, f.tenant); e != nil {
		t.Fatal(e)
	}
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND message_type='form' AND source='keyword'", conversation) != 1 {
		t.Fatal("关键词未发送表单")
	}
	var formID string
	if e := f.admin.QueryRow(ctx, "SELECT id::text FROM messages WHERE conversation_id=$1 AND message_type='form'", conversation).Scan(&formID); e != nil {
		t.Fatal(e)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "message_type": "form_result", "form_request_id": formID, "form_values": map[string]string{fields[0].ID: "张三"}})
	mustData(t, status, reply)
	if e := f.s.runAutomatic(ctx, f.tenant); e != nil {
		t.Fatal(e)
	}
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND message_type='form'", conversation) != 1 {
		t.Fatal("表单结果不能再次触发关键词循环")
	}
	private := createQuickReply(t, f, f.owner, map[string]any{"title": "私有表单", "message_type": "form", "form_fields": fields})
	if c, _ := f.request("POST", "/api/v1/tenant/keyword-rules", f.owner, "", map[string]any{"name": "无效公共引用", "pattern": "x", "quick_reply_id": private["id"], "enabled": true}); c != 403 {
		t.Fatal(c)
	}
	version, e := strconv.ParseInt(q["version"].(string), 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	j := &aiJobData{Tenant: f.tenant, Owner: f.agent}
	e = platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		if e := f.s.aiCatalog(ctx, tx, j); e != nil {
			return e
		}
		found := false
		for _, ref := range j.Snapshot.Scripts {
			if ref.ID == private["id"] {
				t.Fatal("AI读取其他成员个人话术")
			}
			if ref.ID == q["id"] && ref.QuickReply {
				found = true
			}
		}
		if !found {
			t.Fatal("AI目录无公共快捷回复")
		}
		blocks, e := f.s.expandAIPlan(ctx, tx, j, aiPlan{Items: []aiReplyItem{{Type: "script", ScriptID: q["id"].(string), Version: version}}})
		if e != nil {
			return e
		}
		if len(blocks) != 1 || blocks[0].Type != "form" || len(blocks[0].FormFields) != 1 {
			t.Fatal(blocks)
		}
		_, e = tx.Exec(ctx, "UPDATE quick_replies SET version=version+1 WHERE id=$1", q["id"])
		if e != nil {
			return e
		}
		if _, e = f.s.expandAIPlan(ctx, tx, j, aiPlan{Items: []aiReplyItem{{Type: "script", ScriptID: q["id"].(string), Version: version}}}); e == nil {
			t.Fatal("AI绕过旧版本")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
