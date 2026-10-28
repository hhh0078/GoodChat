// 文件作用说明：通过真实访问与隔离PostgreSQL验证链接UV/PV去重、跨入口复访和租户日界线。
package chat

import "testing"

// TestLinkStatisticsUseActualEntryAndTenantDay 验证跨入口复访不被咨询创建入口吞掉，重试和午夜边界不多计。
func TestLinkStatisticsUseActualEntryAndTenantDay(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a := f.createRoutingResource("access-links", map[string]any{"name": "统计入口甲", "reception_mode": "manual"})
	b := f.createRoutingResource("access-links", map[string]any{"name": "统计入口乙", "reception_mode": "manual"})
	visits := []string{newID(), newID(), newID(), newID()}
	status, reply := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": a["entry_key"], "visit_id": visits[0]})
	first := mustData(t, status, reply)
	token := first["token"].(string)
	for i := 0; i < 3; i++ {
		status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": a["entry_key"], "visit_id": visits[1], "visitor_token": token})
		mustData(t, status, reply)
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": b["entry_key"], "visit_id": visits[2], "visitor_token": token})
	cross := mustData(t, status, reply)
	firstID := first["conversation"].(map[string]any)["id"].(string)
	crossID := cross["conversation"].(map[string]any)["id"].(string)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": b["entry_key"], "visit_id": visits[3]})
	second := mustData(t, status, reply)
	if _, err := f.admin.Exec(ctx, "UPDATE tenants SET timezone='Asia/Shanghai' WHERE id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	for i, at := range []string{"2026-10-25T15:59:59Z", "2026-10-25T16:00:00Z", "2026-10-26T15:59:59Z", "2026-10-26T16:00:00Z"} {
		if _, err := f.admin.Exec(ctx, "UPDATE contact_visits SET created_at=$2 WHERE event_id=$1", visits[i], at); err != nil {
			t.Fatal(err)
		}
	}
	for id, at := range map[string]string{first["conversation"].(map[string]any)["id"].(string): "2026-10-25T15:59:59Z", second["conversation"].(map[string]any)["id"].(string): "2026-10-26T16:00:00Z"} {
		if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at=$2 WHERE id=$1", id, at); err != nil {
			t.Fatal(err)
		}
	}
	if crossID != firstID {
		if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at=$2 WHERE id=$1", crossID, "2026-10-26T15:59:59Z"); err != nil {
			t.Fatal(err)
		}
	}
	status, reply = f.request("GET", "/api/v1/tenant/link-statistics?from=2026-10-26&to=2026-10-26", f.owner, "", nil)
	data := mustData(t, status, reply)
	if data["timezone"] != "Asia/Shanghai" {
		t.Fatal("链接统计未采用租户时区")
	}
	found := 0
	for _, raw := range data["items"].([]any) {
		item := raw.(map[string]any)
		if item["id"] != a["id"] && item["id"] != b["id"] {
			continue
		}
		found++
		wantNew := float64(0)
		if item["id"] == b["id"] && crossID != firstID {
			wantNew = 1
		}
		if item["visits"] != float64(1) || item["visitors"] != float64(1) || item["new_conversations"] != wantNew {
			t.Fatal("实际访问入口、重试或日界线统计错误", item)
		}
	}
	if found != 2 {
		t.Fatal("链接批量统计缺少入口")
	}
	status, _ = f.request("GET", "/api/v1/tenant/link-statistics", f.agent, "", nil)
	if status != 403 {
		t.Fatal("普通客服不应读取管理员链接统计")
	}
	status, reply = f.request("GET", "/api/v1/tenant/link-statistics", f.otherOwner, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		if raw.(map[string]any)["id"] == a["id"] {
			t.Fatal("链接统计泄漏其他租户")
		}
	}
}
