// 文件作用说明：验证套餐关闭功能后的新增业务限制及历史、现有咨询继续可用，并检查接待资格变化的列表可见性。
package chat

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestFeatureRestrictionsPreserveExistingChat 验证权益关闭拦截新咨询、入口与标签，同时保留现有咨询发送和历史读取。
func TestFeatureRestrictionsPreserveExistingChat(t *testing.T) {
	f := newFixture(t)
	token, contact, conversation := f.visitor("")
	f.s.SetFeatureChecker(func(ctx context.Context, tx pgx.Tx, tenant, feature string) error {
		if tenant != f.tenant {
			t.Error("功能检查租户身份不正确")
		}
		return platform.Fail(403, "feature_not_available", "测试中此新增业务权益已关闭")
	})
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "原咨询继续沟通"})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visit_id": newID()})
	if status != 403 {
		t.Fatalf("关闭chat必须拦新咨询: %d %v", status, reply["code"])
	}
	for _, path := range []string{"access-links", "tags", "tag-categories"} {
		status, reply = f.request("POST", "/api/v1/tenant/"+path, f.owner, "", map[string]any{"name": "不应创建"})
		if status != 403 {
			t.Fatalf("关闭新增权益应拦%s: %d %v", path, status, reply["code"])
		}
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/contacts/"+contact, f.owner, "", map[string]any{"tag_ids": []string{newID()}})
	if status != 403 {
		t.Fatalf("关闭labels必须拦客户标签写入: %d %v", status, reply["code"])
	}
	status, reply = f.request("GET", "/api/v1/tenant/tags", f.owner, "", nil)
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	if status != 403 {
		t.Fatalf("关闭chat后结束再开属于新咨询，应拒绝: %d %v", status, reply["code"])
	}
	var count int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM contacts WHERE tenant_id=$1", f.tenant).Scan(&count); err != nil || count != 1 {
		t.Fatalf("新增业务被拒绝应回滚新建客户: %d %v", count, err)
	}
}

// TestAgentWithoutServingCannotListQueue 验证已关闭接待资格的普通成员不能通过列表读取自己无权打开的排队咨询资料。
func TestAgentWithoutServingCannotListQueue(t *testing.T) {
	f := newFixture(t)
	_, _, conversation := f.visitor("")
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET can_serve=false WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	p := f.principals[f.agent]
	p.CanServe = false
	f.principals[f.agent] = p
	status, reply := f.request("GET", "/api/v1/conversations", f.agent, "", nil)
	data := mustData(t, status, reply)
	if len(data["items"].([]any)) != 0 {
		t.Fatal("无接待资格的普通成员不应看到排队客户资料")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, f.agent, "", nil)
	if status != 404 {
		t.Fatalf("无接待资格成员详情应拒绝: %d %v", status, reply["code"])
	}
}
