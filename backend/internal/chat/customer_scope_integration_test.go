// 文件作用说明：在真实PostgreSQL验证客户列表和访问轨迹只向有效接待客服或本租户管理员开放，关闭与转接后立即撤销额外资料读取范围。
package chat

import (
	"context"
	"testing"
)

// contactListIDs 通过真实资源接口读取可见客户编号，断言接口成功且不依赖菜单隐藏。
func (f *fixture) contactListIDs(member string) []string {
	f.t.Helper()
	status, reply := f.request("GET", "/api/v1/tenant/contacts", member, "", nil)
	items := mustData(f.t, status, reply)["items"].([]any)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.(map[string]any)["id"].(string))
	}
	return ids
}

// TestContactScopeRequiresCurrentAssignment 验证未接待、无接待资格和其他租户不能列出客户或访问轨迹，结束后不能凭旧会话获取客户后续context。
func TestContactScopeRequiresCurrentAssignment(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	f.online(f.owner)
	token, contact, conversation := f.visitor("13800000001")
	otherToken, otherContact, otherConversation := f.visitor("13800000002")
	_, _, _ = f.visitor("13800000003")
	if ids := f.contactListIDs(f.agent); len(ids) != 0 {
		t.Fatalf("未接待客服不应列出客户: %d", len(ids))
	}
	if ids := f.contactListIDs(f.owner); len(ids) != 3 {
		t.Fatalf("租户管理员应能列出本租户3位客户: %d", len(ids))
	}
	if ids := f.contactListIDs(f.otherOwner); len(ids) != 0 {
		t.Fatal("其他租户管理员不能列出本租户客户")
	}
	for _, member := range []string{f.agent, f.otherOwner} {
		status, reply := f.request("GET", "/api/v1/tenant/contacts/"+contact+"/visits", member, "", nil)
		if status != 404 {
			t.Fatalf("无接待授权轨迹应拒绝: %d %v", status, reply["code"])
		}
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+otherConversation+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	if ids := f.contactListIDs(f.agent); len(ids) != 1 || ids[0] != contact {
		t.Fatalf("普通客服只能列出当前接待客户: %+v", ids)
	}
	status, reply = f.request("GET", "/api/v1/tenant/contacts/"+contact+"/visits", f.agent, "", nil)
	visits := mustData(t, status, reply)["items"].([]any)
	if len(visits) != 1 || visits[0].(map[string]any)["context"].(map[string]any)["布尔"] != false {
		t.Fatal("当前接待客户的合法访问上下文应保留")
	}
	status, reply = f.request("GET", "/api/v1/tenant/contacts/"+otherContact+"/visits", f.agent, "", nil)
	if status != 404 {
		t.Fatalf("不能读取他人接待客户轨迹: %d %v", status, reply["code"])
	}
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET can_serve=false WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	p := f.principals[f.agent]
	p.CanServe = false
	f.principals[f.agent] = p
	if ids := f.contactListIDs(f.agent); len(ids) != 0 {
		t.Fatal("无接待资格的普通成员不能列出客户档案")
	}
	status, reply = f.request("GET", "/api/v1/tenant/contacts/"+contact+"/visits", f.agent, "", nil)
	if status != 404 {
		t.Fatalf("无接待资格成员不能读取访问轨迹: %d %v", status, reply["code"])
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET can_serve=true WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	p.CanServe = true
	f.principals[f.agent] = p
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	if ids := f.contactListIDs(f.agent); len(ids) != 0 {
		t.Fatal("已结束会话不能继续授予客户档案权限")
	}
	status, reply = f.request("GET", "/api/v1/tenant/contacts/"+contact+"/visits", f.agent, "", nil)
	if status != 404 {
		t.Fatalf("结束后不能读取客户后续轨迹: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+otherConversation+"/close", "", otherToken, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID(), "context": map[string]any{"新咨询业务": "不对旧接待人开放"}})
	next := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	status, reply = f.request("POST", "/api/v1/conversations/"+next+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/contacts/"+contact+"/visits", f.agent, "", nil)
	if status != 404 {
		t.Fatalf("旧接待关系不能读取重新咨询的业务context: %d %v", status, reply["code"])
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET can_serve=false WHERE id=$1", f.owner); err != nil {
		t.Fatal(err)
	}
	owner := f.principals[f.owner]
	owner.CanServe = false
	f.principals[f.owner] = owner
	if ids := f.contactListIDs(f.owner); len(ids) != 3 {
		t.Fatal("纯管理成员仍可管理本租户客户")
	}
	status, reply = f.request("GET", "/api/v1/tenant/contacts/"+contact+"/visits", f.owner, "", nil)
	mustData(t, status, reply)
}

// TestTransferredOrRevokedContactScope 验证转出客服即时失去客户资料与轨迹范围，当前接待人的会话详情仍可用于服务。
func TestTransferredOrRevokedContactScope(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	f.online(f.owner)
	_, contact, conversation := f.visitor("13800000004")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	mustData(t, status, reply)
	if ids := f.contactListIDs(f.agent); len(ids) != 0 {
		t.Fatal("转出客服不应继续列出客户")
	}
	status, reply = f.request("GET", "/api/v1/tenant/contacts/"+contact+"/visits", f.agent, "", nil)
	if status != 404 {
		t.Fatalf("转出后访问轨迹应拒绝: %d %v", status, reply["code"])
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, f.owner, "", nil)
	if mustData(t, status, reply)["contact"].(map[string]any)["id"] != contact {
		t.Fatal("当前接待人会话客户资料不应受损")
	}
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.owner); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/tenant/contacts", "/api/v1/tenant/contacts/" + contact + "/visits"} {
		status, reply = f.request("GET", path, f.owner, "", nil)
		if status != 403 {
			t.Fatalf("撤权后旧身份不能继续读取: %d %v", status, reply["code"])
		}
	}
}
