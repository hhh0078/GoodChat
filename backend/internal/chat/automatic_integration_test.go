// 文件作用说明：在真实PostgreSQL验证欢迎语延迟任务跨重连与服务重建不重复，关键词优先级、冷却、版本撤销与关闭取消均持久生效。
package chat

import (
	"context"
	"sync"
	"testing"

	"supportdesk/backend/internal/platform"
)

// countRows 从隔离schema读取单个计数，错误立即中止测试。
func (f *fixture) countRows(query string, args ...any) int {
	f.t.Helper()
	var count int
	if err := f.admin.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		f.t.Fatal(err)
	}
	return count
}

// runWorkers 使用多个真实事务模拟同一租户的并发调度进程，不依靠内存标记保存执行结果。
func runWorkers(t *testing.T, s *Service, tenant string) {
	t.Helper()
	var group sync.WaitGroup
	errors := make(chan error, 6)
	for index := 0; index < 6; index++ {
		group.Add(1)
		go func() { defer group.Done(); errors <- s.runAutomatic(context.Background(), tenant) }()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestWelcomePersistentDelayReconnectAndCancellation 验证延迟前不发送、重连不重复建任务、服务重建后并发执行一次、关闭后旧任务取消且不污染新咨询。
func TestWelcomePersistentDelayReconnectAndCancellation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	status, reply := f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": true, "welcome_text": "持久延迟欢迎语", "welcome_delay_seconds": 60, "keywords_enabled": false})
	mustData(t, status, reply)
	token, _, conversation := f.visitor("")
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND job_type='welcome' AND status='queued' AND run_after>=created_at+interval '59 seconds'", conversation) != 1 {
		t.Fatal("欢迎语未保存真实延迟任务")
	}
	if err := f.s.runAutomatic(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='system'", conversation) != 0 {
		t.Fatal("延迟未到不能提前发送")
	}
	visit := newID()
	for index := 0; index < 4; index++ {
		status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": visit})
		if mustData(t, status, reply)["conversation"].(map[string]any)["id"] != conversation {
			t.Fatal("重连重建了咨询")
		}
		if index == 1 {
			visit = newID()
		}
	}
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND job_type='welcome'", conversation) != 1 {
		t.Fatal("刷新或初始化重试重复建立欢迎任务")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	restarted := New(f.pool, testAuthorizer{f.principals}, platform.Config{})
	runWorkers(t, restarted, f.tenant)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='system' AND content='持久延迟欢迎语'", conversation) != 1 || f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='completed' AND attempts=1", conversation) != 1 {
		t.Fatal("服务重建或并发调度重复发送欢迎语")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	history := mustData(t, status, reply)["items"].([]any)
	if len(history) != 1 || history[0].(map[string]any)["sender_kind"] != "system" || history[0].(map[string]any)["sender_name"] != "系统" {
		t.Fatal("历史消息必须明确标注系统来源，不能显示为空或冒充人工")
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1", conversation) != 1 {
		t.Fatal("执行后再次进入重复创建欢迎任务")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	next := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	if next == conversation || f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='queued'", next) != 1 {
		t.Fatal("新的独立咨询应有自己的欢迎任务")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+next+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1", next); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, restarted, f.tenant)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='cancelled'", next) != 1 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='system'", next) != 0 {
		t.Fatal("已结束咨询的未执行欢迎任务应取消，不能迟到发送")
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	disabled := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": false, "welcome_text": "更新后的配置"})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1", disabled); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, restarted, f.tenant)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='cancelled'", disabled) != 1 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='system'", disabled) != 0 {
		t.Fatal("关闭欢迎语后待执行旧任务必须取消")
	}
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND content='持久延迟欢迎语'", conversation) != 1 {
		t.Fatal("更新配置不能改写已经发送的欢迎语历史")
	}
}

// TestKeywordPriorityCooldownAndVersionCancellation 验证只匹配访客新文字，单消息最高优先级、幂等与冷却生效，规则改版取消旧任务而不覆盖新回复。
func TestKeywordPriorityCooldownAndVersionCancellation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	status, reply := f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": false, "keywords_enabled": true})
	mustData(t, status, reply)
	for _, rule := range []map[string]any{{"name": "低优先级", "pattern": "价格", "match_mode": "contains", "reply": "低优先级回复", "priority": 1, "cooldown_seconds": 60}, {"name": "完整匹配", "pattern": "价格", "match_mode": "exact", "reply": "高优先级回复", "priority": 10, "cooldown_seconds": 60}, {"name": "包含匹配", "pattern": "价", "match_mode": "contains", "reply": "包含匹配回复", "priority": 9, "cooldown_seconds": 60}} {
		status, reply = f.request("POST", "/api/v1/tenant/keyword-rules", f.owner, "", rule)
		mustData(t, status, reply)
	}
	token, _, conversation := f.visitor("")
	client := newID()
	for index := 0; index < 3; index++ {
		status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": client, "content": "价格"})
		mustData(t, status, reply)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "价格"})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1", conversation) != 1 {
		t.Fatal("访客重试与同规则冷却不应重复建立任务，也不能回退低优先级规则")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "咨询价格"})
	mustData(t, status, reply)
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='system' AND content IN('高优先级回复','包含匹配回复')", conversation) != 2 || f.countRows("SELECT count(*) FROM messages WHERE sender_kind='system' AND content='低优先级回复'") != 0 {
		t.Fatal("关键词最高优先级或完全/包含匹配口径错误")
	}
	f.online(f.agent)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "价格"})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1", conversation) != 2 {
		t.Fatal("客服文字、系统回复或历史补拉不能触发关键词循环")
	}
	status, reply = f.request("POST", "/api/v1/tenant/keyword-rules", f.owner, "", map[string]any{"name": "变更规则", "pattern": "变更", "reply": "旧版不应发送", "priority": 20, "cooldown_seconds": 60})
	rule := mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "变更"})
	mustData(t, status, reply)
	status, reply = f.request("PATCH", "/api/v1/tenant/keyword-rules/"+rule["id"].(string), f.owner, "", map[string]any{"reply": "新版准确回复"})
	mustData(t, status, reply)
	if err := f.s.runAutomatic(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE rule_id=$1 AND status='cancelled'", rule["id"]) != 1 || f.countRows("SELECT count(*) FROM messages WHERE content='旧版不应发送'") != 0 {
		t.Fatal("改版后旧任务应被取消而非发送迟到结果")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "变更"})
	mustData(t, status, reply)
	if err := f.s.runAutomatic(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM messages WHERE content='新版准确回复'") != 1 {
		t.Fatal("取消旧版本后新触发应使用已保存的新规则")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"keywords_enabled": false})
	mustData(t, status, reply)
	before := f.countRows("SELECT count(*) FROM chat_operation_jobs")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "变更"})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs") != before {
		t.Fatal("关键词总开关关闭后不得建立任务")
	}
}
