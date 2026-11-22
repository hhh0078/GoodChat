// 文件作用说明：使用真实独立PostgreSQL随机schema验证通知事务、RLS、并发和恢复，不外发真实邮件。
package notifications

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"supportdesk/backend/internal/platform"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fixture 保存随机schema和通知服务，所有测试数据均随schema清理。
type fixture struct {
	s                   *Service
	admin               *pgx.Conn
	pool                *pgxpool.Pool
	user, other, tenant string
}

// integration 仅在显式开启时使用独立goodchat_saas库，迁移副本固定引用改为随机schema。
func integration(t *testing.T) *fixture {
	t.Helper()
	if os.Getenv("SUPPORT_NOTICE_INTEGRATION") != "1" {
		t.Skip("需显式开启独立数据库集成测试")
	}
	path := os.Getenv("GOODCHAT_TEST_CONFIG")
	if path == "" {
		path = filepath.Join("..", "..", "..", ".local", "dev-database.json")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal("独立测试配置不可用")
	}
	var cfg struct {
		Database  string `json:"database_url"`
		Migration string `json:"migration_url"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("配置无效")
	}
	parsed, e := pgxpool.ParseConfig(cfg.Database)
	if e != nil || !(parsed.ConnConfig.Database == "goodchat_saas" || (strings.HasPrefix(parsed.ConnConfig.Database, "goodchat_saas_test_") && (parsed.ConnConfig.Host == "127.0.0.1" || parsed.ConnConfig.Host == "localhost" || parsed.ConnConfig.Host == "::1"))) {
		t.Fatal("只允许独立测试库")
	}
	ctx := context.Background()
	admin, e := pgx.Connect(ctx, cfg.Migration)
	if e != nil {
		t.Fatal("独立数据库连接失败")
	}
	token, _ := platform.Token()
	schema := "notice_test_" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(token))[:20]
	q := pgx.Identifier{schema}.Sanitize()
	if _, e := admin.Exec(ctx, "CREATE SCHEMA "+q); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP SCHEMA "+q+" CASCADE")
		admin.Close(context.Background())
	})
	admin.Exec(ctx, "SET search_path TO "+q+",public")
	files, e := os.ReadDir(filepath.Join("..", "database", "migrations"))
	if e != nil {
		t.Fatal(e)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		b, e := os.ReadFile(filepath.Join("..", "database", "migrations", file.Name()))
		if e != nil {
			t.Fatal(e)
		}
		sql := strings.ReplaceAll(string(b), "public.", schema+".")
		sql = strings.ReplaceAll(sql, "table_schema='public'", "table_schema='"+schema+"'")
		sql = strings.ReplaceAll(sql, "pg_catalog,public", "pg_catalog,"+schema)
		if _, e := admin.Exec(ctx, sql); e != nil {
			t.Fatalf("迁移%s: %v", file.Name(), e)
		}
	}
	parsed.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, e := pgxpool.NewWithConfig(ctx, parsed)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	var role string
	pool.QueryRow(ctx, "SELECT current_user").Scan(&role)
	r := pgx.Identifier{role}.Sanitize()
	for _, sql := range []string{"GRANT USAGE ON SCHEMA " + q + " TO " + r, "GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + q + " TO " + r, "GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + q + " TO " + r} {
		if _, e := admin.Exec(ctx, sql); e != nil {
			t.Fatal(e)
		}
	}
	f := &fixture{admin: admin, pool: pool}
	f.s = New(pool, nil, platform.Config{PublicURL: "https://goodchat.example"}, func(context.Context, string, Mail) (string, string) { return "accepted", "" })
	if e := admin.QueryRow(ctx, "INSERT INTO users(email,display_name,password_hash,is_platform_admin,preferred_locale,display_timezone) VALUES('owner@notice.invalid','Owner','unused',true,'en','UTC') RETURNING id::text").Scan(&f.user); e != nil {
		t.Fatal(e)
	}
	if e := admin.QueryRow(ctx, "INSERT INTO users(email,display_name,password_hash) VALUES('other@notice.invalid','Other','unused') RETURNING id::text").Scan(&f.other); e != nil {
		t.Fatal(e)
	}
	if e := admin.QueryRow(ctx, "INSERT INTO tenants(name,slug) VALUES('通知测试','notice-test') RETURNING id::text").Scan(&f.tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'owner')", f.tenant, f.user); e != nil {
		t.Fatal(e)
	}
	return f
}

// enqueue 事务内创建一封本人安全通知。
func (f *fixture) enqueue(t *testing.T, key string) {
	t.Helper()
	if e := f.s.systemTx(context.Background(), func(tx pgx.Tx) error {
		return Enqueue(context.Background(), tx, Event{User: f.user, Kind: "password_changed", Key: key})
	}); e != nil {
		t.Fatal(e)
	}
}

// status 返回指定事件的真实数据库状态。
func (f *fixture) status(t *testing.T, key string) string {
	t.Helper()
	var v string
	if e := f.admin.QueryRow(context.Background(), "SELECT status FROM notification_jobs WHERE event_key=$1 OR event_key=tenant_id::text||':'||$1", key).Scan(&v); e != nil {
		t.Fatal(e)
	}
	var failure string
	var attempts int
	f.admin.QueryRow(context.Background(), "SELECT failure_kind,attempts FROM notification_jobs WHERE event_key=$1 OR event_key=tenant_id::text||':'||$1", key).Scan(&failure, &attempts)
	t.Log("通知事件状态", key, v, failure, attempts)
	return v
}

// TestQueueConcurrency 验证重复事件并发只发送一次，无上下文RLS隐藏任务及回滚不入队。
func TestQueueConcurrency(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	var sent atomic.Int32
	f.s.send = func(context.Context, string, Mail) (string, string) {
		sent.Add(1)
		time.Sleep(10 * time.Millisecond)
		return "accepted", ""
	}
	for i := 0; i < 3; i++ {
		f.enqueue(t, "one")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.s.tick(ctx) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if sent.Load() != 1 || f.status(t, "one") != "accepted" {
		t.Fatal("未保持单次发送")
	}
	var n int
	if e := f.pool.QueryRow(ctx, "SELECT count(*) FROM notification_jobs").Scan(&n); e != nil || n != 0 {
		t.Fatal("无上下文泄露任务", e)
	}
	tx, e := f.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e := Enqueue(ctx, tx, Event{User: f.user, Kind: "welcome", Key: "rollback"}); e != nil {
		t.Fatal(e)
	}
	tx.Rollback(ctx)
	f.admin.QueryRow(ctx, "SELECT count(*) FROM notification_jobs WHERE event_key='rollback'").Scan(&n)
	if n != 0 {
		t.Fatal("回滚未撤销任务")
	}
}

// TestRetryAndUnknown 验证确定临时失败有限退避、最终确认丢失和租约中断不自动重发。
func TestRetryAndUnknown(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	f.s.send = func(context.Context, string, Mail) (string, string) { return "failed", "temporary" }
	f.enqueue(t, "temporary")
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.status(t, "temporary") != "queued" {
		t.Fatal("未退避")
	}
	f.admin.Exec(ctx, "UPDATE notification_jobs SET due_at=now(),attempts=3 WHERE event_key='temporary'")
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.status(t, "temporary") != "failed" {
		t.Fatal("未限制次数")
	}
	f.enqueue(t, "unknown")
	f.s.send = func(context.Context, string, Mail) (string, string) { return "unknown", "confirmation_lost" }
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.status(t, "unknown") != "unknown" {
		t.Fatal("未知结果错误")
	}
	f.enqueue(t, "crashed")
	f.admin.Exec(ctx, "UPDATE notification_jobs SET status='running',lease_until=now()-interval '1 second' WHERE event_key='crashed'")
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.status(t, "crashed") != "unknown" {
		t.Fatal("中断未转未知")
	}
}

// TestQuietPreference 验证一般邮件静默延迟、安全邮件即时、验证码禁止进入持久任务。
func TestQuietPreference(t *testing.T) {
	f := integration(t)
	ctx := context.Background()
	var h int
	f.admin.QueryRow(ctx, "SELECT extract(hour FROM statement_timestamp() AT TIME ZONE 'UTC')::int").Scan(&h)
	f.admin.Exec(ctx, "INSERT INTO mail_notification_preferences(user_id,quiet_start,quiet_end) VALUES($1,$2,$3)", f.user, h, (h+1)%24)
	if e := f.s.systemTx(ctx, func(tx pgx.Tx) error { return Enqueue(ctx, tx, Event{User: f.user, Kind: "welcome", Key: "quiet"}) }); e != nil {
		t.Fatal(e)
	}
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.status(t, "quiet") != "queued" {
		t.Fatal("静默未延迟")
	}
	f.enqueue(t, "security")
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.status(t, "security") != "accepted" {
		t.Fatal("安全邮件被静默")
	}
	if e := f.s.systemTx(ctx, func(tx pgx.Tx) error {
		return Enqueue(ctx, tx, Event{User: f.user, Kind: "welcome", Key: "secret", Params: map[string]string{"code": "123456"}})
	}); e == nil {
		t.Fatal("验证码允许入队")
	}
}
