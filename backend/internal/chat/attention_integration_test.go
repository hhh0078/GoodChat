// 文件作用说明：通过真实PostgreSQL验证无消息新咨询、最早未读期限、多标签原子领取与已读停止提醒。
package chat

import (
	"sync"
	"sync/atomic"
	"testing"
)

// reminderItems 使用已认证客服身份领取提醒，只断言服务端实际返回的持久结果。
func (f *fixture) reminderItems(member string, initial bool) []any {
	f.t.Helper()
	status, reply := f.request("POST", "/api/v1/agent/attention-reminders/claim", member, "", map[string]any{"initial": initial})
	return mustData(f.t, status, reply)["items"].([]any)
}

// TestAttentionRemindersFollowEarliestUnread 验证空咨询确认、最早未读不被新消息延期及结束后停止。
func TestAttentionRemindersFollowEarliestUnread(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	if len(f.reminderItems(f.agent, false)) != 0 {
		t.Fatal("新咨询尚未达到默认10秒，不应提前重复提醒")
	}
	if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_assignments SET accepted_at=now()-interval '11 seconds' WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	items := f.reminderItems(f.agent, false)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "new" {
		t.Fatal("没有发消息的新咨询也必须超时提醒")
	}
	if len(f.reminderItems(f.agent, true)) != 0 {
		t.Fatal("同一接待段的重复事件不能绕过提醒间隔")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": "0"})
	mustData(t, status, reply)
	if len(f.reminderItems(f.agent, true)) != 0 {
		t.Fatal("实际打开空咨询确认后应停止提醒")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "最早未读消息"})
	first := mustData(t, status, reply)
	if len(f.reminderItems(f.agent, true)) != 1 {
		t.Fatal("查看后新来的访客消息应触发新提醒")
	}
	if _, err := f.admin.Exec(t.Context(), "UPDATE messages SET created_at=now()-interval '11 seconds' WHERE id=$1", first["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_attention_state SET last_reminded_at=now()-interval '11 seconds' WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "新消息不能推迟旧未读的期限"})
	last := mustData(t, status, reply)
	if len(f.reminderItems(f.agent, false)) != 1 {
		t.Fatal("重复期限必须跟随最早未读，不能被最新消息重置")
	}
	if len(f.reminderItems(f.otherOwner, true)) != 0 {
		t.Fatal("不能领取其他租户的客服提醒")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.owner, "", map[string]any{"last_read_seq": last["seq"]})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_attention_state SET last_reminded_at=now()-interval '11 seconds' WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	if len(f.reminderItems(f.agent, false)) != 1 {
		t.Fatal("旁观管理员不能代替接待客服清除提醒")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": last["seq"]})
	mustData(t, status, reply)
	if len(f.reminderItems(f.agent, true)) != 0 {
		t.Fatal("接待客服确认实际已读后应停止提醒")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "即将结束的未读"})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/close", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	if len(f.reminderItems(f.agent, true)) != 0 {
		t.Fatal("咨询结束后不能继续重复提醒")
	}
}

// TestAttentionClaimIsSharedAcrossTabs 验证同一成员并行领取只能一个请求成功，无归属队列不代领。
func TestAttentionClaimIsSharedAcrossTabs(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	_, _, conversation := f.visitor("")
	if len(f.reminderItems(f.agent, true)) != 0 {
		t.Fatal("无归属排队咨询不属于某成员的个人重复提醒")
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	var count atomic.Int64
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func() { defer group.Done(); count.Add(int64(len(f.reminderItems(f.agent, true)))) }()
	}
	group.Wait()
	if count.Load() != 1 {
		t.Fatalf("8个并行标签只应领取一次，实际%d", count.Load())
	}
	if _, err := f.admin.Exec(t.Context(), "UPDATE conversations SET current_agent_id=$2 WHERE id=$1", conversation, f.owner); err != nil {
		t.Fatal(err)
	}
	if len(f.reminderItems(f.agent, true)) != 0 {
		t.Fatal("失去当前接待归属后应停止提醒")
	}
}

// TestTenantReminderSettingsValidate 保存真实租户配置并拒绝非法间隔、未知音色与普通客服修改。
func TestTenantReminderSettingsValidate(t *testing.T) {
	f := newFixture(t)
	status, reply := f.request("GET", "/api/v1/tenant/chat-settings", f.owner, "", nil)
	settings := mustData(t, status, reply)
	if settings["repeat_unread_seconds"] != float64(10) || settings["notification_sound_id"] != "gentle" {
		t.Fatal("出厂提醒必须为10秒与默认柔和音")
	}
	for _, body := range []map[string]any{{"repeat_unread_seconds": 0}, {"repeat_unread_seconds": 1.5}, {"notification_sound_id": "https://bad.invalid/sound.wav"}} {
		status, _ = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", body)
		if status != 400 {
			t.Fatal("不合法提醒设置未拒绝")
		}
	}
	status, _ = f.request("PATCH", "/api/v1/tenant/chat-settings", f.agent, "", map[string]any{"repeat_unread_seconds": 5})
	if status != 403 {
		t.Fatal("普通成员不能修改租户提醒配置")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"repeat_unread_seconds": 12, "notification_sound_id": "glass"})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/entries/"+f.entry, "", "", nil)
	if mustData(t, status, reply)["notification_sound_id"] != "glass" {
		t.Fatal("访客应读取租户已保存提示音")
	}
}
