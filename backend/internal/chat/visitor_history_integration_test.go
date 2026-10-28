// 文件作用说明：真实PostgreSQL验收可信访客窗口、指定客服与历史回落、过期容量释放，以及关闭跨客服历史的多条读取边界。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// revisitHistory 使用服务端令牌和可选提示重新进入，不使用浏览器时间作为窗口凭证。
func revisitHistory(t *testing.T, f *fixture, token, conversation string, extra map[string]any) map[string]any {
	t.Helper()
	in := map[string]any{"entry_key": f.entry, "visitor_token": token, "resume_conversation_id": conversation, "visit_id": newID()}
	for key, value := range extra {
		in[key] = value
	}
	status, reply := f.request("POST", "/api/v1/visitor/session", "", "", in)
	return mustData(t, status, reply)
}

// historySend 通过正式消息接口构造持久公开记录，同时返回真实消息编号和序号。
func historySend(t *testing.T, f *fixture, conversation, member, token, text string) map[string]any {
	t.Helper()
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", member, token, map[string]any{"client_message_id": newID(), "content": text})
	return mustData(t, status, reply)
}

// TestVisitorHistoryWindowAndTrustedResume 验证零窗口永久、按公开消息判窗、过期释放、参数合并和令牌恢复权限。
func TestVisitorHistoryWindowAndTrustedResume(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	token, _, conversation := f.visitor("13800000000")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at=now()-interval '10 days' WHERE id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	zero := revisitHistory(t, f, token, conversation, nil)
	if zero["resumed"] != true || zero["conversation"].(map[string]any)["id"] != conversation {
		t.Fatal("零窗口应永久复用当前合格接待")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"history_agent_minutes": 1})
	mustData(t, status, reply)
	historySend(t, f, conversation, "", token, "刷新公开消息窗口")
	within := revisitHistory(t, f, token, conversation, map[string]any{"context": map[string]any{"有无房产": "有", "零": 0}})
	if within["resumed"] != true {
		t.Fatal("最新公开消息应覆盖很久以前的创建时间")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET last_message_at=now()-interval '2 minutes',updated_at=now() WHERE id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	// 内部活动不会延长公开消息窗口；关闭历史池级开关也不影响租户的全局窗口规则。
	expired := revisitHistory(t, f, token, conversation, map[string]any{"context": map[string]any{"是否逾期": "无", "零": false}})
	next := expired["conversation"].(map[string]any)
	if expired["history_expired"] != true || expired["resumed"] != false || next["id"] == conversation || expired["previous_conversation_id"] != conversation {
		t.Fatal("超窗应新建咨询并返回明确恢复结果")
	}
	values := next["context"].(map[string]any)
	if values["有无房产"] != "有" || values["是否逾期"] != "无" || values["零"] != false {
		t.Fatal("超窗重建丢失已有业务资料或零值覆盖")
	}
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='closed' AND close_reason='history_window_expired'", conversation) != 1 || f.countRows("SELECT count(*) FROM agent_capacity_holds WHERE conversation_id=$1 AND status!='released'", conversation) != 0 {
		t.Fatal("超窗必须关闭旧咨询并释放旧容量")
	}
	if f.countRows("SELECT current_load FROM agent_runtime WHERE membership_id=$1", f.agent) != 0 {
		t.Fatal("人工领取入口超窗后不得保留旧占用")
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "resume_conversation_id": conversation, "profile": map[string]any{"phone": "13800000000"}})
	if status != 400 || reply["code"] != "invalid_resume" {
		t.Fatal("手机号和咨询编号不能恢复身份")
	}
	foreign, _, foreignConversation := f.visitor("13800000000")
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": foreign, "resume_conversation_id": conversation})
	if status != 403 || reply["code"] != "resume_not_allowed" {
		t.Fatal("可信令牌也不能提示其他客户的咨询")
	}
	if foreignConversation == next["id"] {
		t.Fatal("手机号不能合并两个访客")
	}
	for _, bad := range []any{-1, 1.5, 525601, "10"} {
		status, _ = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"history_agent_minutes": bad})
		if status != 400 {
			t.Fatal("窗口必须为0至525600整数")
		}
	}
}

// TestSpecifiedAgentAndHistoricalOfflineFallback 验证指定离线不可排队、容量资格不绕过，以及历史离线按正常池重新匹配。
func TestSpecifiedAgentAndHistoricalOfflineFallback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.owner)
	status, reply := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "routing": map[string]any{"agent_id": f.agent}})
	if status != 409 || reply["code"] != "specified_agent_offline" || reply["message"] != "无客服在线，暂时无法访问" {
		t.Fatalf("指定离线错误不稳定: %d %v", status, reply["code"])
	}
	if f.countRows("SELECT count(*) FROM conversations") != 0 {
		t.Fatal("指定客服离线不得留下队列或改派记录")
	}
	f.online(f.agent)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "routing": map[string]any{"agent_id": f.agent}})
	assigned := mustData(t, status, reply)
	if assigned["conversation"].(map[string]any)["current_agent_id"] != f.agent {
		t.Fatal("显式指定应优先于人工或轮询算法")
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "routing": map[string]any{"agent_id": f.agent}})
	if status != 409 || reply["code"] != "candidate_unavailable" {
		t.Fatal("指定客服不能绕过容量")
	}
	token, _, old := f.visitor("")
	status, reply = f.request("POST", "/api/v1/conversations/"+old+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+old+"/close", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE agent_runtime SET online_until=now()-interval '1 second' WHERE membership_id=$1", f.owner); err != nil {
		t.Fatal(err)
	}
	// 先释放另一条显式接待，使池内唯一合格客服确实有容量。
	explicit := assigned["conversation"].(map[string]any)["id"].(string)
	status, reply = f.request("POST", "/api/v1/conversations/"+explicit+"/close", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE routing_pools SET algorithm='least_load',history_priority=false WHERE id=$1", f.poolID); err != nil {
		t.Fatal(err)
	}
	resumed := revisitHistory(t, f, token, old, nil)
	if resumed["conversation"].(map[string]any)["current_agent_id"] != f.agent {
		t.Fatal("历史客服离线应走正常分配给其他合格客服")
	}
}

// TestHistoryPrivacyTransferAndSharedClosedChats 验证消息、补拉事件、引用、附件、未读、旧游标及结束记录均遵循当前分享策略。
func TestHistoryPrivacyTransferAndSharedClosedChats(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.owner)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	old := historySend(t, f, conversation, "", token, "转接前的客户秘密")
	status, reply = uploadQuickReplyMedia(t, f, f.owner, "private.png", avatarTestPNG(t, 4, 4))
	if status != 201 {
		t.Fatalf("上传失败HTTP%d", status)
	}
	file := reply["data"].(map[string]any)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.owner, "", map[string]any{"client_message_id": newID(), "message_type": "image", "attachment_id": file["id"]})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	shared := mustData(t, status, reply)
	if len(shared["items"].([]any)) != 2 {
		t.Fatal("默认开启分享应显示原客服消息")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"share_previous_chats": false})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/events?after_cursor="+url.QueryEscape(shared["sync_cursor"].(string)), f.agent, "", nil)
	if status != 409 || reply["code"] != "resync_required" {
		t.Fatal("分享策略变更必须使旧缓存游标失效")
	}
	current := historySend(t, f, conversation, "", token, "转接后的公开消息")
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	private := mustData(t, status, reply)
	items := private["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != current["id"] {
		t.Fatal("关闭分享不能显示其他客服接待期间消息")
	}
	if unreadValue(t, f.conversationList(f.agent), conversation) != 1 {
		t.Fatal("未读数不能计算不可见历史消息")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/events", f.agent, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		event := raw.(map[string]any)
		if event["event_type"] == "message.created" && event["payload"].(map[string]any)["id"] != current["id"] {
			t.Fatal("事件补拉泄漏转接前正文")
		}
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "不应引用", "reply_to_id": old["id"]})
	if status != 403 {
		t.Fatal("客服不能引用隐藏消息")
	}
	request, err := http.NewRequest("GET", f.server.URL+"/api/v1/attachments/"+file["id"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Test-Member", f.agent)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 && response.StatusCode != 403 {
		t.Fatalf("隐藏历史附件仍可下载: %d", response.StatusCode)
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.owner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 3 {
		t.Fatal("管理员应保留审计全量")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 3 {
		t.Fatal("访客仍能查看本人完整记录")
	}
	// 未知升级前段不能凭时间猜测，关闭分享时隐藏该段全部消息。
	if _, err := f.admin.Exec(ctx, "UPDATE conversation_assignments SET visibility_after_seq=NULL WHERE conversation_id=$1 AND membership_id=$2", conversation, f.agent); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("未知旧水位应保守隐藏")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	// 已结束咨询不原地重开；当前接待获得同空间同组的旧咨询共享授权。
	if _, err := f.admin.Exec(ctx, "UPDATE agent_runtime SET available=false WHERE membership_id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	next := revisitHistory(t, f, token, conversation, map[string]any{"routing": map[string]any{"agent_id": f.owner}})["conversation"].(map[string]any)["id"].(string)
	if next == conversation {
		t.Fatal("结束咨询不能原地重开")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+next+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	if status == 200 {
		t.Fatal("离线客服不能接受转接")
	}
	// 为非历史客服建立当前合法接待后测试共享权限，关闭开关保持拒绝。
	f.online(f.agent)
	status, reply = f.request("POST", "/api/v1/conversations/"+next+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET current_agent_id=$2 WHERE id=$1", conversation, f.owner); err != nil {
		t.Fatal(err)
	}
	status, _ = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	if status != 404 {
		t.Fatal("关闭分享不能读其他客服的旧咨询")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"share_previous_chats": true})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 3 {
		t.Fatal("开启分享当前接待应能查看同客户以前记录")
	}
	space := newID()
	if _, err := f.admin.Exec(ctx, "INSERT INTO service_spaces(id,tenant_id,name) VALUES($1,$2,'隔离历史空间')", space, f.tenant); err != nil {
		t.Fatal(err)
	}
	var oldSpace string
	if err := f.admin.QueryRow(ctx, "SELECT service_space_id::text FROM conversations WHERE id=$1", conversation).Scan(&oldSpace); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET service_space_id=$2 WHERE id=$1", conversation, space); err != nil {
		t.Fatal(err)
	}
	status, _ = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	if status != 404 {
		t.Fatal("开启分享不能扩展到其他服务空间")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET service_space_id=$2 WHERE id=$1", conversation, oldSpace); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, "DELETE FROM agent_group_members WHERE membership_id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	status, _ = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	if status != 404 {
		t.Fatal("历史分享不能跨越已撤销业务组授权")
	}

}

// historyBroadcast 将数据库真实事件送入正式广播通道，直接验证每位接收者的权限与引用裁剪。
func historyBroadcast(t *testing.T, f *fixture, conversation, message string, client *liveClient) bool {
	t.Helper()
	var event map[string]any
	if err := f.admin.QueryRow(t.Context(), "SELECT to_jsonb(e) FROM conversation_events e WHERE conversation_id=$1 AND event_type='message.created' AND payload->>'id'=$2", conversation, message).Scan(&event); err != nil {
		t.Fatal(err)
	}
	f.s.broadcast(t.Context(), f.tenant, conversation, "public", map[string]any{"type": "event", "data": event})
	select {
	case raw := <-client.send:
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatal(err)
		}
		payload := frame["data"].(map[string]any)["payload"].(map[string]any)
		if payload["reply"] != nil {
			t.Fatal("广播引用泄漏当前不可见正文")
		}
		return true
	default:
		return false
	}
}

// TestPrivateHistoryRedactsVisitorQuotesAndLivePush 验证访客可引用自己的旧消息，但新接待客服只能看到新消息正文，实时与HTTP一致。
func TestPrivateHistoryRedactsVisitorQuotesAndLivePush(t *testing.T) {
	f := newFixture(t)
	f.online(f.owner)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	old := historySend(t, f, conversation, "", token, "不可见原始引用内容")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	mustData(t, status, reply)
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"share_previous_chats": false})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "引用之后的新正文", "reply_to_id": old["id"]})
	quoted := mustData(t, status, reply)
	if quoted["reply"] == nil {
		t.Fatal("访客应保留本人完整引用")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	items := mustData(t, status, reply)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["reply"] != nil {
		t.Fatal("当前客服HTTP引用必须裁剪")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": old["seq"]})
	if status != 400 && status != 403 {
		t.Fatalf("隐藏旧消息不能推进已读水位:HTTP%d", status)
	}
	principal := f.principals[f.agent]
	client := &liveClient{actor: actor{TenantID: f.tenant, MemberID: f.agent, UserID: principal.UserID, Role: principal.Role, CanServe: true}, send: make(chan []byte, 2), cancel: func() {}}
	f.s.mu.Lock()
	f.s.clients[client] = struct{}{}
	f.s.mu.Unlock()
	defer func() { f.s.mu.Lock(); delete(f.s.clients, client); f.s.mu.Unlock() }()
	if historyBroadcast(t, f, conversation, old["id"].(string), client) {
		t.Fatal("旧消息实时广播不应送达新接待人")
	}
	if !historyBroadcast(t, f, conversation, quoted["id"].(string), client) {
		t.Fatal("新消息应送达且移除旧引用")
	}
	status, reply = f.request("GET", "/api/v1/tenant/chat-records/export?include_messages=true", f.agent, "", nil)
	if status != 403 {
		t.Fatal("普通客服不能利用管理员导出路径绕过历史策略")
	}
}

// TestGlobalHistoryPriorityAndChangedEntry 验证永久历史不依赖池级旧开关，显式目标更高优先，新入口不复用旧会话或上下文。
func TestGlobalHistoryPriorityAndChangedEntry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.owner)
	f.online(f.agent)
	token, _, old := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+old+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+old+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at=now()-interval '3 years',last_message_at=NULL WHERE id=$1", old); err != nil {
		t.Fatal(err)
	}
	prior := revisitHistory(t, f, token, old, nil)
	next := prior["conversation"].(map[string]any)
	if next["id"] == old || next["current_agent_id"] != f.agent {
		t.Fatal("0永久历史应覆盖旧池history_priority=false和人工接待算法")
	}
	// 现有窗口内改变显式目标也必须尊重新目标，不能悄悄复用历史客服。
	explicit := revisitHistory(t, f, token, next["id"].(string), map[string]any{"routing": map[string]any{"agent_id": f.owner}, "context": map[string]any{"仅旧入口": "保留"}})["conversation"].(map[string]any)
	if explicit["id"] == next["id"] || explicit["current_agent_id"] != f.owner {
		t.Fatal("指定客服必须高于当前历史接待")
	}
	link := f.createRoutingResource("access-links", map[string]any{"name": "另一独立入口", "reception_mode": "manual"})
	fresh := revisitHistory(t, f, token, explicit["id"].(string), map[string]any{"entry_key": link["entry_key"], "context": map[string]any{"新入口": "独立"}})["conversation"].(map[string]any)
	if fresh["id"] == explicit["id"] || fresh["context"].(map[string]any)["仅旧入口"] != nil || fresh["context"].(map[string]any)["新入口"] != "独立" {
		t.Fatal("切换入口应新建，不能继承旧入口会话业务上下文")
	}
	if f.countRows("SELECT count(*) FROM agent_capacity_holds WHERE conversation_id=$1 AND status!='released'", explicit["id"]) != 0 {
		t.Fatal("切换入口必须释放旧客服名额")
	}
}
