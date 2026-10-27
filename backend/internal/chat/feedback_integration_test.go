// 文件作用说明：在真实PostgreSQL验证访客离线留言去重、管理员处理与内部备注隔离，以及关闭后单次评价和转接后的会话归属。
package chat

import (
	"context"
	"sync"
	"testing"
)

// TestOfflineLeadPersistenceAndPrivacy 验证留言只记录一次且不会虚构接待，管理员处理持久化，访客重试不能读取内部跟进备注或改写幂等请求。
func TestOfflineLeadPersistenceAndPrivacy(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token, _, conversation := f.visitor("13800000001")
	body := map[string]any{"entry_key": f.entry, "conversation_id": conversation, "client_request_id": newID(), "display_name": "离线留言访客", "phone": "13800000001", "content": "请联系我处理售后"}
	var group sync.WaitGroup
	codes := make(chan int, 8)
	ids := make(chan string, 8)
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			status, reply := f.request("POST", "/api/v1/visitor/leads", "", token, body)
			codes <- status
			if data, ok := reply["data"].(map[string]any); ok {
				ids <- data["id"].(string)
			}
		}()
	}
	group.Wait()
	close(codes)
	close(ids)
	for code := range codes {
		if code != 200 {
			t.Fatalf("同留言重试应幂等成功: %d", code)
		}
	}
	lead := ""
	for id := range ids {
		if lead == "" {
			lead = id
		} else if lead != id {
			t.Fatal("同一留言重复创建不同编号")
		}
	}
	if f.countRows("SELECT count(*) FROM offline_leads") != 1 || f.countRows("SELECT count(*) FROM conversations") != 1 || f.countRows("SELECT count(*) FROM conversations WHERE status='queued'") != 1 {
		t.Fatal("留言不能重复落库或伪造人工接待")
	}
	status, reply := f.request("GET", "/api/v1/tenant/leads", f.owner, "", nil)
	items := mustData(t, status, reply)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != lead {
		t.Fatal("管理员未读到真实留言")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/leads/"+lead, f.owner, "", map[string]any{"status": "following", "notes": "仅内部跟进凭据，不向访客展示"})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/leads", f.owner, "", nil)
	item := mustData(t, status, reply)["items"].([]any)[0].(map[string]any)
	if item["status"] != "following" || item["notes"] != "仅内部跟进凭据，不向访客展示" {
		t.Fatal("管理员处理结果未真实保存")
	}
	status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, body)
	visitor := mustData(t, status, reply)
	if _, exists := visitor["notes"]; exists {
		t.Fatal("访客重试泄漏管理员内部跟进备注")
	}
	copyBody := map[string]any{}
	for key, value := range body {
		copyBody[key] = value
	}
	copyBody["display_name"] = "篡改同请求的姓名"
	status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, copyBody)
	if status != 409 {
		t.Fatalf("同留言请求改变姓名应冲突: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", "/api/v1/tenant/access-links", f.owner, "", map[string]any{"name": "另一个留言来源"})
	if status != 201 {
		t.Fatalf("创建第二来源入口失败: %d %v", status, reply["code"])
	}
	secondEntry := reply["data"].(map[string]any)["entry_key"].(string)
	copyBody["display_name"] = body["display_name"]
	copyBody["entry_key"] = secondEntry
	status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, copyBody)
	if status != 409 {
		t.Fatalf("同一留言编号不能改成另一来源入口: %d %v", status, reply["code"])
	}
	copyBody["entry_key"] = body["entry_key"]
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	copyBody["conversation_id"] = mustData(t, status, reply)["conversation"].(map[string]any)["id"]
	status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, copyBody)
	if status != 409 {
		t.Fatalf("同一留言编号不能改绑新的咨询: %d %v", status, reply["code"])
	}
	copyBody["conversation_id"] = body["conversation_id"]
	status, reply = f.request("GET", "/api/v1/tenant/leads", f.otherOwner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("留言跨租户泄漏")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/leads/"+lead, f.otherOwner, "", map[string]any{"status": "resolved", "notes": "越权"})
	if status != 404 {
		t.Fatalf("跨租户处理留言应拒绝: %d %v", status, reply["code"])
	}
	status, reply = f.request("GET", "/api/v1/tenant/leads", f.agent, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 1 {
		t.Fatal("有效同组客服应能接收本组留言待办")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET can_serve=false WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	member := f.principals[f.agent]
	member.CanServe = false
	f.principals[f.agent] = member
	status, reply = f.request("GET", "/api/v1/tenant/leads", f.agent, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("无接待资格的普通成员不能平铺读取本组访客留言")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET can_serve=true WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	member.CanServe = true
	f.principals[f.agent] = member
	if _, err := f.admin.Exec(ctx, "DELETE FROM agent_group_members WHERE membership_id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("GET", "/api/v1/tenant/leads", f.agent, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("退出业务组后不能继续读取该组留言")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/leads/"+lead, f.agent, "", map[string]any{"status": "resolved", "notes": "越权处理"})
	if status != 404 {
		t.Fatalf("非授权组成员不能修改留言: %d %v", status, reply["code"])
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/leads/"+lead, f.owner, "", map[string]any{"status": "resolved", "notes": "已处理，尚无对外回执"})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM chat_audit_logs WHERE action='lead.updated' AND target_id=$1", lead) != 2 {
		t.Fatal("管理员处理动作应保留真实审计")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenants SET status='suspended' WHERE id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	copyBody["client_request_id"] = newID()
	status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, copyBody)
	if status != 403 {
		t.Fatalf("暂停租户不能新提交留言: %d %v", status, reply["code"])
	}
}

// TestEvaluationOnceAfterTransferClosure 验证仅本次咨询访客关闭后可以评分，重复提交只保留一条，转接后归属最终接待且不重复计入咨询。
func TestEvaluationOnceAfterTransferClosure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	f.online(f.owner)
	token, contact, conversation := f.visitor("")
	otherToken, _, otherConversation := f.visitor("")
	path := "/api/v1/conversations/" + conversation + "/evaluation"
	body := map[string]any{"score": 4, "comment": "售后已解决"}
	status, reply := f.request("POST", path, "", token, body)
	if status != 409 {
		t.Fatalf("未结束咨询不能评价: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", path, "", otherToken, body)
	if status != 404 {
		t.Fatalf("其他访客不能替本咨询评价: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", path, f.agent, "", body)
	if status != 403 {
		t.Fatalf("客服不能代评: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	var group sync.WaitGroup
	codes := make(chan int, 8)
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func() { defer group.Done(); status, _ := f.request("POST", path, "", token, body); codes <- status }()
	}
	group.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("评价同内容重试应幂等: %d", code)
		}
	}
	if f.countRows("SELECT count(*) FROM conversation_evaluations WHERE conversation_id=$1 AND contact_id=$2 AND score=4", conversation, contact) != 1 {
		t.Fatal("并发评分没有保持每咨询仅一条")
	}
	status, reply = f.request("POST", path, "", token, map[string]any{"score": 1, "comment": "重复修改"})
	if status != 409 {
		t.Fatalf("评价后不能覆盖原评分: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", path, f.owner, "", map[string]any{"score": 5, "comment": "管理员篡改"})
	if status != 403 {
		t.Fatalf("管理员也不能代评或修改评分: %d %v", status, reply["code"])
	}
	var finalAgent string
	var total int
	if err := f.admin.QueryRow(ctx, "SELECT c.current_agent_id::text FROM conversation_evaluations e JOIN conversations c ON c.id=e.conversation_id AND c.tenant_id=e.tenant_id WHERE e.conversation_id=$1", conversation).Scan(&finalAgent); err != nil || finalAgent != f.owner {
		t.Fatalf("转接后评价应关联最终关闭接待人: %s %v", finalAgent, err)
	}
	if err := f.admin.QueryRow(ctx, "SELECT count(*) FROM conversation_assignments WHERE conversation_id=$1", conversation).Scan(&total); err != nil || total != 2 {
		t.Fatalf("转接应保留两次接待事实且仍为同一咨询: %d %v", total, err)
	}
	status, reply = f.request("GET", "/api/v1/tenant/evaluations", f.owner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 1 {
		t.Fatal("评价列表不能按参与客服重复展开")
	}
	status, reply = f.request("GET", "/api/v1/tenant/evaluations", f.otherOwner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("其他租户不能读取评价")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+otherConversation+"/close", "", otherToken, map[string]any{})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE tenants SET status='suspended' WHERE id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+otherConversation+"/evaluation", "", otherToken, body)
	if status != 403 || f.countRows("SELECT count(*) FROM conversation_evaluations WHERE conversation_id=$1", otherConversation) != 0 {
		t.Fatalf("暂停租户不能新增评分: %d %v", status, reply["code"])
	}
	status, reply = f.request("GET", "/api/v1/tenant/evaluations", f.owner, "", nil)
	if status != 403 {
		t.Fatalf("租户暂停后旧管理员身份不能继续读取评价: %d %v", status, reply["code"])
	}
}
