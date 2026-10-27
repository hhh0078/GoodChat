// 文件作用说明：验证接待动作幂等、确认转接容量以及运营接口的真实持久化和错误回滚边界。
package chat

import (
	"context"
	"sync"
	"testing"
)

// TestConcurrentAssignmentCommandIdempotency 验证同一操作编号并发重试全部返回原结果，不出现第二次插入幂等记录冲突。
func TestConcurrentAssignmentCommandIdempotency(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	_, _, conversation := f.visitor("")
	const attempts = 16
	statuses := make(chan int, attempts)
	var group sync.WaitGroup
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			status, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{}, map[string]string{"Idempotency-Key": "same-command"})
			statuses <- status
		}()
	}
	group.Wait()
	close(statuses)
	for status := range statuses {
		if status != 200 {
			t.Fatalf("同操作幂等重试应全部成功，得到%d", status)
		}
	}
	var commands, holds int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM chat_commands").Scan(&commands); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM agent_capacity_holds WHERE status!='released'").Scan(&holds); err != nil {
		t.Fatal(err)
	}
	if commands != 1 || holds != 1 {
		t.Fatalf("幂等动作应仅存1记录和1占位: commands=%d holds=%d", commands, holds)
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{"reason": "不同请求"}, map[string]string{"Idempotency-Key": "same-command"})
	if status != 409 {
		t.Fatalf("同幂等键不同内容应冲突: %d %v", status, reply["code"])
	}
}

// TestTransferConfirmationAndCloseRace 验证待确认转接原客服继续占位、目标预占、并发确认与结束最终一致释放容量。
func TestTransferConfirmationAndCloseRace(t *testing.T) {
	f := newFixture(t)
	f.online(f.owner)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	if _, err := f.admin.Exec(context.Background(), "UPDATE routing_pools SET assignment_mode='confirm' WHERE id=$1", f.poolID); err != nil {
		t.Fatal(err)
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	data := mustData(t, status, reply)
	if data["status"] != "assigned" {
		t.Fatal("确认模式领取后应等待接单")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/accept", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	mustData(t, status, reply)
	var request string
	var total int
	if err := f.admin.QueryRow(context.Background(), "SELECT id::text FROM transfer_requests WHERE conversation_id=$1 AND status='pending'", conversation).Scan(&request); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT sum(current_load) FROM agent_runtime WHERE tenant_id=$1", f.tenant).Scan(&total); err != nil || total != 2 {
		t.Fatalf("转接确认前原客服和目标应共占2位: %d %v", total, err)
	}
	var group sync.WaitGroup
	statuses := make(chan int, 2)
	group.Add(2)
	go func() {
		defer group.Done()
		status, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/accept", f.owner, "", map[string]any{"transfer_request_id": request})
		statuses <- status
	}()
	go func() {
		defer group.Done()
		status, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
		statuses <- status
	}()
	group.Wait()
	close(statuses)
	for status := range statuses {
		if status != 200 && status != 409 {
			t.Fatalf("转接确认关闭竞态意外状态: %d", status)
		}
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT sum(current_load) FROM agent_runtime WHERE tenant_id=$1", f.tenant).Scan(&total); err != nil || total != 0 {
		t.Fatalf("结束后不得遗留或重复释放容量: %d %v", total, err)
	}
	var open int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM conversation_assignments WHERE conversation_id=$1 AND released_at IS NULL", conversation).Scan(&open); err != nil || open != 0 {
		t.Fatalf("结束后不得残留正式接待: %d %v", open, err)
	}
}

// TestOperationsPersistenceAndValidation 验证设置、偏好、话术和规则落库，非法整数在事务前拒绝且没有副作用。
func TestOperationsPersistenceAndValidation(t *testing.T) {
	f := newFixture(t)
	status, reply := f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": false, "welcome_text": "已保存欢迎语", "welcome_delay_seconds": 0})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/chat-settings", f.owner, "", nil)
	settings := mustData(t, status, reply)
	if settings["welcome_enabled"] != false || settings["welcome_text"] != "已保存欢迎语" {
		t.Fatalf("设置未持久化: %+v", settings)
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/notification-preferences", f.agent, "", map[string]any{"volume": 0, "sound_enabled": false})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/notification-preferences", f.agent, "", nil)
	prefs := mustData(t, status, reply)
	if prefs["volume"] != float64(0) || prefs["sound_enabled"] != false {
		t.Fatalf("零值偏好未持久化: %+v", prefs)
	}
	status, reply = f.request("POST", "/api/v1/tenant/quick-replies", f.owner, "", map[string]any{"title": "公共测试话术", "content": "真实持久话术", "scope": "public", "sort": 0})
	created := mustData(t, status, reply)
	status, reply = f.request("PATCH", "/api/v1/tenant/quick-replies/"+created["id"].(string), f.owner, "", map[string]any{"sort": 2})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/tenant/quick-replies", f.owner, "", map[string]any{"title": "不得保存的小数", "content": "无副作用", "sort": 1.5})
	if status != 400 {
		t.Fatalf("小数排序应拒绝: %d %v", status, reply["code"])
	}
	var count int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM quick_replies WHERE title='不得保存的小数'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("校验错误不能先落库再报错: %d %v", count, err)
	}
	status, reply = f.request("POST", "/api/v1/tenant/quick-replies", f.agent, "", map[string]any{"title": "越权公共话术", "content": "拒绝", "scope": "public"})
	if status != 403 {
		t.Fatalf("普通客服不能创建公共话术: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", "/api/v1/tenant/keyword-rules", f.owner, "", map[string]any{"name": "价格回复", "pattern": "价格", "reply": "请联系人工", "priority": 10, "cooldown_seconds": 0})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/keyword-rules", f.owner, "", nil)
	rules := mustData(t, status, reply)
	if len(rules["items"].([]any)) != 1 {
		t.Fatal("规则未持久化")
	}
	token, _, conversation := f.visitor("")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "咨询价格"})
	mustData(t, status, reply)
	if err := f.s.runAutomatic(context.Background(), f.tenant); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM messages WHERE sender_kind='system' AND content='请联系人工'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("规则任务未真实回复: %d %v", count, err)
	}
	if err := f.s.runAutomatic(context.Background(), f.tenant); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM messages WHERE sender_kind='system' AND content='请联系人工'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("自动任务重复执行应幂等: %d %v", count, err)
	}
}
