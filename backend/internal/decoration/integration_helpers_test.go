// 文件作用说明：仅在显式指定的独立goodchat_saas测试库中建立装修测试租户，不创建数据库或容器。
package decoration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// integrationFixture 保存仅属于本次测试的两组随机租户及测试服务器。
type integrationFixture struct {
	t                                                                    *testing.T
	pool, admin                                                          *pgxpool.Pool
	service                                                              *Service
	server                                                               *httptest.Server
	principals                                                           map[string]platform.Principal
	tenant, other, user, otherUser, member, otherMember, link, otherLink string
}

// integrationAuth 在测试内注入经过预置成员，实际业务仍走RLS和模块鉴权。
type integrationAuth struct{ principals map[string]platform.Principal }

// RequireUser 使用受控测试成员请求，不接受任意租户编号。
func (a integrationAuth) RequireUser(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireTenant(next)
}

// RequireTenant 从测试成员表选择预置身份。
func (a integrationAuth) RequireTenant(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.principals[r.Header.Get("X-Test-Member")]
		if !ok {
			platform.ErrorJSON(w, r, platform.Fail(401, "unauthorized", "需要登录"))
			return
		}
		next(w, r.WithContext(platform.WithPrincipal(r.Context(), p)))
	}
}

// RequireAdmin 仅允许预置租户管理员执行装修写操作。
func (a integrationAuth) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireTenant(func(w http.ResponseWriter, r *http.Request) {
		p := platform.Current(r)
		if p.Role != "owner" && p.Role != "admin" {
			platform.ErrorJSON(w, r, platform.Fail(403, "forbidden", "需要管理员权限"))
			return
		}
		next(w, r)
	})
}

// RequirePlatform 测试不提供平台管理员身份。
func (a integrationAuth) RequirePlatform(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		platform.ErrorJSON(w, r, platform.Fail(403, "forbidden", "未提供平台管理员身份"))
	}
}

// newIntegrationFixture 检查数据库名和应用角色后创建可清理的隔离测试账号。
func newIntegrationFixture(t *testing.T, cfg platform.Config) *integrationFixture {
	t.Helper()
	path := os.Getenv("GOODCHAT_TEST_CONFIG")
	if path == "" {
		t.Skip("未提供独立PostgreSQL测试库配置")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("无法读取独立测试配置")
	}
	var private struct {
		DatabaseURL  string `json:"database_url"`
		MigrationURL string `json:"migration_url"`
	}
	if json.Unmarshal(raw, &private) != nil {
		t.Fatal("测试配置格式无效")
	}
	for _, dsn := range []string{private.DatabaseURL, private.MigrationURL} {
		parsed, err := pgxpool.ParseConfig(dsn)
		if err != nil || parsed.ConnConfig.Database != "goodchat_saas" {
			t.Fatal("安全检查失败：仅允许goodchat_saas独立测试库")
		}
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, private.DatabaseURL)
	if err != nil {
		t.Fatal("无法连接应用测试库")
	}
	admin, err := pgxpool.New(ctx, private.MigrationURL)
	if err != nil {
		pool.Close()
		t.Fatal("无法连接迁移测试库")
	}
	f := &integrationFixture{t: t, pool: pool, admin: admin, principals: map[string]platform.Principal{}}
	f.tenant, _ = newID()
	f.other, _ = newID()
	f.user, _ = newID()
	f.otherUser, _ = newID()
	f.member, _ = newID()
	f.otherMember, _ = newID()
	t.Cleanup(func() {
		if f.server != nil {
			f.server.Close()
		}
		f.cleanup()
		pool.Close()
		admin.Close()
	})
	var super, bypass, owner bool
	if err := pool.QueryRow(ctx, "SELECT r.rolsuper,r.rolbypassrls,c.relowner=r.oid FROM pg_roles r JOIN pg_class c ON c.relname='tenant_templates' WHERE r.rolname=current_user").Scan(&super, &bypass, &owner); err != nil {
		t.Fatal("无法验证装修表应用角色，需先迁移006")
	}
	if super || bypass || owner {
		t.Fatal("集成测试必须使用非超级、非BYPASSRLS、非表所有者角色")
	}
	for i, tenant := range []string{f.tenant, f.other} {
		user, member := f.user, f.member
		if i == 1 {
			user, member = f.otherUser, f.otherMember
		}
		if _, err := admin.Exec(ctx, "INSERT INTO users(id,email,display_name,password_hash) VALUES($1,$2,'装修测试成员','test-only-unusable-password')", user, "decoration-test-"+user+"@example.invalid"); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, "INSERT INTO tenants(id,name,slug) VALUES($1,'装修隔离测试',$2)", tenant, "decoration-test-"+tenant); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, "INSERT INTO tenant_memberships(id,tenant_id,user_id,role) VALUES($1,$2,$3,'owner')", member, tenant, user); err != nil {
			t.Fatal(err)
		}
		f.principals[member] = platform.Principal{TenantID: tenant, UserID: user, MembershipID: member, Role: "owner"}
		if err := platform.TenantTx(ctx, pool, tenant, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT ensure_chat_defaults($1::uuid,$2::uuid)", tenant, user); err != nil {
				return err
			}
			var link string
			if err := tx.QueryRow(ctx, "SELECT id::text FROM access_links WHERE is_default").Scan(&link); err != nil {
				return err
			}
			if i == 0 {
				f.link = link
			} else {
				f.otherLink = link
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	cfg.PublicURL = "http://127.0.0.1:13000"
	cfg.CodeOrigin = "http://localhost:18081"
	cfg.AIDailyLimit = 20
	f.service, err = New(pool, integrationAuth{f.principals}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	f.service.Register(mux)
	f.server = httptest.NewServer(mux)
	return f
}

// cleanup 在测试管理员事务内仅清理本次随机租户，临时关闭当前事务触发器以删除不可变测试快照。
func (f *integrationFixture) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := f.admin.Begin(ctx)
	if err != nil {
		f.t.Log("测试清理连接失败")
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); err != nil {
		f.t.Log("测试快照清理需要测试管理员权限")
		return
	}
	rows, err := tx.Query(ctx, "SELECT table_name FROM information_schema.columns WHERE table_schema='public' AND column_name='tenant_id' ORDER BY table_name")
	if err != nil {
		return
	}
	tables := []string{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return
		}
		tables = append(tables, table)
	}
	rows.Close()
	for _, table := range tables {
		if _, err := tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE tenant_id=ANY($1::uuid[])", []string{f.tenant, f.other}); err != nil {
			f.t.Logf("测试租户清理失败：%v", err)
			return
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM tenants WHERE id=ANY($1::uuid[])", []string{f.tenant, f.other}); err != nil {
		return
	}
	if _, err := tx.Exec(ctx, "DELETE FROM users WHERE id=ANY($1::uuid[])", []string{f.user, f.otherUser}); err != nil {
		return
	}
	if err := tx.Commit(ctx); err != nil {
		f.t.Log("测试清理提交失败")
	}
}

// request 执行测试HTTP请求且不在输出中打印密钥、数据库连接或访客令牌。
func (f *integrationFixture) request(method, path, member string, body any) (int, map[string]any) {
	f.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	request, err := http.NewRequest(method, f.server.URL+"/api/v1"+path, bytes.NewReader(raw))
	if err != nil {
		f.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-Member", member)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	result := map[string]any{}
	_ = json.Unmarshal(data, &result)
	return response.StatusCode, result
}

// mustRequest 确认HTTP状态并提取成功数据，错误只记录无敏感信息的业务响应。
func (f *integrationFixture) mustRequest(method, path, member string, body any) map[string]any {
	f.t.Helper()
	status, result := f.request(method, path, member, body)
	if status != 200 {
		f.t.Fatalf("装修请求%s %s失败：%d %v", method, path, status, result)
	}
	data, ok := result["data"].(map[string]any)
	if !ok {
		f.t.Fatal("响应没有对象data")
	}
	return data
}

// renderRequest 直接调用独立handler并设置正确的版本来源，用于校验静态公开读取边界。
func (f *integrationFixture) renderRequest(rawURL string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", rawURL, nil)
	response := httptest.NewRecorder()
	f.service.CodeHandler().ServeHTTP(response, request)
	return response
}

// containsString 安全检查JSON设计文本，避免将用例与字段顺序耦合。
func containsString(value any, expected string) bool {
	raw, _ := json.Marshal(value)
	return strings.Contains(string(raw), expected)
}
