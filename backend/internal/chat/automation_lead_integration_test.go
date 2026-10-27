// 文件作用说明：在独立真实PostgreSQL与HTTP验证留言附件主动共享、权限撤销和通知队列状态，不发送真实收件邮件。
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// uploadLeadFixture 上传真实文本附件并返回内部编号，调用方明确选择测试访客或成员身份。
func uploadLeadFixture(t *testing.T, f *fixture, token, member string) string {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "lead-file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte("真实HTTP留言附件内容")); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("POST", f.server.URL+"/api/v1/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if member != "" {
		request.Header.Set("X-Test-Member", member)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 201 {
		t.Fatalf("附件上传失败HTTP%d", response.StatusCode)
	}
	return result["data"].(map[string]any)["id"].(string)
}

// readLeadFixture 读取真实私有文件响应，测试不把API成功当作实际浏览器保存。
func readLeadFixture(t *testing.T, f *fixture, id, member, token string) (int, string) {
	t.Helper()
	request, err := http.NewRequest("GET", f.server.URL+"/api/v1/attachments/"+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if member != "" {
		request.Header.Set("X-Test-Member", member)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

// leadFixtureBody 使用合成客户资料构造稳定留言，不包含真实手机号或收件地址。
func leadFixtureBody(f *fixture, conversation string, files ...string) map[string]any {
	return map[string]any{"entry_key": f.entry, "conversation_id": conversation, "client_request_id": newID(), "display_name": "合成留言访客", "phone": "13800000001", "content": "不应进入提醒邮件的合成客户正文", "attachment_ids": files}
}

// TestLeadAttachmentSharingAndAuthorization 验证主动共享、幂等、撤回独立性、当前组权限和封禁优先。
func TestLeadAttachmentSharingAndAuthorization(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token, contact, conversation := f.visitor("")
	otherToken, _, _ := f.visitor("")
	file := uploadLeadFixture(t, f, token, "")
	oversized := newID()
	if _, err := f.admin.Exec(ctx, "INSERT INTO attachments(id,tenant_id,uploader_id,uploader_kind,file_name,storage_key,content_type,size_bytes,sha256) VALUES($1::uuid,$2,$3,'visitor','oversized.txt',$1::uuid::text,'text/plain',20971521,$4)", oversized, f.tenant, contact, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if status, _ := f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation, oversized)); status != 403 {
		t.Fatal("即使内部元数据异常，超过20MiB的附件也不能被留言共享")
	}
	status, _ := f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation, file, file, file, file))
	if status != 400 {
		t.Fatal("留言必须限制为最多3个附件")
	}
	foreign := uploadLeadFixture(t, f, otherToken, "")
	staff := uploadLeadFixture(t, f, "", f.owner)
	for _, id := range []string{foreign, staff} {
		status, _ := f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation, id))
		if status != 403 {
			t.Fatal("别访客或成员上传的附件不能冒名提交留言")
		}
	}
	body := leadFixtureBody(f, conversation, file)
	status, reply := f.request("POST", "/api/v1/visitor/leads", "", token, body)
	lead := mustData(t, status, reply)
	status, reply = f.request("PATCH", "/api/v1/tenant/leads/"+lead["id"].(string), f.owner, "", map[string]any{"status": "following", "notes": "内部跟进不应被状态切换清空"})
	mustData(t, status, reply)
	status, reply = f.request("PATCH", "/api/v1/tenant/leads/"+lead["id"].(string), f.owner, "", map[string]any{"status": "resolved"})
	updated := mustData(t, status, reply)
	if updated["notes"] != "内部跟进不应被状态切换清空" {
		t.Fatal("仅切换状态不能清空内部跟进备注")
	}
	if f.countRows("SELECT count(*) FROM offline_lead_attachments WHERE lead_id=$1", lead["id"]) != 1 || f.countRows("SELECT count(*) FROM lead_notification_jobs") != 0 {
		t.Fatal("附件必须真实保存，默认关闭通知不能建立邮件任务")
	}
	attachments := lead["attachments"].([]any)
	metadata := attachments[0].(map[string]any)
	if metadata["id"] != file {
		t.Fatal("留言响应缺少实际附件")
	}
	if _, ok := metadata["storage_key"]; ok {
		t.Fatal("不能泄漏私有存储路径")
	}
	for _, member := range []string{f.owner, f.agent} {
		code, content := readLeadFixture(t, f, file, member, "")
		if code != 200 || content != "真实HTTP留言附件内容" {
			t.Fatal("管理员及有效同组客服应能读取真实留言附件")
		}
	}
	if f.countRows("SELECT count(*) FROM chat_audit_logs WHERE action='lead.created' AND target_id=$1", lead["id"]) != 1 || f.countRows("SELECT count(*) FROM chat_audit_logs WHERE action='lead_attachment.download_authorized' AND target_id=$1", file) != 2 {
		t.Fatal("留言主动共享和授权读取需要可追踪审计")
	}
	for _, member := range []string{f.otherOwner} {
		code, _ := readLeadFixture(t, f, file, member, "")
		if code != 404 {
			t.Fatal("其他租户不能下载留言附件")
		}
	}
	if code, _ := readLeadFixture(t, f, file, "", otherToken); code != 404 {
		t.Fatal("同租户其他访客不能下载留言附件")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "message_type": "file", "attachment_id": file})
	message := mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+message["id"].(string)+"/withdraw", "", token, map[string]any{})
	mustData(t, status, reply)
	if code, _ := readLeadFixture(t, f, file, f.owner, ""); code != 200 {
		t.Fatal("聊天撤回不能删除此前主动建立的独立留言共享")
	}
	status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, body)
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM offline_lead_attachments WHERE lead_id=$1", lead["id"]) != 1 {
		t.Fatal("留言重试不能重复共享附件")
	}
	status, _ = f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation, file))
	if status != 403 {
		t.Fatal("不能把已撤回的聊天附件自动重新挂到新留言")
	}
	body["attachment_ids"] = []string{}
	status, _ = f.request("POST", "/api/v1/visitor/leads", "", token, body)
	if status != 409 {
		t.Fatal("同一留言编号不能替换附件清单")
	}
	if _, err := f.admin.Exec(ctx, "DELETE FROM agent_group_members WHERE membership_id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	if code, _ := readLeadFixture(t, f, file, f.agent, ""); code != 404 {
		t.Fatal("离开原业务组后不能继续下载留言附件")
	}
	status, reply = f.request("POST", "/api/v1/tenant/contacts/"+lead["contact_id"].(string)+"/block", f.owner, "", map[string]any{"reason": "合成测试封禁"})
	mustData(t, status, reply)
	if code, _ := readLeadFixture(t, f, file, "", token); code != 403 && code != 401 {
		t.Fatal("封禁检查必须先于留言本人共享授权")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenants SET status='suspended' WHERE id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	if code, _ := readLeadFixture(t, f, file, f.owner, ""); code != 403 {
		t.Fatal("暂停租户不能凭旧权限读取留言附件")
	}
}

// configureNoticeFixture 注入仅记录参数的SMTP适配器并通过真实设置接口开启测试提醒。
func configureNoticeFixture(t *testing.T, f *fixture, sender func(context.Context, string, string, string) (string, string)) {
	t.Helper()
	f.s.cfg.SMTPHost = "smtp.fixture.invalid"
	f.s.cfg.SMTPPort = "587"
	f.s.cfg.SMTPFrom = "notice@goodchat.test"
	f.s.cfg.PublicURL = "http://127.0.0.1:13000"
	f.s.SetLeadNotifier(sender)
	status, reply := f.request("GET", "/api/v1/tenant/lead-settings", f.owner, "", nil)
	settings := mustData(t, status, reply)
	status, reply = f.request("PUT", "/api/v1/tenant/lead-settings", f.owner, "", map[string]any{"enabled": true, "email": "admin@goodchat.test", "version": settings["version"]})
	mustData(t, status, reply)
}

// TestLeadNotificationQueuePrivacyAndOnce 验证事务建队、同留言幂等和并发发送一次，邮件只含最小提醒。
func TestLeadNotificationQueuePrivacyAndOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var calls atomic.Int32
	var bodyValue string
	configureNoticeFixture(t, f, func(_ context.Context, recipient, subject, body string) (string, string) {
		calls.Add(1)
		bodyValue = body
		if recipient != "admin@goodchat.test" || !strings.Contains(subject, "待处理") {
			t.Error("通知目标或标题不正确")
		}
		return "accepted", ""
	})
	token, _, conversation := f.visitor("")
	body := leadFixtureBody(f, conversation)
	status, reply := f.request("POST", "/api/v1/visitor/leads", "", token, body)
	lead := mustData(t, status, reply)
	if calls.Load() != 0 || f.countRows("SELECT count(*) FROM lead_notification_jobs WHERE lead_id=$1 AND status='queued'", lead["id"]) != 1 {
		t.Fatal("提交留言只应事务入队，不能同步伪造邮件受理")
	}
	var workers sync.WaitGroup
	for index := 0; index < 6; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := f.s.runLeadNotifications(ctx, f.tenant); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if calls.Load() != 1 || f.countRows("SELECT count(*) FROM lead_notification_jobs WHERE lead_id=$1 AND status='accepted' AND attempts=1", lead["id"]) != 1 {
		t.Fatal("并发通知必须只执行一次且保存适配器真实受理分类")
	}
	if !strings.Contains(bodyValue, lead["id"].(string)) || !strings.Contains(bodyValue, "http://127.0.0.1:13000/login") || strings.Contains(bodyValue, "13800000001") || strings.Contains(bodyValue, "合成客户正文") {
		t.Fatal("通知只能含待处理、留言编号和配置官网登录入口")
	}
	status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, body)
	mustData(t, status, reply)
	if err := f.s.runLeadNotifications(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("幂等留言重放不能再发邮件")
	}
	status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation))
	next := mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/lead-settings", f.owner, "", nil)
	settings := mustData(t, status, reply)
	status, reply = f.request("PUT", "/api/v1/tenant/lead-settings", f.owner, "", map[string]any{"version": settings["version"], "enabled": true, "email": "new-admin@goodchat.test"})
	mustData(t, status, reply)
	if err := f.s.runLeadNotifications(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || f.countRows("SELECT count(*) FROM lead_notification_jobs WHERE lead_id=$1 AND status='failed' AND last_error='settings_changed'", next["id"]) != 1 {
		t.Fatal("配置换邮箱后旧任务不能投递到原收件地址")
	}
}

// TestLeadNotificationUnknownAndLeaseFence 验证DATA结果不确定和进程中断都不自动重发，迟到旧执行不能覆盖unknown。
func TestLeadNotificationUnknownAndLeaseFence(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	done := make(chan error, 1)
	var calls atomic.Int32
	configureNoticeFixture(t, f, func(context.Context, string, string, string) (string, string) {
		calls.Add(1)
		close(started)
		<-release
		return "accepted", ""
	})
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation))
	lead := mustData(t, status, reply)
	go func() { done <- f.s.runLeadNotifications(ctx, f.tenant) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("测试通知未被领取")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE lead_notification_jobs SET lease_until=now()-interval '1 second' WHERE lead_id=$1", lead["id"]); err != nil {
		t.Fatal(err)
	}
	if err := f.s.runLeadNotifications(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || f.countRows("SELECT count(*) FROM lead_notification_jobs WHERE lead_id=$1 AND status='unknown' AND last_error='worker_interrupted'", lead["id"]) != 1 {
		t.Fatal("租约过期后的迟到受理不能覆盖结果不确定或触发重发")
	}
	var job string
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM lead_notification_jobs WHERE lead_id=$1", lead["id"]).Scan(&job); err != nil {
		t.Fatal(err)
	}
	status, _ = f.request("POST", "/api/v1/tenant/lead-notifications/"+job+"/retry", f.owner, "", map[string]any{})
	if status != 409 {
		t.Fatal("unknown通知不得自动或无确认重发")
	}
}

// TestLeadNotificationFailureRetryAndQuota 验证明确失败重试冷却、输入注入拒绝及留言频率限制。
func TestLeadNotificationFailureRetryAndQuota(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var attempts atomic.Int32
	configureNoticeFixture(t, f, func(context.Context, string, string, string) (string, string) {
		if attempts.Add(1) == 1 {
			return "failed", "temporary"
		}
		return "unknown", "confirmation_lost"
	})
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation))
	lead := mustData(t, status, reply)
	if err := f.s.runLeadNotifications(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	var job string
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM lead_notification_jobs WHERE lead_id=$1 AND status='failed'", lead["id"]).Scan(&job); err != nil {
		t.Fatal(err)
	}
	status, _ = f.request("POST", "/api/v1/tenant/lead-notifications/"+job+"/retry", f.owner, "", map[string]any{})
	if status != 429 {
		t.Fatal("失败通知重试必须保留冷却")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE lead_notification_jobs SET finished_at=now()-interval '2 minutes' WHERE id=$1", job); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/tenant/lead-notifications/"+job+"/retry", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	if err := f.s.runLeadNotifications(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM lead_notification_jobs WHERE id=$1 AND status='unknown' AND jsonb_array_length(history)=2", job) != 1 {
		t.Fatal("逐次失败和不确定结果必须保留追加记录")
	}
	if err := f.s.runLeadNotifications(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatal("最终结果不确定不允许后台继续重发")
	}
	status, reply = f.request("GET", "/api/v1/tenant/lead-settings", f.owner, "", nil)
	settings := mustData(t, status, reply)
	status, _ = f.request("PUT", "/api/v1/tenant/lead-settings", f.owner, "", map[string]any{"version": settings["version"], "enabled": true, "email": "admin@goodchat.test\r\nBcc:evil@example.com"})
	if status != 400 {
		t.Fatal("收件地址不能注入邮件头")
	}
	for index := 0; index < 4; index++ {
		status, reply = f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation))
		mustData(t, status, reply)
	}
	status, _ = f.request("POST", "/api/v1/visitor/leads", "", token, leadFixtureBody(f, conversation))
	if status != 429 {
		t.Fatal("每访客短时大量新留言应被限流")
	}
}
