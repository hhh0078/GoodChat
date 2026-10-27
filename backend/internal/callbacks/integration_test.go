// 文件作用说明：在现有PostgreSQL随机schema与本机HTTP接收器中验证回调去重、筛选、签名、重试及配置失效。
package callbacks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// callbackIntegration 建立随机schema，不修改现有租户数据或发送外部请求。
func callbackIntegration(t *testing.T) (*Service, string) {
	t.Helper()
	if os.Getenv("SUPPORT_CALLBACK_INTEGRATION") != "1" {
		t.Skip("设置SUPPORT_CALLBACK_INTEGRATION=1启用真实PostgreSQL测试")
	}
	var local struct {
		DatabaseURL  string `json:"database_url"`
		MigrationURL string `json:"migration_url"`
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", ".local", "dev-database.json"))
	if err != nil || json.Unmarshal(body, &local) != nil {
		t.Fatal("独立数据库配置不可用")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, local.MigrationURL)
	if err != nil {
		t.Fatal("迁移连接不可用")
	}
	random := make([]byte, 12)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "callbacks_test_" + hex.EncodeToString(random)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close(context.Background())
	})
	if _, err = admin.Exec(ctx, "SET search_path TO "+quoted+",public"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"000_base.sql", "001_identity.sql", "003_callbacks.sql"} {
		b, e := os.ReadFile(filepath.Join("..", "database", "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = admin.Exec(ctx, string(b)); e != nil {
			t.Fatal(e)
		}
	}
	config, err := pgxpool.ParseConfig(local.DatabaseURL)
	if err != nil {
		t.Fatal("运行配置无效")
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var role string
	var elevated bool
	if err = pool.QueryRow(ctx, "SELECT current_user,rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user").Scan(&role, &elevated); err != nil || elevated {
		t.Fatal("测试需要非特权运行角色")
	}
	role = pgx.Identifier{role}.Sanitize()
	if _, err = admin.Exec(ctx, "GRANT USAGE ON SCHEMA "+quoted+" TO "+role+";GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA "+quoted+" TO "+role+";GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA "+quoted+" TO "+role); err != nil {
		t.Fatal(err)
	}
	var tenant string
	if err = admin.QueryRow(ctx, "INSERT INTO tenants(name,slug)VALUES('回调隔离测试','callbacks-test')RETURNING id::text").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	return New(pool, nil, platform.Config{AuthSecret: "test-only-callbacks-secret", CallbackAllowLocal: true}), tenant
}

// TestPersistentCallbackRetryAndCancellation 验证真实入队、重复访问、数据最小化、HMAC、失败重试和配置变更取消。
func TestPersistentCallbackRetryAndCancellation(t *testing.T) {
	s, tenant := callbackIntegration(t)
	ctx := context.Background()
	var mu sync.Mutex
	calls := 0
	var bodies [][]byte
	var eventIDs []string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-GoodChat-Signature") != signature("receiver-secret", r.Header.Get("X-GoodChat-Timestamp"), b) {
			t.Error("HMAC未覆盖实际请求正文")
		}
		mu.Lock()
		defer mu.Unlock()
		calls++
		bodies = append(bodies, b)
		eventIDs = append(eventIDs, r.Header.Get("X-GoodChat-Event-ID"))
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	encrypted, err := s.encrypt("receiver-secret")
	if err != nil {
		t.Fatal(err)
	}
	err = platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO tenant_callback_settings(tenant_id,enabled,url,secret_encrypted)VALUES($1,true,$2,$3)", tenant, receiver.URL, encrypted); err != nil {
			return err
		}
		payload := map[string]any{"display_name": "访客", "phone": "18800000000", "profile": map[string]any{"display_name": "访客", "phone": "18800000000"}, "context": map[string]any{"private": "不应发送"}}
		if err := s.Enqueue(ctx, tx, tenant, "visit-once", payload); err != nil {
			return err
		}
		return s.Enqueue(ctx, tx, tenant, "visit-once", payload)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.claim(ctx) {
		t.Fatal("任务未认领")
	}
	var status string
	var attempts, count int
	err = platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT status,attempts FROM callback_deliveries WHERE tenant_id=$1", tenant).Scan(&status, &attempts); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM callback_deliveries").Scan(&count); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE callback_dispatch_queue SET next_attempt_at=now() WHERE tenant_id=$1", tenant)
		return err
	})
	if err != nil || status != "retrying" || attempts != 1 || count != 1 {
		t.Fatalf("去重/重试结果异常 %s %d %d %v", status, attempts, count, err)
	}
	if !s.claim(ctx) {
		t.Fatal("重试未认领")
	}
	err = platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT status,attempts FROM callback_deliveries WHERE tenant_id=$1", tenant).Scan(&status, &attempts)
	})
	if err != nil || status != "delivered" || attempts != 2 {
		t.Fatalf("回调未成功: %s %d %v", status, attempts, err)
	}
	mu.Lock()
	if calls != 2 || eventIDs[0] == "" || eventIDs[0] != eventIDs[1] {
		t.Error("重试事件标识必须稳定")
	}
	var payload map[string]any
	json.Unmarshal(bodies[0], &payload)
	mu.Unlock()
	if payload["phone"] != nil || payload["context"] != nil || payload["profile"].(map[string]any)["phone"] != nil {
		t.Fatal("回调泄漏未授权字段")
	}
	err = platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		if err := s.Enqueue(ctx, tx, tenant, "visit-cancel", map[string]any{}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE tenant_callback_settings SET version=version+1 WHERE tenant_id=$1", tenant)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.claim(ctx) {
		t.Fatal("取消任务未认领")
	}
	mu.Lock()
	if calls != 2 {
		t.Error("配置变更后仍发送旧任务")
	}
	mu.Unlock()
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM callback_deliveries").Scan(&count); err != nil || count != 0 {
		t.Fatal("缺少租户上下文仍可读取回调载荷")
	}
}
