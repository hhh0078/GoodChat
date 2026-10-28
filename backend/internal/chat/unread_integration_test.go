// 文件作用说明：通过真实消息、已读与心跳接口验证成员独立未读数和只由公开消息驱动的咨询排序。
package chat

import (
	"testing"
)

// conversationList 从真实HTTP接口读取当前成员可见咨询，用于断言未读与排序而非模拟前端计数。
func (f *fixture) conversationList(member string) []map[string]any {
	f.t.Helper()
	status, reply := f.request("GET", "/api/v1/conversations", member, "", nil)
	items := mustData(f.t, status, reply)["items"].([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, item.(map[string]any))
	}
	return result
}

// unreadValue 返回指定咨询的真实未读数，缺失咨询或字段即测试失败。
func unreadValue(t *testing.T, items []map[string]any, id string) float64 {
	t.Helper()
	for _, item := range items {
		if item["id"] == id {
			value, ok := item["unread_count"].(float64)
			if !ok {
				t.Fatal("未读数字段缺失")
			}
			return value
		}
	}
	t.Fatal("未找到咨询")
	return -1
}

// TestUnreadCountsArePersonalAndMessageOrdered 验证公开访客消息计数、原编号重试去重、读取不乱排序和其他成员已读不代清。
func TestUnreadCountsArePersonalAndMessageOrdered(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, first := f.visitor("")
	_, _, second := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+first+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	client := newID()
	input := map[string]any{"client_message_id": client, "content": "需要客服读取的消息"}
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/messages", "", token, input)
	message := mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/messages", "", token, input)
	mustData(t, status, reply)
	items := f.conversationList(f.agent)
	if items[0]["id"] != first || unreadValue(t, items, first) != 1 || unreadValue(t, items, second) != 0 {
		t.Fatal("新消息应置顶且幂等重试只计一次")
	}
	var beforeRead, afterRead int64
	if err := f.admin.QueryRow(t.Context(), "SELECT next_event_seq FROM conversations WHERE id=$1", first).Scan(&beforeRead); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/read-cursor", f.owner, "", map[string]any{"last_read_seq": message["seq"]})
	mustData(t, status, reply)
	if err := f.admin.QueryRow(t.Context(), "SELECT next_event_seq FROM conversations WHERE id=$1", first).Scan(&afterRead); err != nil {
		t.Fatal(err)
	}
	if beforeRead != afterRead {
		t.Fatal("旁观管理员个人已读不能广播成接待客服已读")
	}
	if unreadValue(t, f.conversationList(f.agent), first) != 1 || unreadValue(t, f.conversationList(f.owner), first) != 0 {
		t.Fatal("成员自己的已读游标不应被他人代清")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": message["seq"]})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "这是内部备注", "audience": "internal"})
	mustData(t, status, reply)
	if unreadValue(t, f.conversationList(f.agent), first) != 0 {
		t.Fatal("已读后应归零，内部备注不能计入收到的访客消息")
	}
	// 第二个咨询产生公开消息后，读取第一个咨询或续在线租约均不能把第一个挪回顶部。
	secondToken, _, third := f.visitor("")
	status, reply = f.request("POST", "/api/v1/conversations/"+third+"/messages", "", secondToken, map[string]any{"client_message_id": newID(), "content": "较新的消息"})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": message["seq"]})
	mustData(t, status, reply)
	if err := f.s.heartbeat(t.Context(), actor{TenantID: f.tenant, MemberID: f.agent, UserID: f.principals[f.agent].UserID, Role: "agent", CanServe: true}); err != nil {
		t.Fatal(err)
	}
	if f.conversationList(f.agent)[0]["id"] != third {
		t.Fatal("已读和心跳不应改变公开消息排序")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+third+"/messages/"+mustLastVisitorMessage(t, f, third, secondToken)+"/withdraw", "", secondToken, map[string]any{})
	mustData(t, status, reply)
	if unreadValue(t, f.conversationList(f.agent), third) != 0 {
		t.Fatal("已撤回消息不再保留红色未读数")
	}
	if len(f.conversationList(f.otherOwner)) != 0 {
		t.Fatal("跨租户不能获取未读或咨询信息")
	}
}

// mustLastVisitorMessage 通过授权历史接口取得最后一条消息，避免测试绕过业务身份读取数据。
func mustLastVisitorMessage(t *testing.T, f *fixture, id, token string) string {
	t.Helper()
	status, reply := f.request("GET", "/api/v1/conversations/"+id+"/messages", "", token, nil)
	items := mustData(t, status, reply)["items"].([]any)
	if len(items) == 0 {
		t.Fatal("缺少测试消息")
	}
	return items[len(items)-1].(map[string]any)["id"].(string)
}
