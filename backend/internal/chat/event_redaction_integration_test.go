// 文件作用说明：真实PostgreSQL回归已撤回引用的事件重放、HTTP幂等响应以及出站认领后延迟广播，防止旧载荷重新带回正文。
package chat

import (
	"encoding/json"
	"testing"
)

// assertWithdrawnReply 验证引用按当前数据库撤回状态重建，不信任历史事件副本中的旧正文。
func assertWithdrawnReply(t *testing.T, item map[string]any) {
	t.Helper()
	reply, ok := item["reply"].(map[string]any)
	if !ok || reply["withdrawn"] != true || reply["content"] != "" {
		t.Fatal("已撤回引用正文或标志仍使用旧副本")
	}
}

// TestWithdrawnQuoteEventsAndDelayedOutbox 验证引用目标撤回后补拉与延迟推送均没有旧内容，原编号重试仍返回规范化结果。
func TestWithdrawnQuoteEventsAndDelayedOutbox(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	source := historySend(t, f, conversation, "", token, "撤回后不应重新出现的原文")
	input := map[string]any{"client_message_id": newID(), "content": "保留的新回复正文", "reply_to_id": source["id"]}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, input)
	quoted := mustData(t, status, reply)
	// 模拟deliverOutbox在独立事务内已经认领并读取的旧事件载荷，之后才发生撤回。
	claimed := []map[string]any{}
	for _, id := range []any{source["id"], quoted["id"]} {
		var event map[string]any
		if err := f.admin.QueryRow(t.Context(), "SELECT to_jsonb(e) FROM conversation_events e WHERE conversation_id=$1 AND event_type='message.created' AND payload->>'id'=$2", conversation, id).Scan(&event); err != nil {
			t.Fatal(err)
		}
		claimed = append(claimed, event)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+source["id"].(string)+"/withdraw", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == quoted["id"] {
			assertWithdrawnReply(t, item)
		}
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/events", f.agent, "", nil)
	seen := false
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		event := raw.(map[string]any)
		if event["event_type"] == "message.created" {
			payload := event["payload"].(map[string]any)
			if payload["id"] == quoted["id"] {
				assertWithdrawnReply(t, payload)
				seen = true
			}
			if payload["id"] == source["id"] && payload["content"] != "" {
				t.Fatal("补拉仍包含已撤回主正文")
			}
		}
	}
	if !seen {
		t.Fatal("引用事件丢失")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, input)
	assertWithdrawnReply(t, mustData(t, status, reply))
	p := f.principals[f.agent]
	client := &liveClient{actor: actor{TenantID: f.tenant, MemberID: f.agent, UserID: p.UserID, Role: p.Role, CanServe: true}, send: make(chan []byte, 4), cancel: func() {}}
	f.s.mu.Lock()
	f.s.clients[client] = struct{}{}
	f.s.mu.Unlock()
	defer func() { f.s.mu.Lock(); delete(f.s.clients, client); f.s.mu.Unlock() }()
	for _, event := range claimed {
		f.s.broadcast(t.Context(), f.tenant, conversation, "public", map[string]any{"type": "event", "data": event})
		select {
		case raw := <-client.send:
			var frame map[string]any
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatal(err)
			}
			payload := frame["data"].(map[string]any)["payload"].(map[string]any)
			if payload["id"] == source["id"] && payload["content"] != "" {
				t.Fatal("已认领延迟事件重放了已撤回主正文")
			}
			if payload["id"] == quoted["id"] {
				assertWithdrawnReply(t, payload)
			}
		default:
			t.Fatal("合法延迟事件丢失")
		}
	}
}
