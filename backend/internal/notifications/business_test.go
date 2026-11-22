// 文件作用说明：验证已提交账本扫描、余额恢复取消、免费版不提醒、订阅时区调度及模板版本权限。
package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestWalletScanAndRecovery 验证只扫描新真实账本事件、重复扫描不重复通知、充值恢复取消旧预警。
func TestWalletScanAndRecovery(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	var member string
	if e := f.admin.QueryRow(ctx, "SELECT id::text FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2", f.tenant, f.user).Scan(&member); e != nil {
		t.Fatal(e)
	}
	if _, e := f.admin.Exec(ctx, "INSERT INTO ai_wallets(tenant_id,balance_micro) VALUES($1,10000000)", f.tenant); e != nil {
		t.Fatal(e)
	}
	f.admin.Exec(ctx, "INSERT INTO ai_wallet_entries(tenant_id,delta_micro,kind,reason,client_key) VALUES($1,10000000,'topup','test','initial')", f.tenant)
	if e := f.s.scanTenant(ctx, f.tenant); e != nil {
		t.Fatal(e)
	}
	var job string
	if e := f.admin.QueryRow(ctx, "INSERT INTO ai_jobs(tenant_id,kind,owner_membership_id,status,snapshot) VALUES($1,'test',$2,'succeeded','{}') RETURNING id::text", f.tenant, member).Scan(&job); e != nil {
		t.Fatal(e)
	}
	f.admin.Exec(ctx, "UPDATE ai_wallets SET balance_micro=4000000 WHERE tenant_id=$1", f.tenant)
	if _, e := f.admin.Exec(ctx, "INSERT INTO ai_wallet_entries(tenant_id,job_id,delta_micro,kind,reason,client_key) VALUES($1,$2,-6000000,'charge','test','charge')", f.tenant, job); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e := f.s.scanTenant(ctx, f.tenant); e != nil {
			t.Fatal(e)
		}
	}
	var count int
	if e := f.admin.QueryRow(ctx, "SELECT count(*) FROM notification_jobs WHERE kind='balance_low'").Scan(&count); e != nil || count != 1 {
		t.Fatal("余额预警未去重", e, count)
	}
	f.admin.Exec(ctx, "UPDATE ai_wallets SET balance_micro=10000000 WHERE tenant_id=$1", f.tenant)
	f.admin.Exec(ctx, "UPDATE notification_jobs SET status='cancelled' WHERE kind<>'balance_low'")
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	var status string
	f.admin.QueryRow(ctx, "SELECT status FROM notification_jobs WHERE kind='balance_low'").Scan(&status)
	if status != "cancelled" {
		t.Fatal("恢复余额后未取消旧提醒", status)
	}
}

// TestSubscriptionLocalSchedule 验证免费版不创建到期提醒，付费版按收件人本地日期安排并在续费后撤销。
func TestSubscriptionLocalSchedule(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	f.admin.Exec(ctx, "INSERT INTO tenant_subscriptions(tenant_id,plan_id,plan_snapshot) SELECT $1,id,to_jsonb(plans) FROM plans WHERE code='free'", f.tenant)
	if e := f.s.scanTenant(ctx, f.tenant); e != nil {
		t.Fatal(e)
	}
	var n int
	f.admin.QueryRow(ctx, "SELECT count(*) FROM notification_jobs WHERE kind LIKE 'subscription_%'").Scan(&n)
	if n != 0 {
		t.Fatal("免费版产生到期提醒")
	}
	f.admin.Exec(ctx, "UPDATE users SET display_timezone='America/New_York' WHERE id=$1", f.user)
	expires := time.Now().Add(10 * 24 * time.Hour).UTC().Truncate(time.Second)
	f.admin.Exec(ctx, "UPDATE tenant_subscriptions SET expires_at=$2,plan_snapshot=jsonb_set(plan_snapshot,'{code}','\"paid_notice\"') WHERE tenant_id=$1", f.tenant, expires)
	if e := f.s.scanTenant(ctx, f.tenant); e != nil {
		t.Fatal(e)
	}
	rows, e := f.admin.Query(ctx, "SELECT kind,due_at FROM notification_jobs WHERE kind='subscription_expiring' ORDER BY due_at")
	if e != nil {
		t.Fatal(e)
	}
	l, _ := time.LoadLocation("America/New_York")
	for rows.Next() {
		var kind string
		var due time.Time
		if e := rows.Scan(&kind, &due); e != nil {
			t.Fatal(e)
		}
		if due.In(l).Hour() != 9 {
			t.Fatal("未按本地9点调度")
		}
		n++
	}
	rows.Close()
	if n != 3 {
		t.Fatal("缺少7/3/1天提醒", n)
	}
	f.admin.Exec(ctx, "UPDATE tenant_subscriptions SET expires_at=$2 WHERE tenant_id=$1", f.tenant, expires.AddDate(0, 1, 0))
	f.admin.Exec(ctx, "UPDATE notification_jobs SET status='cancelled' WHERE kind<>'subscription_expiring'")
	f.admin.Exec(ctx, "UPDATE notification_jobs SET due_at=now() WHERE kind='subscription_expiring'")
	for i := 0; i < 3; i++ {
		if e := f.s.tick(ctx); e != nil {
			t.Fatal(e)
		}
	}
	f.admin.QueryRow(ctx, "SELECT count(*) FROM notification_jobs WHERE kind='subscription_expiring' AND status='cancelled'").Scan(&n)
	if n != 3 {
		t.Fatal("续费后旧提醒未撤销", n)
	}
}

// TestVersionsAndHistory 验证同模板并发旧版本拒绝、回滚创建新版本，以及本人历史范围。
func TestVersionsAndHistory(t *testing.T) {
	f := integration(t)
	principal := platform.Principal{UserID: f.user, PlatformAdmin: true}
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r = r.WithContext(platform.WithPrincipal(r.Context(), principal))
		w := httptest.NewRecorder()
		if e := f.s.publish(w, r); e != nil {
			platform.ErrorJSON(w, r, e)
		}
		return w
	}
	body := `{"kind":"test","locale":"en","subject":"Test {name}","body":"Hello {reference}","button":"Open","base_version":0}`
	if w := call(body); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(body); w.Code != 409 {
		t.Fatal("旧版本未拒绝", w.Code)
	}
	if w := call(`{"kind":"test","locale":"en","base_version":1,"restore_version":1}`); w.Code != 201 {
		t.Fatal("回滚未生成新版本", w.Code)
	}
	f.enqueue(t, "personal")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: f.other, TenantID: f.tenant}))
	w := httptest.NewRecorder()
	if e := f.s.history(w, r); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(w.Body.String(), "password_changed") {
		t.Fatal("泄露他人记录")
	}
	// 验证通知与业务共同回滚，而非提交后临时内存任务。
	tx, e := f.pool.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e := Enqueue(context.Background(), tx, Event{User: f.user, Kind: "welcome", Key: "rolled-back"}); e != nil {
		t.Fatal(e)
	}
	tx.Rollback(context.Background())
	var n int
	f.admin.QueryRow(context.Background(), "SELECT count(*) FROM notification_jobs WHERE event_key='rolled-back'").Scan(&n)
	if n != 0 {
		t.Fatal("事务回滚未撤销通知")
	}
}

// TestCrossTenantRecipient 验证企业通知收件人不能指定其他账号或其他企业管理员。
func TestCrossTenantRecipient(t *testing.T) {
	f := integration(t)
	r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"recipient_ids":["`+f.other+`"],"low_micro":5000000,"critical_micro":1000000,"handoff_minutes":5,"service_enabled":true}`))
	r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: f.user, TenantID: f.tenant, Role: "owner"}))
	w := httptest.NewRecorder()
	if e := f.s.settings(w, r); e != nil {
		platform.ErrorJSON(w, r, e)
	}
	if w.Code != 400 {
		t.Fatal("非成员收件人未拒绝", w.Code, w.Body.String())
	}
}

// TestGracefulMailShutdown 验证关闭连接池之前等待SMTP取消结果持久化，避免把已知状态遗留为执行中。
func TestGracefulMailShutdown(t *testing.T) {
	f := integration(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	f.s.send = func(ctx context.Context, _ string, _ Mail) (string, string) {
		close(started)
		<-ctx.Done()
		return "unknown", "confirmation_lost"
	}
	f.enqueue(t, "shutdown")
	f.s.Start(ctx)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		f.s.Wait()
		t.Fatal("执行器未开始任务")
	}
	cancel()
	f.s.Wait()
	if f.status(t, "shutdown") != "unknown" {
		t.Fatal("关闭前未保存结果")
	}
}

// TestPreviewTimezoneAndImmutableVersion 验证预览按钮携带所选时区，已发布版本不能被数据库更新覆盖。
func TestPreviewTimezoneAndImmutableVersion(t *testing.T) {
	f := integration(t)
	d, _ := Find("subscription_expiring")
	body, _ := json.Marshal(map[string]string{"kind": d.Kind, "locale": "zh-CN", "timezone": "Asia/Tokyo", "subject": d.Subject, "body": d.Body, "button": d.Button})
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	if e := f.s.preview(w, r); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(w.Body.String(), "timezone=Asia%2FTokyo") || !strings.Contains(w.Body.String(), "Asia/Tokyo") {
		t.Fatal("预览未应用时区")
	}
	ctx := context.Background()
	if _, e := f.admin.Exec(ctx, "INSERT INTO notification_templates(kind,locale,version,subject,body,button) VALUES('test','en',1,'Test','Test','Open')"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.admin.Exec(ctx, "UPDATE notification_templates SET body='changed' WHERE kind='test'"); e == nil {
		t.Fatal("允许覆盖已发布模板")
	}
}

// TestQueuedFinancePermissionRevoked 验证排队后失去管理员或收件人权限，不能收到租户账务数据。
func TestQueuedFinancePermissionRevoked(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	f.admin.Exec(ctx, "INSERT INTO notification_settings(tenant_id) VALUES($1)", f.tenant)
	if e := f.s.systemTx(ctx, func(tx pgx.Tx) error {
		return Enqueue(ctx, tx, Event{Tenant: f.tenant, User: f.user, Kind: "topup", Key: "finance-revoked", Params: map[string]string{"amount": "CNY 1.00", "tenant": "Tenant", "reference": "TEST"}})
	}); e != nil {
		t.Fatal(e)
	}
	f.admin.Exec(ctx, "UPDATE tenant_memberships SET role='agent' WHERE tenant_id=$1 AND user_id=$2", f.tenant, f.user)
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.status(t, "finance-revoked") != "cancelled" {
		t.Fatal("已降权账号仍收到财务通知")
	}
}

// TestDigestRespectsMembershipScope 验证普通客服摘要不泄露其他成员AI用量，管理员摘要可汇总租户数据。
func TestDigestRespectsMembershipScope(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	var owner, agent string
	f.admin.QueryRow(ctx, "SELECT id::text FROM tenant_memberships WHERE tenant_id=$1", f.tenant).Scan(&owner)
	if e := f.admin.QueryRow(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'agent') RETURNING id::text", f.tenant, f.other).Scan(&agent); e != nil {
		t.Fatal(e)
	}
	for _, item := range []struct {
		member string
		tokens int
	}{{owner, 100}, {agent, 15}} {
		if _, e := f.admin.Exec(ctx, "INSERT INTO ai_jobs(tenant_id,kind,owner_membership_id,status,snapshot,input_tokens) VALUES($1,'test',$2,'succeeded','{}',$3)", f.tenant, item.member, item.tokens); e != nil {
			t.Fatal(e)
		}
	}
	if e := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, tokens, e := digestUsage(ctx, tx, agent, false)
		if e != nil {
			return e
		}
		if tokens != 15 {
			t.Fatal("普通客服看到其他成员用量", tokens)
		}
		_, all, e := digestUsage(ctx, tx, owner, true)
		if e != nil {
			return e
		}
		if all != 115 {
			t.Fatal("管理员汇总不正确", all)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

// TestFinancialOutboxLateCommit 验证流水回滚不产生邮件，较早开始但较晚提交的事务也不会被游标漏掉。
func TestFinancialOutboxLateCommit(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	f.admin.Exec(ctx, "INSERT INTO ai_wallets(tenant_id,balance_micro) VALUES($1,10000000)", f.tenant)
	tx, e := f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.tenant)
	if _, e := tx.Exec(ctx, "INSERT INTO ai_wallet_entries(tenant_id,delta_micro,kind,reason,client_key,created_at) VALUES($1,1000000,'topup','late','late',now()-interval '1 day')", f.tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := f.admin.Exec(ctx, "INSERT INTO ai_wallet_entries(tenant_id,delta_micro,kind,reason,client_key) VALUES($1,1000000,'topup','early','early')", f.tenant); e != nil {
		t.Fatal(e)
	}
	if e := f.s.scanTenant(ctx, f.tenant); e != nil {
		t.Fatal(e)
	}
	if e := tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e := f.s.scanTenant(ctx, f.tenant); e != nil {
		t.Fatal(e)
	}
	var n int
	f.admin.QueryRow(ctx, "SELECT count(*) FROM notification_jobs WHERE kind='topup'").Scan(&n)
	if n != 2 {
		t.Fatal("遗漏迟提交交易", n)
	}
	tx, e = f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.tenant)
	tx.Exec(ctx, "INSERT INTO ai_wallet_entries(tenant_id,delta_micro,kind,reason,client_key) VALUES($1,1000000,'topup','rollback','rollback')", f.tenant)
	tx.Rollback(ctx)
	f.admin.QueryRow(ctx, "SELECT count(*) FROM notification_sources").Scan(&n)
	if n != 2 {
		t.Fatal("回滚后仍存在邮件事件", n)
	}
}

// TestSameRecipientDifferentTenantEvents 验证同一所有者在两个租户的同日预算事件不互相去重。
func TestSameRecipientDifferentTenantEvents(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	var other string
	if e := f.admin.QueryRow(ctx, "INSERT INTO tenants(name,slug) VALUES('Other','notice-other') RETURNING id::text").Scan(&other); e != nil {
		t.Fatal(e)
	}
	for _, tenant := range []string{f.tenant, other} {
		if e := f.s.systemTx(ctx, func(tx pgx.Tx) error {
			return Enqueue(ctx, tx, Event{Tenant: tenant, User: f.user, Kind: "ai_blocked", Key: "same-day"})
		}); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	if e := f.admin.QueryRow(ctx, "SELECT count(*) FROM notification_jobs WHERE kind='ai_blocked'").Scan(&n); e != nil || n != 2 {
		t.Fatal("跨租户事件被错误去重", n, e)
	}
}

// TestLocalizedAnnouncementOptIn 验证公告仅发送给订阅者，并按收件语言选择正文及生成安全退订入口。
func TestLocalizedAnnouncementOptIn(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	f.s.cfg.AuthSecret = strings.Repeat("a", 32)
	f.admin.Exec(ctx, "UPDATE users SET preferred_locale='de' WHERE id=$1", f.user)
	f.admin.Exec(ctx, "INSERT INTO mail_notification_preferences(user_id,updates_enabled,quiet_start,quiet_end) VALUES($1,true,0,0)", f.user)
	texts := map[string]string{}
	for _, locale := range []string{"zh-CN", "en", "zh-TW", "ja", "ko", "es", "pt-BR", "fr", "de"} {
		texts[locale] = locale + " Product QA"
	}
	call := func() *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"key": "qa-release-2026", "translations": texts})
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(raw)))
		r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: f.user, PlatformAdmin: true}))
		w := httptest.NewRecorder()
		if e := f.s.announcement(w, r); e != nil {
			platform.ErrorJSON(w, r, e)
		}
		return w
	}
	if w := call(); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(); w.Code != 202 {
		t.Fatal("重复公告失败", w.Code)
	}
	var n int
	f.admin.QueryRow(ctx, "SELECT count(*) FROM notification_jobs WHERE kind='product_update'").Scan(&n)
	if n != 1 {
		t.Fatal("发送给未订阅用户或重复发送", n)
	}
	texts["de"] = "changed"
	if w := call(); w.Code != 409 {
		t.Fatal("公告译文被同一编号覆盖", w.Code)
	}
	sent := ""
	f.s.send = func(_ context.Context, _ string, mail Mail) (string, string) {
		sent = mail.Body
		if mail.UnsubscribeURL == "" {
			t.Fatal("公告缺少退订入口")
		}
		return "accepted", ""
	}
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(sent, "de Product QA") {
		t.Fatal("未按收件语言选择公告正文")
	}
}

// TestKnowledgeGapNotificationDedup 验证真实待办扫描携带原问题、重复扫描去重，补充完成后不再发送旧提醒。
func TestKnowledgeGapNotificationDedup(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	var member, job, id string
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2", f.tenant, f.user).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(ctx, "INSERT INTO ai_jobs(tenant_id,kind,owner_membership_id,status,snapshot) VALUES($1,'test',$2,'succeeded','{}') RETURNING id::text", f.tenant, member).Scan(&job); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(ctx, "INSERT INTO ai_pending_items(tenant_id,question,normalized_question,reason,source_job_id,last_job_id) VALUES($1,'作者的公开微信号是多少？','作者的公开微信号是多少','知识没有公开说明',$2,$2) RETURNING id::text", f.tenant, job).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.s.scanTenant(ctx, f.tenant); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var question string
	if err := f.admin.QueryRow(ctx, "SELECT count(*),max(params->>'question') FROM notification_jobs WHERE kind='knowledge_gap'").Scan(&count, &question); err != nil || count != 1 || question != "作者的公开微信号是多少？" {
		t.Fatal("待补充通知没有正确去重或缺少真实问题", err, count)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE ai_pending_items SET status='resolved' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if err := f.s.systemTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.tenant); err != nil {
			return err
		}
		valid, err := f.s.stillRelevant(ctx, tx, f.tenant, "knowledge_gap", map[string]string{"reference": id})
		if valid {
			t.Error("已经处理的知识提醒仍有效")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
