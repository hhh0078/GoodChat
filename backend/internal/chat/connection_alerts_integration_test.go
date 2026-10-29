// 文件作用说明：通过真实HTTP与WS验证上班断线提醒、示忙成员、多连接恢复和租户可见范围。
package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// readConnectionAlerts 从真实告警接口读取成员列表，不以HTTP上下班租约冒充WS探活。
func readConnectionAlerts(t *testing.T, f *fixture, member string) []any {
	t.Helper()
	status, reply := f.request("GET", "/api/v1/tenant/connection-alerts", member, "", nil)
	data := mustData(t, status, reply)
	if data["probe_timeout_seconds"] != float64(45) {
		t.Fatal("告警没有标明真实探活窗口")
	}
	return data["items"].([]any)
}

// openAlertConnection 使用独立一次性票据建立真实WS，等待ready后才视为探活成功。
func openAlertConnection(t *testing.T, f *fixture, member string) *websocket.Conn {
	t.Helper()
	status, reply := f.request("POST", "/api/v1/realtime/ticket", member, "", map[string]any{})
	ticket := mustData(t, status, reply)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+ticket["url"].(string), nil)
	if err != nil {
		t.Fatal("告警测试无法建立真实WS")
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	_, raw, err := conn.Read(ctx)
	var frame map[string]any
	if err != nil || json.Unmarshal(raw, &frame) != nil || frame["type"] != "ready" {
		t.Fatal("告警测试WS未完成就绪探活")
	}
	return conn
}

// TestConnectionAlertsRequireRealWSPresence 验证HTTP状态续租不掩盖断线、忙碌也提醒、普通客服和其他租户不泄露团队异常。
func TestConnectionAlertsRequireRealWSPresence(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	f.online(f.owner)
	f.online(f.otherOwner)
	status, reply := f.request("PATCH", "/api/v1/agent/status", f.agent, "", map[string]any{"busy": true})
	mustData(t, status, reply)
	items := readConnectionAlerts(t, f, f.owner)
	if len(items) != 2 {
		t.Fatal("HTTP上班续租掩盖了没有WS的成员，或跨租户误计")
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["last_seen_at"] != nil || item["offline_since"] != nil {
			t.Fatal("从未连接的成员被伪造探活时间")
		}
		if item["id"] == f.agent && item["busy"] != true {
			t.Fatal("示忙断线成员没有被保留")
		}
	}
	self := readConnectionAlerts(t, f, f.agent)
	if len(self) != 1 || self[0].(map[string]any)["id"] != f.agent {
		t.Fatal("普通客服看到了其他成员异常")
	}
	other := readConnectionAlerts(t, f, f.otherOwner)
	if len(other) != 1 || other[0].(map[string]any)["id"] != f.otherOwner {
		t.Fatal("告警跨租户泄露")
	}
	status, reply = f.request("PATCH", "/api/v1/agent/status", f.owner, "", map[string]any{"available": false})
	mustData(t, status, reply)
	items = readConnectionAlerts(t, f, f.owner)
	if len(items) != 1 || items[0].(map[string]any)["id"] != f.agent {
		t.Fatal("下班成员不应出现断线告警")
	}
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	if len(readConnectionAlerts(t, f, f.owner)) != 0 {
		t.Fatal("停用成员不应出现接待异常")
	}
	status, _ = f.request("GET", "/api/v1/tenant/connection-alerts", f.agent, "", nil)
	if status != 403 {
		t.Fatal("失效成员仍可查询告警")
	}
}

// TestConnectionAlertsRecoverAndMergeTabs 验证重连即时解除告警、旧租约不会压过活跃连接，关闭一个标签不提前使其他标签离线。
func TestConnectionAlertsRecoverAndMergeTabs(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	ctx := t.Context()
	if _, err := f.admin.Exec(ctx, `INSERT INTO member_presence_intervals(tenant_id,membership_id,started_at,last_seen_at,online_until) VALUES($1,$2,now()-interval '2 minutes',now()-interval '1 minute',now()-interval '15 seconds')`, f.tenant, f.agent); err != nil {
		t.Fatal(err)
	}
	if len(readConnectionAlerts(t, f, f.owner)) != 1 {
		t.Fatal("过期WS租约没有告警")
	}
	first := openAlertConnection(t, f, f.agent)
	if len(readConnectionAlerts(t, f, f.owner)) != 0 {
		t.Fatal("真实重连没有立即清除异常，或旧租约覆盖活跃连接")
	}
	second := openAlertConnection(t, f, f.agent)
	_ = first.CloseNow()
	if len(readConnectionAlerts(t, f, f.owner)) != 0 {
		t.Fatal("关闭单标签提前使多标签成员离线")
	}
	_ = second.CloseNow()
	if _, err := f.admin.Exec(ctx, `UPDATE member_presence_intervals SET started_at=now()-interval '2 minutes',last_seen_at=now()-interval '1 minute',online_until=now()-interval '15 seconds' WHERE tenant_id=$1 AND membership_id=$2`, f.tenant, f.agent); err != nil {
		t.Fatal(err)
	}
	items := readConnectionAlerts(t, f, f.owner)
	if len(items) != 1 || items[0].(map[string]any)["offline_since"] == nil {
		t.Fatal("所有连接租约过期后未恢复告警时间")
	}
}
