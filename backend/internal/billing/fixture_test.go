// 文件作用说明：使用现有独立PostgreSQL的随机schema与最小权限应用角色执行套餐集成测试，不创建数据库或容器、不读取邮件配置。
package billing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// billingFixture 保存真实数据库连接、模块路由与隔离测试账号。
type billingFixture struct {
	s         *Service
	pool      *pgxpool.Pool
	admin     *pgx.Conn
	mux       *http.ServeMux
	principal platform.Principal
}

// testAuthorizer 仅为模块测试注入身份；平台事务仍检查真实users平台标记。
type testAuthorizer struct{}

// RequireUser 拒绝测试请求缺少账号的情况。
func (testAuthorizer) RequireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if platform.Current(r).UserID == "" {
			platform.ErrorJSON(w, r, platform.Fail(401, "unauthenticated", "请登录"))
			return
		}
		next(w, r)
	}
}

// RequireTenant 拒绝测试请求缺少当前租户的情况。
func (a testAuthorizer) RequireTenant(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireUser(func(w http.ResponseWriter, r *http.Request) {
		if platform.Current(r).TenantID == "" {
			platform.ErrorJSON(w, r, platform.Fail(403, "tenant_required", "请选择租户"))
			return
		}
		next(w, r)
	})
}

// RequireAdmin 在测试路由上执行租户管理员角色校验。
func (a testAuthorizer) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireTenant(func(w http.ResponseWriter, r *http.Request) {
		p := platform.Current(r)
		if p.Role != "owner" && p.Role != "admin" {
			platform.ErrorJSON(w, r, platform.Fail(403, "admin_required", "需要管理员"))
			return
		}
		next(w, r)
	})
}

// RequirePlatform 在测试路由上拒绝非平台管理员，事务再查询数据库标记。
func (a testAuthorizer) RequirePlatform(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireUser(func(w http.ResponseWriter, r *http.Request) {
		if !platform.Current(r).PlatformAdmin {
			platform.ErrorJSON(w, r, platform.Fail(403, "platform_required", "需要平台管理员"))
			return
		}
		next(w, r)
	})
}

// billingIntegration 初始化随机schema并授权既有最小权限运行角色，所有测试数据随schema清理。
func billingIntegration(t *testing.T) *billingFixture {
	t.Helper()
	if os.Getenv("SUPPORT_BILLING_INTEGRATION") != "1" {
		t.Skip("设置SUPPORT_BILLING_INTEGRATION=1后使用现有独立PostgreSQL执行")
	}
	var local struct {
		DatabaseURL  string `json:"database_url"`
		MigrationURL string `json:"migration_url"`
	}
	local.DatabaseURL, local.MigrationURL = os.Getenv("SUPPORT_TEST_DATABASE_URL"), os.Getenv("SUPPORT_TEST_MIGRATION_URL")
	if local.DatabaseURL == "" && local.MigrationURL == "" {
		body, err := os.ReadFile(filepath.Join("..", "..", "..", ".local", "dev-database.json"))
		if err != nil || json.Unmarshal(body, &local) != nil {
			t.Fatal("项目独立数据库配置不可用")
		}
	}
	if local.DatabaseURL == "" || local.MigrationURL == "" {
		t.Fatal("测试需要运行与迁移数据库连接")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, local.MigrationURL)
	if err != nil {
		t.Fatal("测试数据库连接失败")
	}
	bytes := make([]byte, 12)
	if _, err = rand.Read(bytes); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	schema := "billing_test_" + hex.EncodeToString(bytes)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		_ = admin.Close(context.Background())
	})
	if _, err = admin.Exec(ctx, "SET search_path TO "+quoted+",public"); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"000_base.sql", "001_identity.sql", "005_billing.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "database", "migrations", file))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, string(body)); err != nil {
			t.Fatalf("隔离迁移%s失败: %v", file, err)
		}
	}
	config, err := pgxpool.ParseConfig(local.DatabaseURL)
	if err != nil {
		t.Fatal("测试连接配置无效")
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	config.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("测试运行池连接失败")
	}
	t.Cleanup(pool.Close)
	var role string
	var elevated bool
	if err = pool.QueryRow(ctx, "SELECT current_user,rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user").Scan(&role, &elevated); err != nil {
		t.Fatal(err)
	}
	if elevated {
		t.Fatal("测试必须使用非超级用户且不能绕过RLS的运行角色")
	}
	role = pgx.Identifier{role}.Sanitize()
	for _, sql := range []string{"GRANT USAGE ON SCHEMA " + quoted + " TO " + role, "GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO " + role, "GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + quoted + " TO " + role} {
		if _, err = admin.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	f := &billingFixture{pool: pool, admin: admin}
	f.s = New(pool, testAuthorizer{}, platform.Config{})
	f.mustScan(t, "INSERT INTO users(email,display_name,password_hash,is_platform_admin) VALUES('billing@example.invalid','套餐测试管理员','test-only',true) RETURNING id::text", &f.principal.UserID)
	f.mustScan(t, "INSERT INTO tenants(name,slug) VALUES('套餐隔离测试','billing-test') RETURNING id::text", &f.principal.TenantID)
	f.mustScan(t, "INSERT INTO tenant_memberships(tenant_id,user_id,role,can_serve) VALUES($1,$2,'owner',true) RETURNING id::text", &f.principal.MembershipID, f.principal.TenantID, f.principal.UserID)
	f.principal.Role = "owner"
	f.principal.CanServe = true
	f.principal.PlatformAdmin = true
	if err = platform.TenantTx(ctx, pool, f.principal.TenantID, func(tx pgx.Tx) error { return f.s.EnsureTenant(ctx, tx, f.principal.TenantID) }); err != nil {
		t.Fatal(err)
	}
	f.mux = http.NewServeMux()
	f.s.Register(f.mux)
	return f
}

// mustScan 通过测试管理连接建立明确的随机schema数据，失败立即终止测试。
func (f *billingFixture) mustScan(t *testing.T, sql string, destination any, args ...any) {
	t.Helper()
	if err := f.admin.QueryRow(context.Background(), sql, args...).Scan(destination); err != nil {
		t.Fatal(err)
	}
}

// call 通过真实HTTP路由调用模块，身份中间件测试边界与数据库RLS分别验证。
func (f *billingFixture) call(t *testing.T, method, path string, input any, p platform.Principal) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	r = r.WithContext(platform.WithPrincipal(r.Context(), p))
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

// decodeData 读取统一成功信封，保留中文错误响应以便测试定位。
func decodeData[T any](t *testing.T, w *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if w.Code != status {
		t.Fatalf("期望HTTP%d，得到%d: %s", status, w.Code, w.Body.String())
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

// paidPlan 通过平台真实创建路由添加仅用于隔离schema测试的明确付费版本。
func (f *billingFixture) paidPlan(t *testing.T) Plan {
	t.Helper()
	return decodeData[Plan](t, f.call(t, "POST", "/api/v1/platform/plans", map[string]any{"code": "team", "name": "测试收费版", "price_amount": 12000, "currency": "CNY", "validity_days": 30, "seat_limit": 5, "features": map[string]bool{"chat": true}, "is_public": true, "is_active": true}, f.principal), 201)
}
