// 文件作用说明：使用真实PostgreSQL隔离schema验收并发容量、消息幂等、访客可见性及租户数据边界。
package chat

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// mustData 检查真实HTTP结果并返回统一data对象，失败不打印访客令牌。
func mustData(t *testing.T, status int, response map[string]any) map[string]any {
	t.Helper()
	if status != 200 {
		t.Fatalf("HTTP%d: code=%v message=%v", status, response["code"], response["message"])
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatal("响应缺少data对象")
	}
	return data
}

// TestVisitorIdentityAndParameters 验证展示手机号不恢复身份、访问重试去重、业务零值保留以及跨租户路由拒绝。
func TestVisitorIdentityAndParameters(t *testing.T) {
	f := newFixture(t)
	token, contact, conversation := f.visitor("13800000000")
	_, otherContact, otherConversation := f.visitor("13800000000")
	if contact == otherContact || conversation == otherConversation {
		t.Fatal("未验证手机号不能恢复其他访客历史")
	}
	status, reply := f.request("GET", "/api/v1/conversations/"+otherConversation, "", token, nil)
	if status != 404 {
		t.Fatalf("其他访客咨询应拒绝: %d %v", status, reply["code"])
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, f.otherOwner, "", nil)
	if status != 404 {
		t.Fatalf("跨租户咨询应拒绝: %d %v", status, reply["code"])
	}
	visitID := newID()
	input := map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": visitID, "context": map[string]any{"数量": 0, "开关": false, "中文": "张三&+%#"}, "tracking": map[string]any{"landing_url": "https://example.invalid/item?token=secret&phone=138&utm_source=广告#private"}}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", input)
	first := mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", input)
	again := mustData(t, status, reply)
	if first["conversation"].(map[string]any)["id"] != conversation || again["conversation"].(map[string]any)["id"] != conversation {
		t.Fatal("重进应复用同一未结束咨询")
	}
	var visits int
	var contextValue, tracking map[string]any
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM contact_visits WHERE event_id=$1", visitID).Scan(&visits); err != nil || visits != 1 {
		t.Fatalf("相同visit_id必须持久去重: %d %v", visits, err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT context,tracking FROM contact_visits WHERE event_id=$1", visitID).Scan(&contextValue, &tracking); err != nil {
		t.Fatal(err)
	}
	if contextValue["数量"] != float64(0) || contextValue["开关"] != false || contextValue["中文"] != "张三&+%#" {
		t.Fatalf("业务参数失真: %+v", contextValue)
	}
	if tracking["landing_url"] != "https://example.invalid/item?utm_source=%E5%B9%BF%E5%91%8A" {
		t.Fatalf("来源应清除敏感资料: %+v", tracking)
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "routing": map[string]string{"agent_id": f.otherOwner}})
	if status != 400 {
		t.Fatalf("动态目标覆盖默认应禁止: %d %v", status, reply["code"])
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM contacts").Scan(&count); err != nil || count != 0 {
		t.Fatalf("缺少租户上下文不应返回客户: %d %v", count, err)
	}
}

// TestConcurrentCapacityAndClose 验证多咨询竞争同一客服最后容量、同一咨询重复领取和重复结束均不重复占用或释放。
func TestConcurrentCapacityAndClose(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	const attempts = 12
	ids := make([]string, attempts)
	for index := range ids {
		_, _, ids[index] = f.visitor(fmt.Sprintf("138%08d", index))
	}
	statuses := make(chan int, attempts)
	var group sync.WaitGroup
	for _, id := range ids {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			status, _ := f.request("POST", "/api/v1/conversations/"+id+"/assignments/claim", f.agent, "", map[string]any{})
			statuses <- status
		}(id)
	}
	group.Wait()
	close(statuses)
	success := 0
	for status := range statuses {
		if status == 200 {
			success++
		} else if status != 409 {
			t.Fatalf("容量竞争出现意外状态: %d", status)
		}
	}
	if success != 1 {
		t.Fatalf("容量1应只有一个领取成功，得到%d", success)
	}
	var load, holds, assignments int
	var activeID string
	if err := f.admin.QueryRow(context.Background(), "SELECT current_load FROM agent_runtime WHERE membership_id=$1", f.agent).Scan(&load); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM agent_capacity_holds WHERE membership_id=$1 AND status!='released'", f.agent).Scan(&holds); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM conversation_assignments WHERE membership_id=$1 AND released_at IS NULL", f.agent).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if load != 1 || holds != 1 || assignments != 1 {
		t.Fatalf("容量事实不一致: load=%d holds=%d assignments=%d", load, holds, assignments)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT id::text FROM conversations WHERE current_agent_id=$1", f.agent).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		status, reply := f.request("POST", "/api/v1/conversations/"+activeID+"/assignments/claim", f.agent, "", map[string]any{})
		mustData(t, status, reply)
	}
	for index := 0; index < 2; index++ {
		status, reply := f.request("POST", "/api/v1/conversations/"+activeID+"/close", f.agent, "", map[string]any{})
		mustData(t, status, reply)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT current_load FROM agent_runtime WHERE membership_id=$1", f.agent).Scan(&load); err != nil || load != 0 {
		t.Fatalf("重复关闭必须仅释放一次容量: %d %v", load, err)
	}
}

// TestMessageIdempotencyAndVisibility 验证并发重试只落一条消息，内部备注不对访客开放，引用与阅读游标不能泄漏不可见消息。
func TestMessageIdempotencyAndVisibility(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	clientID := newID()
	body := map[string]any{"client_message_id": clientID, "content": "并发可靠消息"}
	const attempts = 12
	responses := make(chan map[string]any, attempts)
	statuses := make(chan int, attempts)
	var group sync.WaitGroup
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, body)
			statuses <- status
			responses <- reply
		}()
	}
	group.Wait()
	close(statuses)
	close(responses)
	for status := range statuses {
		if status != 200 {
			t.Fatalf("同内容消息重试应成功: %d", status)
		}
	}
	messageID := ""
	for response := range responses {
		item := mustData(t, 200, response)
		if messageID == "" {
			messageID = item["id"].(string)
		} else if item["id"] != messageID {
			t.Fatal("同一client_message_id产生多个消息编号")
		}
	}
	var count int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM messages WHERE client_message_id=$1", clientID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("消息只能持久化1条: %d %v", count, err)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": clientID, "content": "不同内容"})
	if status != 409 {
		t.Fatalf("相同幂等编号不同内容应冲突: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "内部秘密备注", "audience": "internal"})
	internal := mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	history := mustData(t, status, reply)
	for _, raw := range history["items"].([]any) {
		if raw.(map[string]any)["audience"] == "internal" {
			t.Fatal("访客历史泄漏内部消息")
		}
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/events", "", token, nil)
	events := mustData(t, status, reply)
	for _, raw := range events["items"].([]any) {
		if raw.(map[string]any)["audience"] == "internal" {
			t.Fatal("访客补拉泄漏内部事件")
		}
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "引用", "reply_to_id": internal["id"]})
	if status != 403 {
		t.Fatalf("访客不能引用内部消息: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", "", token, map[string]any{"last_read_seq": internal["seq"]})
	if status != 400 {
		t.Fatalf("访客不能读取内部消息游标: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+messageID+"/withdraw", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	history = mustData(t, status, reply)
	for _, raw := range history["items"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == messageID && item["content"] != "" {
			t.Fatal("撤回后历史仍显示正文")
		}
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.otherTenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM messages WHERE conversation_id=$1", conversation).Scan(&count)
	}); err != nil || count != 0 {
		t.Fatalf("跨租户RLS泄漏消息: %d %v", count, err)
	}
}
