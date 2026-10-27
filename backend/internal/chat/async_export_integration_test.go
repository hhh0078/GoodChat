// 文件作用说明：在真实隔离PG中验证异步导出超同步上限、持久认领、租户权限、幂等、取消、到期与失败整份拒绝。
package chat

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// createExportTest 通过真实HTTP创建管理员导出任务并返回数据投影。
func createExportTest(t *testing.T, f *fixture, client, filters string) map[string]any {
	t.Helper()
	code, body := f.request("POST", "/api/v1/tenant/export-jobs", f.owner, "", map[string]any{"client_request_id": client, "filters": filters})
	if code != 201 {
		t.Fatalf("新导出应返回201，实际%d code=%v", code, body["code"])
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatal("新任务缺少data对象")
	}
	return data
}

// TestAsyncExportBeyondSyncLimit 验证10006行可异步完整生成，进程重建可认领，下载继续检查租户角色和有效期。
func TestAsyncExportBeyondSyncLimit(t *testing.T) {
	f := newFixture(t)
	_, contact, conversation := f.visitor("")
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, "UPDATE contacts SET display_name='=1+1' WHERE id=$1", contact); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, `INSERT INTO conversations(tenant_id,service_space_id,contact_id,created_entry_id,service_group_id,routing_pool_id,status,closed_at) SELECT c.tenant_id,c.service_space_id,c.contact_id,c.created_entry_id,c.service_group_id,c.routing_pool_id,'closed',now() FROM conversations c CROSS JOIN generate_series(1,10005) WHERE c.id=$1`, conversation); err != nil {
		t.Fatal(err)
	}
	if response := f.downloadCSV("/api/v1/tenant/chat-records/export", f.owner, ""); response.status != 409 {
		t.Fatalf("同步超限应拒绝，实际%d", response.status)
	}
	job := createExportTest(t, f, newID(), "")
	id := job["id"].(string)
	restarted := New(f.pool, testAuthorizer{f.principals}, platform.Config{DataDir: t.TempDir()})
	var group sync.WaitGroup
	accepted := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); accepted <- restarted.claimExport(ctx) }()
	}
	group.Wait()
	close(accepted)
	claims := 0
	for value := range accepted {
		if value {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("同一持久任务只能认领一次，实际%d", claims)
	}
	status, body := f.request("GET", "/api/v1/tenant/export-jobs", f.owner, "", nil)
	items := mustData(t, status, body)["items"].([]any)
	row := items[0].(map[string]any)
	if row["status"] != "completed" || row["row_count"] != float64(10006) {
		t.Fatalf("异步结果不完整:%v", row)
	}
	if _, ok := row["csv_body"]; ok {
		t.Fatal("列表不得返回CSV载荷")
	}
	if _, ok := row["lease_token"]; ok {
		t.Fatal("列表不得返回内部认领令牌")
	}
	response := f.downloadCSV("/api/v1/tenant/export-jobs/"+id+"/download", f.owner, "")
	if response.status != 200 {
		t.Fatalf("异步下载失败:%d %s", response.status, response.body)
	}
	records := csvRecords(t, response)
	if len(records) != 10007 || records[1][2] != "'=1+1" {
		t.Fatal("异步导出被截断或公式未处理")
	}
	for member, want := range map[string]int{f.agent: 403, f.otherOwner: 404} {
		if got := f.downloadCSV("/api/v1/tenant/export-jobs/"+id+"/download", member, ""); got.status != want {
			t.Fatalf("权限隔离:%d want%d", got.status, want)
		}
	}
	if _, err := f.admin.Exec(ctx, "UPDATE chat_export_jobs SET expires_at=now()-interval '1 second' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if got := f.downloadCSV("/api/v1/tenant/export-jobs/"+id+"/download", f.owner, ""); got.status != 410 {
		t.Fatal("到期结果仍可下载")
	}
	restarted.cleanupExports(ctx)
	var cleared bool
	if err := f.admin.QueryRow(ctx, "SELECT csv_body IS NULL FROM chat_export_jobs WHERE id=$1", id).Scan(&cleared); err != nil || !cleared {
		t.Fatal("到期CSV未清理", err)
	}
}

// TestAsyncExportIdempotencyQuotaAndCancellation 验证并发重放只创建一次，任务上限、改参冲突和取消不留下可下载的半份结果。
func TestAsyncExportIdempotencyQuotaAndCancellation(t *testing.T) {
	f := newFixture(t)
	f.visitor("")
	client := newID()
	var group sync.WaitGroup
	results := make(chan map[string]any, 12)
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			code, body := f.request("POST", "/api/v1/tenant/export-jobs", f.owner, "", map[string]any{"client_request_id": client, "filters": "include_messages=true"})
			if code != 200 && code != 201 {
				t.Errorf("并发重放失败:%d", code)
				return
			}
			results <- body["data"].(map[string]any)
		}()
	}
	group.Wait()
	close(results)
	id := ""
	for row := range results {
		if id == "" {
			id = row["id"].(string)
		}
		if row["id"] != id {
			t.Fatal("同一请求创建了不同任务")
		}
	}
	code, _ := f.request("POST", "/api/v1/tenant/export-jobs", f.owner, "", map[string]any{"client_request_id": client, "filters": "include_messages=false"})
	if code != 409 {
		t.Fatal("幂等编号换参未拒绝")
	}
	second := createExportTest(t, f, newID(), "")
	code, _ = f.request("POST", "/api/v1/tenant/export-jobs", f.owner, "", map[string]any{"client_request_id": newID(), "filters": ""})
	if code != 409 {
		t.Fatal("超过两个待处理任务仍允许提交")
	}
	code, body := f.request("POST", "/api/v1/tenant/export-jobs/"+id+"/cancel", f.owner, "", map[string]any{})
	mustData(t, code, body)
	if got := f.downloadCSV("/api/v1/tenant/export-jobs/"+id+"/download", f.owner, ""); got.status != 409 {
		t.Fatal("取消任务仍提供CSV")
	}
	f.s.claimExport(context.Background())
	var state string
	if err := f.admin.QueryRow(context.Background(), "SELECT status FROM chat_export_jobs WHERE id=$1", second["id"]).Scan(&state); err != nil || state != "completed" {
		t.Fatal("取消后另一个任务未继续", err)
	}
}

// TestAsyncExportRevocationAndSharedLimits 验证生成前权益撤销、下载时撤权及共享生成器遇取消或超限不返回部分CSV。
func TestAsyncExportRevocationAndSharedLimits(t *testing.T) {
	f := newFixture(t)
	f.visitor("")
	job := createExportTest(t, f, newID(), "")
	f.s.SetFeatureChecker(func(context.Context, pgx.Tx, string, string) error {
		return platform.Fail(403, "feature_not_available", "测试导出权益已关闭")
	})
	f.s.claimExport(context.Background())
	var state string
	var empty bool
	if err := f.admin.QueryRow(context.Background(), "SELECT status,csv_body IS NULL FROM chat_export_jobs WHERE id=$1", job["id"]).Scan(&state, &empty); err != nil || state != "failed" || !empty {
		t.Fatal("权益撤销后生成了文件", err)
	}
	f.s.SetFeatureChecker(nil)
	job = createExportTest(t, f, newID(), "")
	f.s.claimExport(context.Background())
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.owner); err != nil {
		t.Fatal(err)
	}
	if got := f.downloadCSV("/api/v1/tenant/export-jobs/"+job["id"].(string)+"/download", f.owner, ""); got.status != 403 {
		t.Fatal("已停用成员仍可下载")
	}
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET disabled=false WHERE id=$1", f.owner); err != nil {
		t.Fatal(err)
	}
	a := actor{TenantID: f.tenant, MemberID: f.owner, UserID: f.principals[f.owner].UserID, Role: "owner", CanServe: f.principals[f.owner].CanServe}
	for _, limit := range []string{"bytes", "cancel"} {
		err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
			bytes := 1000000
			var progress func(int) error
			if limit == "bytes" {
				bytes = 1
			} else {
				progress = func(int) error { return platform.Fail(409, "export_cancelled", "任务已取消") }
			}
			data, _, _, err := renderExportCSV(context.Background(), tx, httptest.NewRequest("GET", "/export", nil), a, 100, bytes, progress)
			var typed *platform.APIError
			if !errors.As(err, &typed) || len(data) != 0 {
				t.Fatalf("%s错误时返回了部分CSV", limit)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"tenant_id=evil", "include_internal=true", "query=a&query=b", "from=%ZZ"} {
		if _, err := exportFilters(query); err == nil {
			t.Fatalf("非法参数未拒绝:%s", strings.Split(query, "=")[0])
		}
	}
}

// TestAsyncExportStaleLeaseCannotReplaceCurrent 验证暂停后的旧worker不能抢回新世代，也不能删除新认领。
func TestAsyncExportStaleLeaseCannotReplaceCurrent(t *testing.T) {
	f := newFixture(t)
	f.visitor("")
	job := createExportTest(t, f, newID(), "")
	id := job["id"].(string)
	old, current := newID(), newID()
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, "UPDATE chat_export_dispatch SET lease_token=$2,lease_until=now()+interval '3 minutes' WHERE job_id=$1", id, current); err != nil {
		t.Fatal(err)
	}
	f.s.generateExport(ctx, id, f.tenant, old)
	var state, lease string
	if err := f.admin.QueryRow(ctx, "SELECT j.status,q.lease_token::text FROM chat_export_jobs j JOIN chat_export_dispatch q ON q.job_id=j.id WHERE j.id=$1", id).Scan(&state, &lease); err != nil || state != "queued" || lease != current {
		t.Fatal("旧世代覆盖或删除了当前认领", err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE chat_export_jobs SET status='running',lease_token=$2 WHERE id=$1", id, old); err != nil {
		t.Fatal(err)
	}
	a := actor{TenantID: f.tenant, MemberID: f.owner, UserID: f.principals[f.owner].UserID, Role: "owner", CanServe: f.principals[f.owner].CanServe}
	if err := f.s.finishExport(ctx, id, f.tenant, old, a, "", []byte("旧世代结果"), 1, "Asia/Shanghai", 0, nil); err != nil {
		t.Fatal(err)
	}
	var empty bool
	if err := f.admin.QueryRow(ctx, "SELECT j.status,j.csv_body IS NULL,q.lease_token::text FROM chat_export_jobs j JOIN chat_export_dispatch q ON q.job_id=j.id WHERE j.id=$1", id).Scan(&state, &empty, &lease); err != nil || state != "running" || !empty || lease != current {
		t.Fatal("旧完成结果抢在新世代初始化前发布", err)
	}
	f.s.generateExport(ctx, id, f.tenant, current)
	if err := f.admin.QueryRow(ctx, "SELECT status FROM chat_export_jobs WHERE id=$1", id).Scan(&state); err != nil || state != "completed" {
		t.Fatal("当前世代未能生成", err)
	}
}

// TestAsyncExportRetainedStorageAndDailyBudget 验证完成文件仍占租户预算，待执行预留可防止并发超存储，取消也不退日次数。
func TestAsyncExportRetainedStorageAndDailyBudget(t *testing.T) {
	f := newFixture(t)
	f.visitor("")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		job := createExportTest(t, f, newID(), "")
		if _, err := f.admin.Exec(ctx, "UPDATE chat_export_jobs SET status='completed',size_bytes=$2,csv_body=decode('78','hex') WHERE id=$1", job["id"], asyncExportBytes); err != nil {
			t.Fatal(err)
		}
	}
	createExportTest(t, f, newID(), "")
	status, body := f.request("POST", "/api/v1/tenant/export-jobs", f.owner, "", map[string]any{"client_request_id": newID(), "filters": ""})
	if status != 409 || body["code"] != "export_storage_limit" {
		t.Fatalf("保留文件及预留未计容量:%d %v", status, body["code"])
	}
	if _, err := f.admin.Exec(ctx, "UPDATE chat_export_jobs SET status='cancelled',size_bytes=0,csv_body=NULL"); err != nil {
		t.Fatal(err)
	}
	for i := 4; i < asyncExportDailyLimit; i++ {
		job := createExportTest(t, f, newID(), "")
		if _, err := f.admin.Exec(ctx, "UPDATE chat_export_jobs SET status='failed' WHERE id=$1", job["id"]); err != nil {
			t.Fatal(err)
		}
	}
	status, body = f.request("POST", "/api/v1/tenant/export-jobs", f.owner, "", map[string]any{"client_request_id": newID(), "filters": ""})
	if status != 429 || body["code"] != "export_daily_limit" {
		t.Fatalf("取消失败任务未计日次数:%d %v", status, body["code"])
	}
}

// TestAsyncBodyExportInvalidatesAfterWithdraw 验证撤回提交后旧正文缓存失效且清理真正提交，时间早于任务开始也不能漏判。
func TestAsyncBodyExportInvalidatesAfterWithdraw(t *testing.T) {
	f := newFixture(t)
	token, _, conversation := f.visitor("")
	code, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "导出完成后撤回的唯一正文"})
	message := mustData(t, code, reply)
	body := createExportTest(t, f, newID(), "include_messages=true")
	metadata := createExportTest(t, f, newID(), "")
	f.s.claimExport(context.Background())
	f.s.claimExport(context.Background())
	before := f.downloadCSV("/api/v1/tenant/export-jobs/"+body["id"].(string)+"/download", f.owner, "")
	if before.status != 200 || !strings.Contains(string(before.body), "导出完成后撤回的唯一正文") {
		t.Fatal("测试前置正文未成功生成")
	}
	code, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+message["id"].(string)+"/withdraw", "", token, map[string]any{})
	mustData(t, code, reply)
	// 模拟撤回事务很早开始但晚提交，证明不能仅比较withdrawn_at与started_at。
	if _, err := f.admin.Exec(context.Background(), "UPDATE messages SET withdrawn_at='2000-01-01' WHERE id=$1", message["id"]); err != nil {
		t.Fatal(err)
	}
	after := f.downloadCSV("/api/v1/tenant/export-jobs/"+body["id"].(string)+"/download", f.owner, "")
	if after.status != 409 || strings.Contains(string(after.body), "导出完成后撤回的唯一正文") {
		t.Fatal("新下载重新暴露了已撤回正文")
	}
	var cleared bool
	if err := f.admin.QueryRow(context.Background(), "SELECT csv_body IS NULL AND status='failed' AND error_code='export_redacted' AND size_bytes=0 FROM chat_export_jobs WHERE id=$1", body["id"]).Scan(&cleared); err != nil || !cleared {
		t.Fatal("失效清理被错误事务回滚", err)
	}
	if got := f.downloadCSV("/api/v1/tenant/export-jobs/"+metadata["id"].(string)+"/download", f.owner, ""); got.status != 200 {
		t.Fatal("元数据导出不应因消息撤回失效")
	}
	fresh := createExportTest(t, f, newID(), "include_messages=true")
	f.s.claimExport(context.Background())
	got := f.downloadCSV("/api/v1/tenant/export-jobs/"+fresh["id"].(string)+"/download", f.owner, "")
	if got.status != 200 || strings.Contains(string(got.body), "导出完成后撤回的唯一正文") {
		t.Fatal("重新生成后仍有已撤回正文")
	}
}
