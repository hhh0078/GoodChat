// 文件作用说明：在隔离PostgreSQL和真实HTTP验证链接混合接待、领取原因、渠道快照与复访业务参数。
package chat

import (
	"context"
	"strings"
	"testing"
)

// TestLinkReceptionMixedQueue 验证同池手领队头不阻塞自动入口、容量限制保留以及多标签主渠道兼容。
func TestLinkReceptionMixedQueue(t *testing.T) {
	f := newFixture(t)
	manual := f.createRoutingResource("access-links", map[string]any{"name": "人工入口", "reception_mode": "manual", "channel_code": "website", "channel_tags": []string{"官网", "售前"}})
	auto := f.createRoutingResource("access-links", map[string]any{"name": "自动入口", "reception_mode": "auto"})
	status, reply := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": manual["entry_key"], "visit_id": newID()})
	first := mustData(t, status, reply)["conversation"].(map[string]any)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": auto["entry_key"], "visit_id": newID()})
	secondSession := mustData(t, status, reply)
	second := secondSession["conversation"].(map[string]any)
	f.online(f.agent)
	if err := f.s.routeQueues(context.Background(), f.tenant); err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='active' AND current_agent_id=$2", second["id"], f.agent) != 1 {
		t.Fatal("人工队头阻塞了后续自动分配")
	}
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='queued'", first["id"]) != 1 {
		t.Fatal("人工入口不应自动分配")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+first["id"].(string)+"/assignments/claim", f.agent, "", map[string]any{})
	if status != 409 || !strings.Contains(reply["message"].(string), "容量已满") {
		t.Fatalf("应明确容量原因:%d %v", status, reply["message"])
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+second["id"].(string)+"/close", "", secondSession["token"].(string), map[string]any{})
	mustData(t, status, reply)
	f.routingPatch("routing-pools/"+f.poolID, map[string]any{"assignment_mode": "confirm"})
	status, reply = f.request("POST", "/api/v1/conversations/"+first["id"].(string)+"/assignments/claim", f.agent, "", map[string]any{})
	claimed := mustData(t, status, reply)
	if claimed["status"] != "active" {
		t.Fatal("新手领入口应一次领取成功，无需重复确认")
	}
	var primary string
	var tags []string
	if err := f.admin.QueryRow(context.Background(), "SELECT source_snapshot->'tracking'->>'channel_code',source_snapshot->'entry'->'channel_tags' FROM conversations WHERE id=$1", first["id"]).Scan(&primary, &tags); err != nil {
		t.Fatal(err)
	}
	if primary != "website" || strings.Join(tags, ",") != "官网,售前" {
		t.Fatal("主渠道或多标签快照未保存")
	}
}

// TestCandidateFailureDiagnostics 验证上班、忙碌、租约和必备技能失败均返回可操作的具体原因。
func TestCandidateFailureDiagnostics(t *testing.T) {
	f := newFixture(t)
	_, _, conversation := f.visitor("")
	path := "/api/v1/conversations/" + conversation + "/assignments/claim"
	status, reply := f.request("POST", path, f.agent, "", map[string]any{})
	if status != 409 || !strings.Contains(reply["message"].(string), "下班") {
		t.Fatalf("缺少下班诊断:%d %v", status, reply["message"])
	}
	f.online(f.agent)
	status, reply = f.request("PATCH", "/api/v1/agent/status", f.agent, "", map[string]any{"busy": true})
	mustData(t, status, reply)
	status, reply = f.request("POST", path, f.agent, "", map[string]any{})
	if status != 409 || !strings.Contains(reply["message"].(string), "忙碌") {
		t.Fatalf("缺少忙碌诊断:%d %v", status, reply["message"])
	}
	f.online(f.agent)
	if _, err := f.admin.Exec(context.Background(), "UPDATE agent_runtime SET online_until=now()-interval '1 second' WHERE membership_id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", path, f.agent, "", map[string]any{})
	if status != 409 || !strings.Contains(reply["message"].(string), "连接已离线") {
		t.Fatalf("缺少离线诊断:%d %v", status, reply["message"])
	}
	f.online(f.agent)
	skill := f.createRoutingResource("routing-skills", map[string]any{"name": "售后专项"})
	f.routingPatch("routing-pools/"+f.poolID, map[string]any{"required_skill_ids": []string{skill["id"].(string)}})
	status, reply = f.request("POST", path, f.agent, "", map[string]any{})
	if status != 409 || !strings.Contains(reply["message"].(string), "售后专项") {
		t.Fatalf("缺少技能诊断:%d %v", status, reply["message"])
	}
}

// TestVisitorLatestContextPreservesSource 验证复访业务字段更新到资料上下文而入口来源快照保持不变。
func TestVisitorLatestContextPreservesSource(t *testing.T) {
	f := newFixture(t)
	token, _, conversation := f.visitor("")
	for _, data := range []map[string]any{{"有无房产": "无", "次数": 0}, {"有无房产": "有", "已核对": false}} {
		status, reply := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID(), "context": data})
		current := mustData(t, status, reply)["conversation"].(map[string]any)
		if current["id"] != conversation {
			t.Fatal("复访不应创建另一咨询")
		}
	}
	var data, source map[string]any
	if err := f.admin.QueryRow(context.Background(), "SELECT context,source_snapshot FROM conversations WHERE id=$1", conversation).Scan(&data, &source); err != nil {
		t.Fatal(err)
	}
	if data["有无房产"] != "有" || data["次数"] != float64(0) || data["已核对"] != false {
		t.Fatal("复访资料未合并或丢失零布尔值")
	}
	if source["entry"].(map[string]any)["entry_key"] != f.entry {
		t.Fatal("复访破坏了原入口来源快照")
	}
}

// TestLinkReceptionSnapshotAndLegacy 验证旧入口继承池配置，而新咨询固定创建时的手领方式。
func TestLinkReceptionSnapshotAndLegacy(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	_, _, legacy := f.visitor("")
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='queued'", legacy) != 1 {
		t.Fatal("旧入口应继承人工池，不能被默认自动接待")
	}
	link := f.createRoutingResource("access-links", map[string]any{"name": "快照人工入口", "reception_mode": "manual"})
	status, reply := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": link["entry_key"], "visit_id": newID()})
	conversation := mustData(t, status, reply)["conversation"].(map[string]any)["id"]
	f.routingPatch("access-links/"+link["id"].(string), map[string]any{"reception_mode": "auto"})
	if err := f.s.routeQueues(context.Background(), f.tenant); err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='queued' AND source_snapshot->'entry'->>'reception_mode'='manual'", conversation) != 1 {
		t.Fatal("更改入口不应改写已有手领咨询")
	}
}

// TestVisitorRetryCannotOverwriteNewerContext 验证旧访问重试不会覆盖较新访问的资料更新。
func TestVisitorRetryCannotOverwriteNewerContext(t *testing.T) {
	f := newFixture(t)
	token, _, conversation := f.visitor("")
	oldVisit := newID()
	first := map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": oldVisit, "context": map[string]any{"阶段": "初次"}}
	status, reply := f.request("POST", "/api/v1/visitor/session", "", "", first)
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID(), "context": map[string]any{"阶段": "已更新"}})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", first)
	current := mustData(t, status, reply)["conversation"].(map[string]any)
	if current["id"] != conversation || current["context"].(map[string]any)["阶段"] != "已更新" {
		t.Fatal("旧访问重试覆盖了较新资料")
	}
}
