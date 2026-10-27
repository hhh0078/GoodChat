// 文件作用说明：验证待确认转接摘要在工作台列表、详情和动作响应中的一致性、有效期与授权范围。
package chat

import (
	"context"
	"testing"
)

// TestPendingTransferProjectionScope 验证原客服和目标获得同一转接编号，访客与其他租户不获得摘要，过期请求不继续授权目标读取。
func TestPendingTransferProjectionScope(t *testing.T) {
	f := newFixture(t)
	f.online(f.owner)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, "UPDATE routing_pools SET assignment_mode='confirm' WHERE id=$1", f.poolID); err != nil {
		t.Fatal(err)
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/accept", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	source := mustData(t, status, reply)
	pending, ok := source["pending_transfer"].(map[string]any)
	if !ok || pending["target_agent_id"] != f.agent || pending["from_agent_id"] != f.owner {
		t.Fatal("原接待转接响应缺少可操作摘要")
	}
	request, ok := pending["id"].(string)
	if !ok || !uuidPattern.MatchString(request) {
		t.Fatal("待转接摘要缺少有效请求编号")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, f.agent, "", nil)
	target := mustData(t, status, reply)
	if target["pending_transfer"].(map[string]any)["id"] != request {
		t.Fatal("目标详情的待转接编号与原客服不一致")
	}
	status, reply = f.request("GET", "/api/v1/conversations", f.agent, "", nil)
	list := mustData(t, status, reply)["items"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["pending_transfer"].(map[string]any)["id"] != request {
		t.Fatal("目标工作台列表缺少待确认摘要")
	}
	_, _, unrelated := f.visitor("")
	status, reply = f.request("GET", "/api/v1/conversations/"+unrelated, f.agent, "", nil)
	other := mustData(t, status, reply)
	if other["pending_transfer"] != nil {
		t.Fatal("其他会话不能混入待转接摘要")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, "", token, nil)
	visitor := mustData(t, status, reply)
	if _, exists := visitor["pending_transfer"]; exists {
		t.Fatal("访客不应获得内部转接操作摘要")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, f.otherOwner, "", nil)
	if status != 404 {
		t.Fatalf("其他租户不能读取待转接会话: %d %v", status, reply["code"])
	}
	if _, err := f.admin.Exec(ctx, "UPDATE transfer_requests SET expires_at=now()-interval '1 second' WHERE id=$1", request); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, f.agent, "", nil)
	if status != 404 {
		t.Fatalf("过期转接目标不能继续读取原会话: %d %v", status, reply["code"])
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, f.owner, "", nil)
	if mustData(t, status, reply)["pending_transfer"] != nil {
		t.Fatal("过期请求不应返回可操作摘要")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE transfer_requests SET expires_at=now()+interval '30 seconds' WHERE id=$1", request); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/accept", f.agent, "", map[string]any{"transfer_request_id": request, "expected_version": target["version"]})
	accepted := mustData(t, status, reply)
	if accepted["current_agent_id"] != f.agent || accepted["pending_transfer"] != nil {
		t.Fatal("确认转接后应更新接待人并移除待确认摘要")
	}
}
