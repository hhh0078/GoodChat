// 文件作用说明：在现有独立PostgreSQL中创建隔离测试schema，真实验证验证码事务、自动建租户、邀请与成员权限；所有业务邮件只发本地SMTP。
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// identityFixture 保存隔离数据库、真实路由和本地测试收件器。
type identityFixture struct {
	service *Service
	mux     *http.ServeMux
	smtp    *smtpFixture
	pool    *pgxpool.Pool
}

// identityIntegration 为显式开启的集成测试准备隔离schema，不启动数据库且不复用其他项目数据。
func identityIntegration(t *testing.T) *identityFixture {
	t.Helper()
	if os.Getenv("SUPPORT_AUTH_INTEGRATION") != "1" {
		t.Skip("设置SUPPORT_AUTH_INTEGRATION=1后使用已配置的独立PostgreSQL执行")
	}
	// go test的工作目录是包目录，独立读取数据库配置，不触碰真实SMTP配置。
	cfg := platform.Config{DatabaseURL: os.Getenv("SUPPORT_TEST_DATABASE_URL"), MigrationURL: os.Getenv("SUPPORT_TEST_MIGRATION_URL")}
	if cfg.DatabaseURL == "" && cfg.MigrationURL == "" {
		body, err := os.ReadFile(filepath.Join("..", "..", "..", ".local", "dev-database.json"))
		if err != nil {
			t.Fatal("独立测试数据库配置不可用")
		}
		var local struct {
			DatabaseURL  string `json:"database_url"`
			MigrationURL string `json:"migration_url"`
		}
		if json.Unmarshal(body, &local) != nil {
			t.Fatal("测试数据库配置格式错误")
		}
		cfg.DatabaseURL, cfg.MigrationURL = local.DatabaseURL, local.MigrationURL
	}
	if cfg.DatabaseURL == "" || cfg.MigrationURL == "" {
		t.Fatal("集成测试需要运行与迁移数据库连接")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, cfg.MigrationURL)
	if err != nil {
		t.Fatal("无法连接独立测试数据库")
	}
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	schema := "auth_test_" + strings.ReplaceAll(id, "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close(ctx)
		t.Fatal("无法建立隔离测试schema")
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close(context.Background())
	})
	if _, err := admin.Exec(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(filepath.Join("..", "database", "migrations", "001_identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, string(file)); err != nil {
		t.Fatal("隔离身份迁移失败", err)
	}
	file, err = os.ReadFile(filepath.Join("..", "database", "migrations", "007_platform_onboarding.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, string(file)); err != nil {
		t.Fatal("隔离平台开通迁移失败", err)
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		t.Fatal("运行数据库配置解析失败")
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("创建测试连接池失败")
	}
	t.Cleanup(pool.Close)
	var runtimeRole string
	if err := pool.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatal(err)
	}
	role := pgx.Identifier{runtimeRole}.Sanitize()
	for _, statement := range []string{"GRANT USAGE ON SCHEMA " + quoted + " TO " + role, "GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO " + role, "GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + quoted + " TO " + role} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	server := startSMTP(t, "accepted")
	authConfig := server.config()
	authConfig.DataDir = t.TempDir()
	service, err := New(pool, authConfig)
	if err != nil {
		t.Fatal(err)
	}
	service.SetTenantInitializer(func(ctx context.Context, tx pgx.Tx, tenantID, userID string) error {
		var current string
		if err := tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id',true)").Scan(&current); err != nil {
			return err
		}
		if current != tenantID {
			return errors.New("初始化事务缺少正确租户上下文")
		}
		if userID == "" {
			_, err := tx.Exec(ctx, "INSERT INTO identity_audit_logs(tenant_id,action,target_id) VALUES($1,'tenant_defaults_initialized',$1)", tenantID)
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO identity_audit_logs(tenant_id,actor_id,action,target_id) VALUES($1,$2,'defaults_initialized',$2)", tenantID, userID)
		return err
	})
	mux := http.NewServeMux()
	service.Register(mux)
	return &identityFixture{service: service, mux: mux, smtp: server, pool: pool}
}

// call 执行真实HTTP路由与中间件，保留Cookie及租户选择契约。
func (f *identityFixture) call(method, path string, input any, cookie *http.Cookie, tenant string) *httptest.ResponseRecorder {
	var body strings.Reader
	if input != nil {
		encoded, _ := json.Marshal(input)
		body = *strings.NewReader(string(encoded))
	}
	request := httptest.NewRequest(method, path, &body)
	request.RemoteAddr = "127.0.0.1:34567"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", f.service.cfg.PublicURL)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if tenant != "" {
		request.Header.Set("X-Tenant-ID", tenant)
	}
	response := httptest.NewRecorder()
	f.mux.ServeHTTP(response, request)
	return response
}

// requireStatus 在错误时仅输出安全API响应，不输出邮件内容或数据库凭证。
func requireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("HTTP状态应为%d，实际%d，响应%s", status, response.Code, response.Body.String())
	}
}

// code 通过本地SMTP发送并捕获测试验证码，验证API不会返回明文。
func (f *identityFixture) code(t *testing.T, email, purpose string) string {
	t.Helper()
	response := f.call("POST", "/api/v1/auth/send-code", map[string]string{"email": email, "purpose": purpose}, nil, "")
	requireStatus(t, response, 200)
	body := f.smtp.nextBody(t)
	code := regexp.MustCompile(`\b[0-9]{6}\b`).FindString(body)
	if code == "" {
		t.Fatal("本地邮件缺少六位验证码")
	}
	if strings.Contains(response.Body.String(), code) {
		t.Fatal("API泄露验证码")
	}
	return code
}

// cookie 从成功登录响应提取安全会话Cookie。
func cookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, item := range response.Result().Cookies() {
		if item.Name == sessionCookie {
			if !item.HttpOnly || item.Domain != "" || item.SameSite != http.SameSiteLaxMode {
				t.Fatal("登录Cookie属性不安全")
			}
			return item
		}
	}
	t.Fatal("登录成功未设置Cookie")
	return nil
}

// accountData 解析认证接口公共data结构。
func accountData(t *testing.T, response *httptest.ResponseRecorder) accountView {
	t.Helper()
	var payload struct {
		Data accountView `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data
}

// register 为集成场景创建真实邮箱验证码验证的测试租户所有者。
func (f *identityFixture) register(t *testing.T, email string) (accountView, *http.Cookie) {
	t.Helper()
	code := f.code(t, email, "register")
	response := f.call("POST", "/api/v1/auth/register", map[string]string{"email": email, "code": code, "password": "GoodChat-test-password", "tenant_name": "测试租户", "display_name": "测试管理员"}, nil, "")
	requireStatus(t, response, 200)
	return accountData(t, response), cookie(t, response)
}

// invitationToken 从仅发送到本地测试SMTP的邀请邮件中提取加入凭证。
func (f *identityFixture) invitationToken(t *testing.T) string {
	t.Helper()
	body := f.smtp.nextBody(t)
	link := regexp.MustCompile(`https?://[^\s]+`).FindString(body)
	parsed, err := url.Parse(link)
	if err != nil || parsed.Query().Get("token") == "" {
		t.Fatal("本地邀请邮件缺少有效加入链接")
	}
	return parsed.Query().Get("token")
}

// TestOTPTransactions 验证用途、重发、过期、错误次数、回滚和并发一次消费。
func TestOTPTransactions(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	email := "otp@example.com"
	code := f.code(t, email, "register")
	noop := func(tx pgx.Tx) error { return nil }
	if err := f.service.withCode(ctx, email, "login", code, noop); err == nil {
		t.Fatal("验证码跨用途被接受")
	}
	wrong := "000000"
	if code == wrong {
		wrong = "999999"
	}
	for attempt := 0; attempt < 5; attempt++ {
		if err := f.service.withCode(ctx, email, "register", wrong, noop); err == nil {
			t.Fatal("错误验证码被接受")
		}
	}
	var attempts int
	if err := f.pool.QueryRow(ctx, "SELECT failed_attempts FROM email_challenges WHERE email=$1", email).Scan(&attempts); err != nil || attempts != 5 {
		t.Fatal("错误次数未独立持久提交")
	}
	if err := f.service.withCode(ctx, email, "register", code, noop); err == nil {
		t.Fatal("超次验证码仍可消费")
	}
	retry := f.call("POST", "/api/v1/auth/send-code", map[string]string{"email": email, "purpose": "register"}, nil, "")
	requireStatus(t, retry, 429)
	if _, err := f.pool.Exec(ctx, "UPDATE email_challenges SET created_at=now()-interval '61 seconds' WHERE email=$1", email); err != nil {
		t.Fatal(err)
	}
	newCode := f.code(t, email, "register")
	if code != newCode && f.service.withCode(ctx, email, "register", code, noop) == nil {
		t.Fatal("重发后旧码有效")
	}
	forced := errors.New("强制业务回滚")
	if err := f.service.withCode(ctx, email, "register", newCode, func(tx pgx.Tx) error { return forced }); !errors.Is(err, forced) {
		t.Fatal("业务错误未返回")
	}
	var successes atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if f.service.withCode(ctx, email, "register", newCode, noop) == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatal("验证码并发消费不是一次")
	}
	if f.service.withCode(ctx, email, "register", newCode, noop) == nil {
		t.Fatal("已消费验证码被重放")
	}
	expiredEmail := "expired@example.com"
	expiredCode := f.code(t, expiredEmail, "login")
	if _, err := f.pool.Exec(ctx, "UPDATE email_challenges SET expires_at=now()-interval '1 second' WHERE email=$1", expiredEmail); err != nil {
		t.Fatal(err)
	}
	if f.service.withCode(ctx, expiredEmail, "login", expiredCode, noop) == nil {
		t.Fatal("过期验证码被接受")
	}
	for index := 0; index < 2; index++ {
		if err := f.service.limit(ctx, "persistent_test", "subject", time.Hour, 2); err != nil {
			t.Fatal(err)
		}
	}
	other := &Service{pool: f.pool, secret: f.service.secret}
	if other.limit(ctx, "persistent_test", "subject", time.Hour, 2) == nil {
		t.Fatal("重建服务绕过持久限流")
	}
}

// TestRegistrationAtomicity 验证重复并发注册、默认资源上下文以及失败后的完整事务回滚。
func TestRegistrationAtomicity(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	email := "owner@example.com"
	code := f.code(t, email, "register")
	input := map[string]string{"email": email, "code": code, "password": "GoodChat-test-password", "tenant_name": "并发租户", "display_name": "所有者"}
	results := make(chan *httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for index := 0; index < 2; index++ {
		group.Add(1)
		go func() { defer group.Done(); results <- f.call("POST", "/api/v1/auth/register", input, nil, "") }()
	}
	group.Wait()
	close(results)
	var winner *httptest.ResponseRecorder
	successes := 0
	for response := range results {
		if response.Code == 200 {
			winner = response
			successes++
		} else if response.Code != 400 && response.Code != 409 {
			t.Fatal("并发注册返回异常")
		}
	}
	if successes != 1 {
		t.Fatal("并发注册没有恰好成功一次")
	}
	view := accountData(t, winner)
	if len(view.Memberships) != 1 || view.Memberships[0].Role != "owner" || !view.Memberships[0].CanServe || view.User.PlatformAdmin {
		t.Fatal("注册角色或接待资格错误")
	}
	var users, tenants, defaults int
	if err := f.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM tenants),(SELECT count(*) FROM identity_audit_logs WHERE action='defaults_initialized')").Scan(&users, &tenants, &defaults); err != nil || users != 1 || tenants != 1 || defaults != 1 {
		t.Fatal("注册产生孤立或重复资源")
	}
	ownerCookie := cookie(t, winner)
	requireStatus(t, f.call("GET", "/api/v1/auth/me", nil, ownerCookie, ""), 200)
	foreign, _ := newID()
	requireStatus(t, f.call("GET", "/api/v1/auth/me", nil, ownerCookie, foreign), 403)
	bad := f.call("POST", "/api/v1/auth/register", map[string]any{"email": "bad@example.com", "role": "platform_admin"}, nil, "")
	requireStatus(t, bad, 400)
	failingEmail := "rollback@example.com"
	failingCode := f.code(t, failingEmail, "register")
	original := f.service.initializeTenant
	f.service.SetTenantInitializer(func(context.Context, pgx.Tx, string, string) error { return errors.New("强制初始化失败") })
	failedInput := map[string]string{"email": failingEmail, "code": failingCode, "password": "GoodChat-test-password", "tenant_name": "回滚租户", "display_name": "回滚账号"}
	requireStatus(t, f.call("POST", "/api/v1/auth/register", failedInput, nil, ""), 500)
	var partial int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE email=$1", failingEmail).Scan(&partial); err != nil || partial != 0 {
		t.Fatal("初始化失败后账号未回滚")
	}
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM tenants").Scan(&partial); err != nil || partial != 1 {
		t.Fatal("初始化失败后租户未回滚")
	}
	f.service.SetTenantInitializer(original)
	requireStatus(t, f.call("POST", "/api/v1/auth/register", failedInput, nil, ""), 200)
}

// TestInvitationsAndTenantPermissions 验证受邀新旧账号加入原租户、角色不可篡改和停用撤权。
func TestInvitationsAndTenantPermissions(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	owner, ownerCookie := f.register(t, "inviter@example.com")
	tenantID := owner.ActiveTenantID
	invitedEmail := "agent@example.com"
	created := f.call("POST", "/api/v1/tenant/invitations", map[string]any{"email": invitedEmail, "role": "agent", "display_name": "客服甲", "can_serve": true, "capacity": 3}, ownerCookie, tenantID)
	requireStatus(t, created, 201)
	token := f.invitationToken(t)
	if strings.Contains(created.Body.String(), token) {
		t.Fatal("邀请API泄露原始令牌")
	}
	code := f.code(t, invitedEmail, "invitation")
	input := map[string]string{"token": token, "code": code, "password": "GoodChat-agent-password", "display_name": "客服甲"}
	requireStatus(t, f.call("POST", "/api/v1/auth/accept-invitation", input, ownerCookie, tenantID), 403)
	accepted := f.call("POST", "/api/v1/auth/accept-invitation", input, nil, "")
	requireStatus(t, accepted, 200)
	agent := accountData(t, accepted)
	agentCookie := cookie(t, accepted)
	if agent.ActiveTenantID != tenantID || agent.Memberships[0].Role != "agent" {
		t.Fatal("受邀加入了错误租户或角色")
	}
	var count int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM tenants").Scan(&count); err != nil || count != 1 {
		t.Fatal("受邀注册额外创建租户")
	}
	requireStatus(t, f.call("POST", "/api/v1/auth/accept-invitation", input, nil, ""), 400)
	requireStatus(t, f.call("POST", "/api/v1/auth/accept-invitation", input, agentCookie, tenantID), 200)
	requireStatus(t, f.call("GET", "/api/v1/tenant/members", nil, agentCookie, tenantID), 403)
	requireStatus(t, f.call("POST", "/api/v1/tenant/invitations", map[string]string{"email": "x@example.com", "role": "admin"}, agentCookie, tenantID), 403)
	requireStatus(t, f.call("POST", "/api/v1/tenant/invitations", map[string]string{"email": "x@example.com", "role": "platform_admin"}, ownerCookie, tenantID), 400)
	memberID := agent.Memberships[0].ID
	requireStatus(t, f.call("PATCH", "/api/v1/tenant/members/"+memberID, map[string]any{"display_name": "租户内昵称"}, ownerCookie, tenantID), 200)
	var globalName string
	if err := f.pool.QueryRow(ctx, "SELECT display_name FROM users WHERE id=$1", agent.User.ID).Scan(&globalName); err != nil || globalName != "客服甲" {
		t.Fatal("租户成员编辑污染全局名称")
	}
	requireStatus(t, f.call("PATCH", "/api/v1/tenant/members/"+memberID, map[string]any{"disabled": true}, ownerCookie, tenantID), 200)
	requireStatus(t, f.call("GET", "/api/v1/auth/me", nil, agentCookie, ""), 401)
	ownerID := owner.Memberships[0].ID
	requireStatus(t, f.call("DELETE", "/api/v1/tenant/members/"+ownerID, nil, ownerCookie, tenantID), 409)
	requireStatus(t, f.call("PATCH", "/api/v1/tenant/members/"+ownerID, map[string]string{"role": "agent"}, ownerCookie, tenantID), 409)
	if _, err := f.pool.Exec(ctx, "UPDATE tenant_memberships SET role='admin' WHERE id=$1", ownerID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.call("PATCH", "/api/v1/tenant/members/"+ownerID, map[string]bool{"disabled": true}, ownerCookie, tenantID), 409)
	if _, err := f.pool.Exec(ctx, "UPDATE tenant_memberships SET role='owner' WHERE id=$1", ownerID); err != nil {
		t.Fatal(err)
	}
	existing, existingCookie := f.register(t, "existing@example.com")
	var oldHash string
	if err := f.pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id=$1", existing.User.ID).Scan(&oldHash); err != nil {
		t.Fatal(err)
	}
	created = f.call("POST", "/api/v1/tenant/invitations", map[string]any{"email": "existing@example.com", "role": "admin", "can_serve": false}, ownerCookie, tenantID)
	requireStatus(t, created, 201)
	existingToken := f.invitationToken(t)
	response := f.call("POST", "/api/v1/auth/accept-invitation", map[string]string{"token": existingToken, "password": "not-to-overwrite"}, existingCookie, "")
	requireStatus(t, response, 200)
	joined := accountData(t, response)
	if len(joined.Memberships) != 2 || joined.ActiveTenantID != tenantID {
		t.Fatal("已有账号多租户关系丢失")
	}
	var newHash string
	if err := f.pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id=$1", existing.User.ID).Scan(&newHash); err != nil || newHash != oldHash {
		t.Fatal("接受邀请覆盖了已有密码")
	}
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM tenants").Scan(&count); err != nil || count != 2 {
		t.Fatal("已有账号接受邀请创建了额外租户")
	}
}

// TestPasswordResetAndMailFailures 验证登录错误、重置撤销、未知用途与未配置邮件服务。
func TestPasswordResetAndMailFailures(t *testing.T) {
	f := identityIntegration(t)
	view, session := f.register(t, "reset@example.com")
	requireStatus(t, f.call("POST", "/api/v1/auth/login", map[string]string{"email": view.User.Email, "password": "wrong-password"}, nil, ""), 401)
	login := f.call("POST", "/api/v1/auth/login", map[string]string{"email": view.User.Email, "password": "GoodChat-test-password"}, nil, "")
	requireStatus(t, login, 200)
	second := cookie(t, login)
	requireStatus(t, f.call("POST", "/api/v1/auth/logout-others", nil, session, ""), 200)
	requireStatus(t, f.call("GET", "/api/v1/auth/me", nil, second, ""), 401)
	code := f.code(t, view.User.Email, "reset_password")
	requireStatus(t, f.call("POST", "/api/v1/auth/reset-password", map[string]string{"email": view.User.Email, "code": code, "password": "Changed-password-123"}, nil, ""), 200)
	requireStatus(t, f.call("GET", "/api/v1/auth/me", nil, session, ""), 401)
	requireStatus(t, f.call("POST", "/api/v1/auth/login", map[string]string{"email": view.User.Email, "password": "GoodChat-test-password"}, nil, ""), 401)
	requireStatus(t, f.call("POST", "/api/v1/auth/login", map[string]string{"email": view.User.Email, "password": "Changed-password-123"}, nil, ""), 200)
	requireStatus(t, f.call("POST", "/api/v1/auth/send-code", map[string]string{"email": view.User.Email, "purpose": "universal"}, nil, ""), 400)
	f.service.mailer.cfg = platform.Config{}
	requireStatus(t, f.call("POST", "/api/v1/auth/send-code", map[string]string{"email": "none@example.com", "purpose": "register"}, nil, ""), 503)
}
