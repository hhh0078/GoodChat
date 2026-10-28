// 文件作用说明：为真实PostgreSQL聊天集成测试创建隔离随机租户和受控认证替身。
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
	"supportdesk/backend/internal/reporting"
)

// fixture 保存仅属于本次测试的账号、租户、连接与HTTP服务。
type fixture struct {
	t                                                            *testing.T
	pool, admin                                                  *pgxpool.Pool
	s                                                            *Service
	server                                                       *httptest.Server
	tenant, otherTenant, owner, agent, otherOwner, entry, poolID string
	schema                                                       string
	principals                                                   map[string]platform.Principal
}

// testAuthorizer 只替代邮箱登录层，聊天授权仍使用真实数据库成员与RLS。
type testAuthorizer struct{ principals map[string]platform.Principal }

// testQueryTracer 只记录测试SQL错误类型，不输出请求参数、访客令牌或数据库连接。
type testQueryTracer struct{ t *testing.T }
type traceSQLKey struct{}

// TraceQueryStart 保留测试请求上下文，不保存数据库参数。
func (trace testQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, traceSQLKey{}, data.SQL)
}

// TraceQueryEnd 输出驱动返回的安全错误，便于定位被统一API信封隐藏的数据库缺陷。
func (trace testQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		trace.t.Logf("测试SQL错误: %v SQL=%s", data.Err, ctx.Value(traceSQLKey{}))
	}
}

// RequireUser 注入测试已验证身份。
func (a testAuthorizer) RequireUser(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireTenant(next)
}

// RequireTenant 根据受控测试成员表选择身份，不接受请求提供的租户编号。
func (a testAuthorizer) RequireTenant(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.principals[r.Header.Get("X-Test-Member")]
		if !ok {
			platform.ErrorJSON(w, r, platform.Fail(401, "unauthorized", "需要登录"))
			return
		}
		next(w, r.WithContext(platform.WithPrincipal(r.Context(), p)))
	}
}

// RequireAdmin 为管理资源验证测试账号的真实预设角色。
func (a testAuthorizer) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireTenant(func(w http.ResponseWriter, r *http.Request) {
		p := platform.Current(r)
		if p.Role != "owner" && p.Role != "admin" {
			platform.ErrorJSON(w, r, platform.Fail(403, "forbidden", "需要管理员权限"))
			return
		}
		next(w, r)
	})
}

// RequirePlatform 拒绝测试聊天模块无须使用的平台写权限。
func (a testAuthorizer) RequirePlatform(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		platform.ErrorJSON(w, r, platform.Fail(403, "forbidden", "不提供平台权限"))
	}
}

// newFixture 仅连接显式指定的独立goodchat_saas数据库，不创建容器或修改其他库。
func newFixture(t *testing.T) *fixture {
	t.Helper()
	path := os.Getenv("GOODCHAT_TEST_CONFIG")
	if path == "" {
		if os.Getenv("SUPPORT_CHAT_INTEGRATION") != "1" {
			t.Skip("设置SUPPORT_CHAT_INTEGRATION=1后使用现有独立PostgreSQL执行")
		}
		path = filepath.Join("..", "..", "..", ".local", "dev-database.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("无法读取独立测试库配置")
	}
	var cfg struct {
		Database  string `json:"database_url"`
		Migration string `json:"migration_url"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("测试库配置格式无效")
	}
	for _, dsn := range []string{cfg.Database, cfg.Migration} {
		parsed, err := pgxpool.ParseConfig(dsn)
		if err != nil || parsed.ConnConfig.Database != "goodchat_saas" {
			t.Fatal("安全检查失败：只允许独立goodchat_saas测试库")
		}
	}
	ctx := context.Background()
	bootstrap, err := pgx.Connect(ctx, cfg.Migration)
	if err != nil {
		t.Fatal("无法建立迁移测试连接")
	}
	schema := "chat_test_" + strings.ReplaceAll(newID(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = bootstrap.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		bootstrap.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = bootstrap.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		_ = bootstrap.Close(context.Background())
	})
	if _, err = bootstrap.Exec(ctx, "SET search_path TO "+quoted+",public"); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"000_base.sql", "001_identity.sql", "002_chat.sql", "003_callbacks.sql", "004_operations.sql", "009_advanced_routing.sql", "011_automation_rich.sql", "012_async_exports.sql", "014_lead_attachments_notifications.sql", "015_export_redaction.sql", "016_link_reception.sql", "017_customer_workspace.sql"} {
		body, readErr := os.ReadFile(filepath.Join("..", "database", "migrations", file))
		if readErr != nil {
			t.Fatal(readErr)
		}
		// 迁移文件保持原样，仅将测试执行副本中的固定public对象定位到随机schema。
		sql := strings.ReplaceAll(string(body), "public.", schema+".")
		sql = strings.ReplaceAll(sql, "pg_catalog,public", "pg_catalog,"+schema)
		if _, err = bootstrap.Exec(ctx, sql); err != nil {
			t.Fatalf("聊天隔离迁移%s失败: %v", file, err)
		}
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.Database)
	if err != nil {
		t.Fatal("运行角色配置解析失败")
	}
	poolCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	poolCfg.ConnConfig.RuntimeParams["application_name"] = schema
	poolCfg.ConnConfig.Tracer = testQueryTracer{t: t}
	poolCfg.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatal("无法建立应用测试连接")
	}
	adminCfg, err := pgxpool.ParseConfig(cfg.Migration)
	if err != nil {
		t.Fatal("迁移角色配置解析失败")
	}
	adminCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	admin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		pool.Close()
		t.Fatal("无法建立测试管理池")
	}
	f := &fixture{t: t, pool: pool, admin: admin, tenant: newID(), otherTenant: newID(), owner: newID(), agent: newID(), otherOwner: newID(), principals: map[string]platform.Principal{}, schema: schema}
	t.Cleanup(func() {
		if f.server != nil {
			f.server.Close()
		}
		pool.Close()
		admin.Close()
	})
	var runtimeRole string
	if err = pool.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatal(err)
	}
	role := pgx.Identifier{runtimeRole}.Sanitize()
	for _, sql := range []string{"GRANT USAGE ON SCHEMA " + quoted + " TO " + role, "GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + quoted + " TO " + role, "GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + quoted + " TO " + role} {
		if _, err = bootstrap.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	var super, bypass, owner bool
	if err := pool.QueryRow(ctx, "SELECT r.rolsuper,r.rolbypassrls,c.relowner=r.oid FROM pg_roles r JOIN pg_class c ON c.oid='contacts'::regclass WHERE r.rolname=current_user").Scan(&super, &bypass, &owner); err != nil {
		t.Fatalf("无法验证应用数据库角色：%v", err)
	}
	if super || bypass || owner {
		t.Fatal("测试必须使用非超级用户、非BYPASSRLS、非表所有者")
	}
	users := []string{newID(), newID(), newID()}
	tenants := []string{f.tenant, f.tenant, f.otherTenant}
	members := []string{f.owner, f.agent, f.otherOwner}
	roles := []string{"owner", "agent", "owner"}
	for i, user := range users {
		if _, err := admin.Exec(ctx, "INSERT INTO users(id,email,display_name,password_hash) VALUES($1,$2,$3,'test-only-unusable-password')", user, "chat-test-"+user+"@example.invalid", "测试成员"); err != nil {
			t.Fatal(err)
		}
		if i != 1 {
			if _, err := admin.Exec(ctx, "INSERT INTO tenants(id,name,slug) VALUES($1,'聊天隔离测试',$2)", tenants[i], "chat-test-"+tenants[i]); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := admin.Exec(ctx, "INSERT INTO tenant_memberships(id,tenant_id,user_id,role,can_serve,capacity) VALUES($1,$2,$3,$4,true,1)", members[i], tenants[i], user, roles[i]); err != nil {
			t.Fatal(err)
		}
		f.principals[members[i]] = platform.Principal{TenantID: tenants[i], UserID: user, MembershipID: members[i], Role: roles[i], CanServe: true}
		if err := platform.TenantTx(ctx, pool, tenants[i], func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SELECT ensure_chat_defaults($1::uuid,$2::uuid)", tenants[i], user)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := platform.TenantTx(ctx, pool, f.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT entry_key,routing_pool_id::text FROM access_links WHERE is_default").Scan(&f.entry, &f.poolID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE routing_pools SET algorithm='manual' WHERE is_default")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.s = New(pool, testAuthorizer{f.principals}, platform.Config{DataDir: t.TempDir()})
	mux := http.NewServeMux()
	f.s.Register(mux)
	reporting.New(pool, testAuthorizer{f.principals}).Register(mux)
	f.server = httptest.NewServer(mux)
	return f
}

// request 执行真实HTTP请求，避免在日志中打印访客令牌或数据库连接。
func (f *fixture) request(method, path, member, token string, body any, extra ...map[string]string) (int, map[string]any) {
	f.t.Helper()
	var data []byte
	if body != nil {
		data = jsonValue(body)
	}
	req, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(data))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if member != "" {
		req.Header.Set("X-Test-Member", member)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, headers := range extra {
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return resp.StatusCode, result
}

// visitor 创建明确的独立匿名客户，重复手机号不会恢复其他客户历史。
func (f *fixture) visitor(phone string) (string, string, string) {
	f.t.Helper()
	status, result := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visit_id": newID(), "profile": map[string]any{"display_name": "集成测试访客", "phone": phone}, "context": map[string]any{"零": 0, "布尔": false}})
	if status != 200 {
		f.t.Fatalf("访客初始化失败：状态%d %v", status, result)
	}
	data := result["data"].(map[string]any)
	return data["token"].(string), data["contact"].(map[string]any)["id"].(string), data["conversation"].(map[string]any)["id"].(string)
}

// online 将测试成员置于真实可接待状态。
func (f *fixture) online(member string) {
	f.t.Helper()
	status, result := f.request("PATCH", "/api/v1/agent/status", member, "", map[string]any{"available": true, "busy": false})
	if status != 200 {
		f.t.Fatalf("上线失败：%d %v", status, result)
	}
}
