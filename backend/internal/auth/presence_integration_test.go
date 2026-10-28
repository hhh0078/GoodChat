// 文件作用说明：使用真实Cookie身份与PostgreSQL验证成员在线投影、多标签续租合并及总管理员监控权限。
package auth

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/monitoring"
	"supportdesk/backend/internal/platform"
	"supportdesk/backend/internal/presence"
)

// TestMemberPresenceIsMergedAndProjected 验证多标签真实续租只保留一个区间且成员列表按租约显示在线。
func TestMemberPresenceIsMergedAndProjected(t *testing.T) {
	f := identityIntegration(t)
	account, session := f.register(t, "presence@example.com")
	member := account.Memberships[0]
	ctx := context.Background()
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			failures <- platform.TenantTx(ctx, f.pool, member.TenantID, func(tx pgx.Tx) error { return presence.Renew(ctx, tx, member.TenantID, member.ID) })
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := platform.TenantTx(ctx, f.pool, member.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM member_presence_intervals WHERE membership_id=$1", member.ID).Scan(&count)
	}); err != nil || count != 1 {
		t.Fatal("多标签在线区间未合并", err)
	}
	response := f.call("GET", "/api/v1/tenant/members", nil, session, member.TenantID)
	requireStatus(t, response, 200)
	var envelope struct {
		Data struct {
			Items []memberView `json:"items"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || len(envelope.Data.Items) != 1 || !envelope.Data.Items[0].Online || envelope.Data.Items[0].LastSeenAt == nil {
		t.Fatal("成员列表缺少真实在线投影")
	}
	if err := platform.TenantTx(ctx, f.pool, member.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE member_presence_intervals SET started_at=now()-interval '120 seconds',last_seen_at=now()-interval '90 seconds',online_until=now()-interval '45 seconds' WHERE membership_id=$1", member.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	response = f.call("GET", "/api/v1/tenant/members", nil, session, member.TenantID)
	requireStatus(t, response, 200)
	_ = json.Unmarshal(response.Body.Bytes(), &envelope)
	if envelope.Data.Items[0].Online {
		t.Fatal("过期在线租约仍显示在线")
	}
}

// TestServerMonitoringRequiresLivePlatformRole 验证真实Cookie的租户所有者无权读取监控，平台角色撤销立即生效。
func TestServerMonitoringRequiresLivePlatformRole(t *testing.T) {
	f := identityIntegration(t)
	account, session := f.register(t, "monitor-admin@example.com")
	member := account.Memberships[0]
	service := monitoring.New(f.pool, f.service, func() map[string]int64 { return map[string]int64{"connections": 2, "online_people": 1} })
	service.Register(f.mux)
	requireStatus(t, f.call("GET", "/api/v1/platform/monitoring", nil, session, member.TenantID), 403)
	input := map[string]any{"tested_people": 1001, "safety_factor": 0.7, "test_date": "2026-10-28", "notes": "演示数据：成员混合真实文字消息场景的手工压测记录"}
	requireStatus(t, f.call("PATCH", "/api/v1/platform/monitoring/capacity", input, session, member.TenantID), 403)
	requireStatus(t, f.call("DELETE", "/api/v1/platform/monitoring/capacity", nil, session, member.TenantID), 403)
	if _, err := f.pool.Exec(context.Background(), "UPDATE users SET is_platform_admin=true WHERE id=$1", account.User.ID); err != nil {
		t.Fatal(err)
	}
	response := f.call("GET", "/api/v1/platform/monitoring", nil, session, member.TenantID)
	requireStatus(t, response, 200)
	var reply struct {
		Data struct {
			Capacity struct {
				Status    string `json:"status"`
				Estimated *int   `json:"estimated_additional_people"`
			} `json:"capacity"`
			Resources struct {
				Scope string `json:"scope"`
			} `json:"resources"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &reply) != nil || reply.Data.Capacity.Status != "uncalibrated" || reply.Data.Capacity.Estimated != nil || reply.Data.Resources.Scope == "" {
		t.Fatal("监控不应伪造未压测容量或资源范围")
	}
	response = f.call("PATCH", "/api/v1/platform/monitoring/capacity", input, session, member.TenantID)
	requireStatus(t, response, 200)
	var capacityReply struct {
		Data struct {
			Status     string `json:"status"`
			Safe       int    `json:"safe_online_people"`
			Additional *int   `json:"estimated_additional_people"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &capacityReply) != nil || capacityReply.Data.Status != "calibrated" || capacityReply.Data.Safe != 700 || capacityReply.Data.Additional == nil || *capacityReply.Data.Additional != 699 {
		t.Fatal("真实人工基线保存或余量公式错误")
	}
	input["safety_factor"] = 1.0
	requireStatus(t, f.call("PATCH", "/api/v1/platform/monitoring/capacity", input, session, member.TenantID), 400)
	input["safety_factor"] = 0.7
	if _, err := f.pool.Exec(context.Background(), "UPDATE platform_capacity_baseline SET resource_fingerprint=repeat('0',64)"); err != nil {
		t.Fatal(err)
	}
	response = f.call("GET", "/api/v1/platform/monitoring/capacity", nil, session, member.TenantID)
	requireStatus(t, response, 200)
	_ = json.Unmarshal(response.Body.Bytes(), &capacityReply)
	if capacityReply.Data.Status != "stale" || capacityReply.Data.Additional != nil {
		t.Fatal("规格不一致时不能输出估算人数")
	}
	response = f.call("DELETE", "/api/v1/platform/monitoring/capacity", nil, session, member.TenantID)
	requireStatus(t, response, 200)
	_ = json.Unmarshal(response.Body.Bytes(), &capacityReply)
	if capacityReply.Data.Status != "uncalibrated" {
		t.Fatal("删除演示基线后未恢复待校准")
	}
	if _, err := f.pool.Exec(context.Background(), "UPDATE users SET is_platform_admin=false WHERE id=$1", account.User.ID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.call("GET", "/api/v1/platform/monitoring", nil, session, member.TenantID), 403)
}
