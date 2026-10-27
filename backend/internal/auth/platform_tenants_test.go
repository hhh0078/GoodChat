// 文件作用说明：通过真实PostgreSQL隔离schema和本地SMTP验证平台代开租户、幂等与首个所有者激活。
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

// platformCall 为平台创建请求添加显式幂等标识，其余鉴权仍走真实Cookie。
func (f *identityFixture) platformCall(path, key string, input any, session *http.Cookie) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(input)
	request := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	request.RemoteAddr = "127.0.0.1:34567"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", f.service.cfg.PublicURL)
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	if session != nil {
		request.AddCookie(session)
	}
	response := httptest.NewRecorder()
	f.mux.ServeHTTP(response, request)
	return response
}

// platformData 解码平台开通结果，测试不记录原始邀请凭证。
func platformData(t *testing.T, response *httptest.ResponseRecorder) platformTenantView {
	t.Helper()
	var envelope struct {
		Data platformTenantView `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		t.Fatal("平台租户响应格式无效")
	}
	return envelope.Data
}

// TestPlatformTenantIdempotentCreationAndActivation 验证平台权限、并发同键防重、默认初始化和邮箱验证后的唯一所有者。
func TestPlatformTenantIdempotentCreationAndActivation(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	platformUser, session := f.register(t, "platform-create@example.com")
	input := map[string]any{"name": "平台代开测试", "admin_email": "first-admin@example.com", "admin_name": "首位管理员", "timezone": "Asia/Shanghai", "can_serve": false}
	requireStatus(t, f.platformCall("/api/v1/platform/tenants", "create-tenant-1", input, session), 403)
	if _, err := f.pool.Exec(ctx, "UPDATE users SET is_platform_admin=true WHERE id=$1", platformUser.User.ID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.platformCall("/api/v1/platform/tenants", "", input, session), 400)
	responses := make(chan *httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			responses <- f.platformCall("/api/v1/platform/tenants", "create-tenant-1", input, session)
		}()
	}
	group.Wait()
	close(responses)
	var tenantID string
	created, replayed := 0, 0
	for response := range responses {
		if response.Code == 201 {
			created++
		} else if response.Code == 200 {
			replayed++
		} else {
			t.Fatalf("平台并发创建意外状态：%d %s", response.Code, response.Body.String())
		}
		data := platformData(t, response)
		id := data.Tenant["id"].(string)
		if tenantID != "" && id != tenantID {
			t.Fatal("同键创建了不同租户")
		}
		tenantID = id
	}
	if created != 1 || replayed != 1 {
		t.Fatal("同键请求应创建一次并重放一次")
	}
	token := f.invitationToken(t)
	var tenants, initializations int
	if err := f.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM tenants),(SELECT count(*) FROM identity_audit_logs WHERE action='tenant_defaults_initialized')").Scan(&tenants, &initializations); err != nil || tenants != 2 || initializations != 1 {
		t.Fatal("平台创建应原子产生一次独立租户及默认资源")
	}
	input["name"] = "不同内容不能复用键"
	requireStatus(t, f.platformCall("/api/v1/platform/tenants", "create-tenant-1", input, session), 409)
	code := f.code(t, "first-admin@example.com", "invitation")
	accepted := f.call("POST", "/api/v1/auth/accept-invitation", map[string]string{"token": token, "code": code, "password": "GoodChat-first-admin-password"}, nil, "")
	requireStatus(t, accepted, 200)
	view := accountData(t, accepted)
	if view.ActiveTenantID != tenantID || len(view.Memberships) != 1 || view.Memberships[0].Role != "owner" {
		t.Fatal("指定的初始管理员必须成为该独立租户唯一所有者")
	}
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM tenants").Scan(&tenants); err != nil || tenants != 2 {
		t.Fatal("接受代开邀请不应额外创建租户")
	}
	requireStatus(t, f.platformCall("/api/v1/platform/tenants/"+tenantID+"/resend-invitation", "", map[string]any{}, session), 409)
}

// TestPlatformTenantMailFailureAndResend 验证邮件失败仍保存资源，幂等重放不重发，专门重发轮换旧凭证。
func TestPlatformTenantMailFailureAndResend(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	user, session := f.register(t, "platform-resend@example.com")
	if _, err := f.pool.Exec(ctx, "UPDATE users SET is_platform_admin=true WHERE id=$1", user.User.ID); err != nil {
		t.Fatal(err)
	}
	reject := startSMTP(t, "reject")
	f.service.mailer = &smtpMailer{cfg: reject.config()}
	input := map[string]any{"name": "发信失败保留租户", "admin_email": "retry-admin@example.com", "admin_name": "等待重发"}
	failed := f.platformCall("/api/v1/platform/tenants", "smtp-failure-1", input, session)
	requireStatus(t, failed, 502)
	_ = reject.nextBody(t)
	replay := f.platformCall("/api/v1/platform/tenants", "smtp-failure-1", input, session)
	requireStatus(t, replay, 200)
	view := platformData(t, replay)
	tenantID := view.Tenant["id"].(string)
	if view.DeliveryStatus != "failed" || !view.Replayed {
		t.Fatal("重放应明确显示先前的真实投递失败")
	}
	var count int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM platform_tenant_creations").Scan(&count); err != nil || count != 1 {
		t.Fatal("SMTP失败不得导致重复租户映射")
	}
	f.service.mailer = &smtpMailer{cfg: f.smtp.config()}
	requireStatus(t, f.platformCall("/api/v1/platform/tenants/"+tenantID+"/resend-invitation", "", map[string]any{}, session), 429)
	if _, err := f.pool.Exec(ctx, "UPDATE member_invitations SET created_at=now()-interval '61 seconds' WHERE id=$1", view.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	resent := f.platformCall("/api/v1/platform/tenants/"+tenantID+"/resend-invitation", "", map[string]any{}, session)
	requireStatus(t, resent, 200)
	newView := platformData(t, resent)
	if newView.Invitation.ID == view.Invitation.ID {
		t.Fatal("重发应轮换初始邀请")
	}
	token := f.invitationToken(t)
	var revoked bool
	if err := f.pool.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM member_invitations WHERE id=$1", view.Invitation.ID).Scan(&revoked); err != nil || !revoked {
		t.Fatal("旧邀请必须立即失效")
	}
	code := f.code(t, "retry-admin@example.com", "invitation")
	accepted := f.call("POST", "/api/v1/auth/accept-invitation", map[string]string{"token": token, "code": code, "password": "GoodChat-retry-admin-password"}, nil, "")
	requireStatus(t, accepted, 200)
	if accountData(t, accepted).Memberships[0].Role != "owner" {
		t.Fatal("重发后当前初始邀请仍应激活唯一所有者")
	}
}

// TestOrdinaryPlatformInvitationCannotTakeOwnership 防止普通管理员邀请借平台身份和无所有者状态自动夺取所有权。
func TestOrdinaryPlatformInvitationCannotTakeOwnership(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	user, session := f.register(t, "platform-ordinary@example.com")
	if _, err := f.pool.Exec(ctx, "UPDATE users SET is_platform_admin=true WHERE id=$1", user.User.ID); err != nil {
		t.Fatal(err)
	}
	created := f.call("POST", "/api/v1/tenant/invitations", map[string]any{"email": "ordinary-admin@example.com", "role": "admin", "can_serve": false}, session, user.ActiveTenantID)
	requireStatus(t, created, 201)
	token := f.invitationToken(t)
	if _, err := f.pool.Exec(ctx, "UPDATE tenant_memberships SET role='agent' WHERE id=$1", user.Memberships[0].ID); err != nil {
		t.Fatal(err)
	}
	code := f.code(t, "ordinary-admin@example.com", "invitation")
	accepted := f.call("POST", "/api/v1/auth/accept-invitation", map[string]string{"token": token, "code": code, "password": "GoodChat-ordinary-admin-password"}, nil, "")
	requireStatus(t, accepted, 403)
	if _, err := f.pool.Exec(ctx, "UPDATE tenant_memberships SET role='admin' WHERE id=$1", user.Memberships[0].ID); err != nil {
		t.Fatal(err)
	}
	accepted = f.call("POST", "/api/v1/auth/accept-invitation", map[string]string{"token": token, "code": code, "password": "GoodChat-ordinary-admin-password"}, nil, "")
	requireStatus(t, accepted, 200)
	if accountData(t, accepted).Memberships[0].Role != "admin" {
		t.Fatal("非指定初始邀请绝不能自动晋升owner")
	}
}

// TestPlatformTenantInitializerRollback 验证初始化失败时租户、邀请和幂等映射全部回滚。
func TestPlatformTenantInitializerRollback(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	user, session := f.register(t, "platform-rollback@example.com")
	if _, err := f.pool.Exec(ctx, "UPDATE users SET is_platform_admin=true WHERE id=$1", user.User.ID); err != nil {
		t.Fatal(err)
	}
	f.service.SetTenantInitializer(func(ctx context.Context, tx pgx.Tx, tenantID, userID string) error { return context.Canceled })
	requireStatus(t, f.platformCall("/api/v1/platform/tenants", "rollback-1", map[string]string{"name": "应回滚租户", "admin_email": "rollback-admin@example.com"}, session), 500)
	var tenants, invitations, mappings int
	if err := f.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM tenants),(SELECT count(*) FROM member_invitations),(SELECT count(*) FROM platform_tenant_creations)").Scan(&tenants, &invitations, &mappings); err != nil || tenants != 1 || invitations != 0 || mappings != 0 {
		t.Fatal("初始化失败必须整笔回滚")
	}
}

// TestInitialInvitationRejectsRevokedPlatformActor 验证初始邀请在平台授权撤销后不能激活所有者，恢复授权后仍需合法邮箱验证。
func TestInitialInvitationRejectsRevokedPlatformActor(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	user, session := f.register(t, "platform-revoked@example.com")
	if _, err := f.pool.Exec(ctx, "UPDATE users SET is_platform_admin=true WHERE id=$1", user.User.ID); err != nil {
		t.Fatal(err)
	}
	created := f.platformCall("/api/v1/platform/tenants", "revoke-actor-1", map[string]string{"name": "待验证租户", "admin_email": "revoked-initial-admin@example.com"}, session)
	requireStatus(t, created, 201)
	token := f.invitationToken(t)
	code := f.code(t, "revoked-initial-admin@example.com", "invitation")
	if _, err := f.pool.Exec(ctx, "UPDATE users SET is_platform_admin=false WHERE id=$1", user.User.ID); err != nil {
		t.Fatal(err)
	}
	input := map[string]string{"token": token, "code": code, "password": "GoodChat-revoked-platform-password"}
	requireStatus(t, f.call("POST", "/api/v1/auth/accept-invitation", input, nil, ""), 403)
	var members int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1", platformData(t, created).Tenant["id"]).Scan(&members); err != nil || members != 0 {
		t.Fatal("平台权限撤销后不得留下初始所有者")
	}
	if _, err := f.pool.Exec(ctx, "UPDATE users SET is_platform_admin=true WHERE id=$1", user.User.ID); err != nil {
		t.Fatal(err)
	}
	accepted := f.call("POST", "/api/v1/auth/accept-invitation", input, nil, "")
	requireStatus(t, accepted, 200)
	if accountData(t, accepted).Memberships[0].Role != "owner" {
		t.Fatal("只有当前初始邀请和有效平台授权可激活所有者")
	}
}
