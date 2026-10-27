// 文件作用说明：在真实PostgreSQL隔离schema中验证并发席位预留、价格权益快照、人工幂等开通、到期回退及RLS。
package billing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestConcurrentSeatsAndReservationRelease 验证免费两席位、管理成员不占位、邀请预占与到期撤销释放，以及并发最后一席竞争。
func TestConcurrentSeatsAndReservationRelease(t *testing.T) {
	f := billingIntegration(t)
	ctx := context.Background()
	tenant := f.principal.TenantID
	var adminID string
	f.mustScan(t, "INSERT INTO users(email,display_name,password_hash) VALUES('manager@example.invalid','纯管理','test') RETURNING id::text", &adminID)
	if _, err := f.admin.Exec(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id,role,can_serve) VALUES($1,$2,'admin',false)", tenant, adminID); err != nil {
		t.Fatal(err)
	}
	for index, ending := range []string{"now()-interval '1 second'", "now()+interval '1 day'"} {
		if _, err := f.admin.Exec(ctx, fmt.Sprintf("INSERT INTO member_invitations(tenant_id,email,role,can_serve,capacity,token_hash,invited_by,expires_at,revoked_at) VALUES($1,$2,'agent',true,5,$3,$4,%s,%s)", ending, []string{"NULL", "now()"}[index]), tenant, fmt.Sprintf("ignored%d@example.invalid", index), []byte(fmt.Sprintf("ignored%d", index)), f.principal.UserID); err != nil {
			t.Fatal(err)
		}
	}
	const attempts = 24
	results := make(chan error, attempts)
	var group sync.WaitGroup
	for index := 0; index < attempts; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			results <- platform.TenantTx(ctx, f.pool, tenant, func(tx pgx.Tx) error {
				if err := f.s.CheckSeats(ctx, tx, tenant, "", "", 1); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, "INSERT INTO member_invitations(tenant_id,email,role,can_serve,capacity,token_hash,invited_by,expires_at) VALUES($1,$2,'agent',true,5,$3,$4,now()+interval '1 day')", tenant, fmt.Sprintf("race%d@example.invalid", index), []byte(fmt.Sprintf("race%d", index)), f.principal.UserID)
				return err
			})
		}(index)
	}
	group.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
			continue
		}
		var api *platform.APIError
		if !errors.As(err, &api) || api.Code != "seat_limit_reached" {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("最后一席应仅1个并发申请成功，得到%d", succeeded)
	}
	var inviteID string
	f.mustScan(t, "SELECT id::text FROM member_invitations WHERE tenant_id=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now()", &inviteID, tenant)
	err := platform.TenantTx(ctx, f.pool, tenant, func(tx pgx.Tx) error {
		if err := f.s.CheckSeats(ctx, tx, tenant, "", inviteID, 1); err != nil {
			return err
		}
		var userID string
		if err := tx.QueryRow(ctx, "INSERT INTO users(email,display_name,password_hash) VALUES('accepted@example.invalid','已激活','test') RETURNING id::text").Scan(&userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id,role,can_serve) VALUES($1,$2,'agent',true)", tenant, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE member_invitations SET accepted_at=now(),accepted_by=$1 WHERE id=$2", userID, inviteID); err != nil {
			return err
		}
		usage, err := seatUsage(ctx, tx, tenant, "", "", 2)
		if err == nil && (usage.SeatsUsed != 2 || usage.SeatsReserved != 0) {
			return fmt.Errorf("激活后占用错误: %+v", usage)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(ctx, "UPDATE tenant_memberships SET disabled=true WHERE tenant_id=$1 AND role='agent'", tenant); err != nil {
		t.Fatal(err)
	}
	if err = platform.TenantTx(ctx, f.pool, tenant, func(tx pgx.Tx) error { return f.s.CheckSeats(ctx, tx, tenant, "", "", 1) }); err != nil {
		t.Fatal("停用应释放席位", err)
	}
}

// TestSubscriptionSnapshotActivationAndExpiry 验证申请快照、真实收款确认、重复开通幂等、停用套餐不改历史及到期保护超额成员。
func TestSubscriptionSnapshotActivationAndExpiry(t *testing.T) {
	f := billingIntegration(t)
	ctx := context.Background()
	paid := f.paidPlan(t)
	request := decodeData[UpgradeRequest](t, f.call(t, "POST", "/api/v1/tenant/upgrade-requests", map[string]string{"plan_id": paid.ID}, f.principal), 201)
	if request.PlanSnapshot.PriceAmount != 12000 || request.PlanSnapshot.SeatLimit != 5 {
		t.Fatalf("申请未保存原价权益: %+v", request)
	}
	if w := f.call(t, "POST", "/api/v1/tenant/upgrade-requests", map[string]string{"plan_id": paid.ID}, f.principal); w.Code != 409 {
		t.Fatalf("重复待处理申请应冲突: %d %s", w.Code, w.Body.String())
	}
	decodeData[Plan](t, f.call(t, "PATCH", "/api/v1/platform/plans/"+paid.ID, map[string]any{"price_amount": 99900, "seat_limit": 7, "validity_days": 90, "features": map[string]bool{"chat": true, "export": true}}, f.principal), 200)
	activatePath := "/api/v1/platform/upgrade-requests/" + request.ID + "/activate"
	if w := f.call(t, "POST", activatePath, map[string]any{"payment_reference": "本地测试审批参考，非真实支付", "payment_confirmed": false}, f.principal); w.Code != 400 {
		t.Fatalf("未确认收款应拒绝: %d %s", w.Code, w.Body.String())
	}
	proof := map[string]any{"payment_reference": "本地测试审批参考，非真实支付", "payment_confirmed": true}
	activated := decodeData[UpgradeRequest](t, f.call(t, "POST", activatePath, proof, f.principal), 200)
	if activated.Status != "activated" || activated.ActivatedAt == nil {
		t.Fatalf("申请未真实开通: %+v", activated)
	}
	type subscriptionResult struct {
		Plan         Plan         `json:"plan"`
		Subscription Subscription `json:"subscription"`
		Usage        Usage        `json:"usage"`
	}
	sub := decodeData[subscriptionResult](t, f.call(t, "GET", "/api/v1/tenant/subscription", nil, f.principal), 200)
	if sub.Plan.PriceAmount != 12000 || sub.Plan.SeatLimit != 5 || sub.Plan.ValidityDays != 30 || sub.Plan.Features["export"] {
		t.Fatalf("后台改价不应改写开通快照: %+v", sub.Plan)
	}
	if sub.Subscription.ExpiresAt == nil || sub.Subscription.ExpiresAt.Sub(sub.Subscription.StartedAt) != 30*24*time.Hour {
		t.Fatalf("开通有效期未取申请快照: %+v", sub.Subscription)
	}
	decodeData[UpgradeRequest](t, f.call(t, "POST", activatePath, proof, f.principal), 200)
	repeat := decodeData[subscriptionResult](t, f.call(t, "GET", "/api/v1/tenant/subscription", nil, f.principal), 200)
	if !repeat.Subscription.StartedAt.Equal(sub.Subscription.StartedAt) || !repeat.Subscription.ExpiresAt.Equal(*sub.Subscription.ExpiresAt) {
		t.Fatal("重复开通重置了有效期")
	}
	var audits int
	f.mustScan(t, "SELECT count(*) FROM billing_audit_logs WHERE action='upgrade_activated' AND target_id=$1", &audits, request.ID)
	if audits != 1 {
		t.Fatalf("幂等开通应只有1条审计，得到%d", audits)
	}
	if err := platform.TenantTx(ctx, f.pool, f.principal.TenantID, func(tx pgx.Tx) error { return f.s.CheckFeature(ctx, tx, f.principal.TenantID, "export") }); err == nil {
		t.Fatal("未购买的导出权益应由服务端拒绝")
	}
	decodeData[Plan](t, f.call(t, "PATCH", "/api/v1/platform/plans/"+paid.ID, map[string]any{"is_active": false}, f.principal), 200)
	public := decodeData[struct {
		Items []Plan `json:"items"`
	}](t, f.call(t, "GET", "/api/v1/public/plans", nil, platform.Principal{}), 200)
	if len(public.Items) != 1 || public.Items[0].Code != "free" {
		t.Fatalf("未启用的版本不应公开: %+v", public.Items)
	}
	for index := 0; index < 2; index++ {
		var user string
		f.mustScan(t, "INSERT INTO users(email,display_name,password_hash) VALUES($1,'额外客服','test') RETURNING id::text", &user, fmt.Sprintf("over%d@example.invalid", index))
		if _, err := f.admin.Exec(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id,role,can_serve) VALUES($1,$2,'agent',true)", f.principal.TenantID, user); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1", f.principal.TenantID); err != nil {
		t.Fatal(err)
	}
	expired := decodeData[subscriptionResult](t, f.call(t, "GET", "/api/v1/tenant/subscription", nil, f.principal), 200)
	if expired.Plan.Code != "free" || expired.Subscription.Status != "expired" || expired.Usage.SeatsUsed != 3 || expired.Usage.SeatLimit != 2 {
		t.Fatalf("到期应回免费并保留超额成员: %+v", expired)
	}
	err := platform.TenantTx(ctx, f.pool, f.principal.TenantID, func(tx pgx.Tx) error { return f.s.CheckSeats(ctx, tx, f.principal.TenantID, "", "", 1) })
	var api *platform.APIError
	if !errors.As(err, &api) || api.Code != "seat_limit_reached" {
		t.Fatalf("到期后新增席位应被拒绝: %v", err)
	}
	if err := platform.TenantTx(ctx, f.pool, f.principal.TenantID, func(tx pgx.Tx) error { return f.s.CheckFeature(ctx, tx, f.principal.TenantID, "chat") }); err != nil {
		t.Fatal("到期不应阻断现有聊天", err)
	}
}

// TestTenantScopeAndPlatformAuthority 验证RLS拒绝缺租户上下文、跨租户访问和伪造平台标记。
func TestTenantScopeAndPlatformAuthority(t *testing.T) {
	f := billingIntegration(t)
	ctx := context.Background()
	var count int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM tenant_subscriptions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("缺租户上下文不应读取订阅: %d %v", count, err)
	}
	var other string
	f.mustScan(t, "INSERT INTO tenants(name,slug) VALUES('其他租户','other') RETURNING id::text", &other)
	if err := platform.TenantTx(ctx, f.pool, other, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM tenant_subscriptions WHERE tenant_id=$1", f.principal.TenantID).Scan(&count)
	}); err != nil || count != 0 {
		t.Fatalf("跨租户不应读取订阅: %d %v", count, err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE users SET is_platform_admin=false WHERE id=$1", f.principal.UserID); err != nil {
		t.Fatal(err)
	}
	if w := f.call(t, "GET", "/api/v1/platform/tenants", nil, f.principal); w.Code != 403 {
		t.Fatalf("伪造或过期平台标记应实时拒绝: %d %s", w.Code, w.Body.String())
	}
}

// TestPlatformDowngradeAndTenantState 验证人工降级保留超额成员且写入审计，停用后不能继续提交开通申请。
func TestPlatformDowngradeAndTenantState(t *testing.T) {
	f := billingIntegration(t)
	paid := f.paidPlan(t)
	path := "/api/v1/platform/tenants/" + f.principal.TenantID
	if w := f.call(t, "PATCH", path, map[string]any{"plan_id": paid.ID, "payment_reference": "测试审批参考", "payment_confirmed": false}, f.principal); w.Code != 400 {
		t.Fatalf("分配付费版须确认实际收款: %d %s", w.Code, w.Body.String())
	}
	decodeData[map[string]bool](t, f.call(t, "PATCH", path, map[string]any{"plan_id": paid.ID, "payment_reference": "测试审批参考", "payment_confirmed": true}, f.principal), 200)
	ctx := context.Background()
	for index := 0; index < 2; index++ {
		var user string
		f.mustScan(t, "INSERT INTO users(email,display_name,password_hash) VALUES($1,'降级保留客服','test') RETURNING id::text", &user, fmt.Sprintf("keep%d@example.invalid", index))
		if _, err := f.admin.Exec(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id,role,can_serve) VALUES($1,$2,'agent',true)", f.principal.TenantID, user); err != nil {
			t.Fatal(err)
		}
	}
	public := decodeData[struct {
		Items []Plan `json:"items"`
	}](t, f.call(t, "GET", "/api/v1/public/plans", nil, platform.Principal{}), 200)
	var free Plan
	for _, plan := range public.Items {
		if plan.Code == "free" {
			free = plan
		}
	}
	decodeData[map[string]bool](t, f.call(t, "PATCH", path, map[string]any{"plan_id": free.ID, "payment_reference": "测试运营降级审批"}, f.principal), 200)
	view := decodeData[struct {
		Plan  Plan  `json:"plan"`
		Usage Usage `json:"usage"`
	}](t, f.call(t, "GET", "/api/v1/tenant/subscription", nil, f.principal), 200)
	if view.Plan.Code != "free" || view.Usage.SeatsUsed != 3 || view.Usage.SeatLimit != 2 {
		t.Fatalf("降级不可删除超额成员: %+v", view)
	}
	err := platform.TenantTx(ctx, f.pool, f.principal.TenantID, func(tx pgx.Tx) error { return f.s.CheckSeats(ctx, tx, f.principal.TenantID, "", "", 1) })
	if err == nil {
		t.Fatal("降级后应拒绝新增席位")
	}
	var audits int
	f.mustScan(t, "SELECT count(*) FROM billing_audit_logs WHERE action='tenant_plan_assigned'", &audits)
	if audits != 2 {
		t.Fatalf("人工分配与降级应留下两条审计，得到%d", audits)
	}
	decodeData[map[string]bool](t, f.call(t, "PATCH", path, map[string]any{"status": "suspended"}, f.principal), 200)
	if w := f.call(t, "POST", "/api/v1/tenant/upgrade-requests", map[string]string{"plan_id": paid.ID}, f.principal); w.Code != 403 {
		t.Fatalf("停用租户不能新建申请: %d %s", w.Code, w.Body.String())
	}
}

// TestExpiryUsesStatementTime 验证事务开始后等待跨过到期时刻，权益按实际校验时间回退，不能沿用事务开始时的旧权益。
func TestExpiryUsesStatementTime(t *testing.T) {
	f := billingIntegration(t)
	paid := f.paidPlan(t)
	decodeData[map[string]bool](t, f.call(t, "PATCH", "/api/v1/platform/tenants/"+f.principal.TenantID, map[string]any{"plan_id": paid.ID, "payment_reference": "测试审批参考", "payment_confirmed": true}, f.principal), 200)
	ctx := context.Background()
	err := platform.TenantTx(ctx, f.pool, f.principal.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE tenant_subscriptions SET expires_at=clock_timestamp()+interval '50 milliseconds' WHERE tenant_id=$1", f.principal.TenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT pg_sleep(0.1)"); err != nil {
			return err
		}
		plan, sub, err := effectivePlan(ctx, tx, f.principal.TenantID)
		if err == nil && (plan.Code != "free" || sub.Status != "expired") {
			return fmt.Errorf("跨到期时刻后仍沿用旧权益: %s %s", plan.Code, sub.Status)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
