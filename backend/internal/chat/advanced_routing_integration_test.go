// 文件作用说明：通过真实HTTP与PostgreSQL验证加权比例、技能组合、历史优先和严格指定规则，以及并发容量与重试幂等。
package chat

import (
	"context"
	"sync"
	"testing"
)

// createRoutingResource 通过管理员真实创建接口取得路由配置，拒绝把前端状态当成持久化结果。
func (f *fixture) createRoutingResource(path string, input any) map[string]any {
	f.t.Helper()
	status, reply := f.request("POST", "/api/v1/tenant/"+path, f.owner, "", input)
	if status != 201 {
		f.t.Fatalf("创建%s失败: %d %v", path, status, reply["code"])
	}
	return reply["data"].(map[string]any)
}

// routingPatch 保存真实路由配置并返回服务端读回值。
func (f *fixture) routingPatch(path string, input any) map[string]any {
	f.t.Helper()
	status, reply := f.request("PATCH", "/api/v1/tenant/"+path, f.owner, "", input)
	return mustData(f.t, status, reply)
}

// TestWeightedRoutingPersistsRatioAndRetry 验证权重1比3在12次实际分配中得到3比9，重放当前分配不推进积分、不重复占位。
func TestWeightedRoutingPersistsRatioAndRetry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.owner)
	f.online(f.agent)
	f.routingPatch("routing-pools/"+f.poolID, map[string]any{"algorithm": "weighted_round_robin", "members_mode": "fixed", "member_ids": []string{f.owner, f.agent}, "member_weights": map[string]int{f.owner: 1, f.agent: 3}})
	counts := map[string]int{}
	var token, last, member string
	for index := 0; index < 12; index++ {
		token, _, last = f.visitor("")
		var status string
		if err := f.admin.QueryRow(ctx, "SELECT current_agent_id::text,status FROM conversations WHERE id=$1", last).Scan(&member, &status); err != nil {
			t.Fatal(err)
		}
		if status != "active" {
			t.Fatal("合格客服应被真实自动分配")
		}
		counts[member]++
		if index < 11 {
			status, reply := f.request("POST", "/api/v1/conversations/"+last+"/close", "", token, map[string]any{})
			mustData(t, status, reply)
		}
	}
	if counts[f.owner] != 3 || counts[f.agent] != 9 {
		t.Fatalf("平滑加权实际比例错误: %+v", counts)
	}
	var before, after map[string]int64
	if err := f.admin.QueryRow(ctx, "SELECT jsonb_object_agg(membership_id::text,current_weight) FROM routing_weight_state WHERE routing_pool_id=$1", f.poolID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	codes := make(chan int, 12)
	for index := 0; index < 12; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			status, _ := f.request("POST", "/api/v1/conversations/"+last+"/assignments/claim", member, "", map[string]any{}, map[string]string{"Idempotency-Key": "weighted-retry"})
			codes <- status
		}()
	}
	group.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("重放当前分配应幂等: %d", code)
		}
	}
	if err := f.admin.QueryRow(ctx, "SELECT jsonb_object_agg(membership_id::text,current_weight) FROM routing_weight_state WHERE routing_pool_id=$1", f.poolID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before[f.owner] != after[f.owner] || before[f.agent] != after[f.agent] || f.countRows("SELECT count(*) FROM conversation_assignments WHERE conversation_id=$1", last) != 1 {
		t.Fatal("重放分配推进了加权积分或重复接待")
	}
	if f.countRows("SELECT count(*) FROM conversation_assignments WHERE decision->>'algorithm'='weighted_round_robin' AND decision->>'reason'='algorithm'") != 12 {
		t.Fatal("加权实际决策没有保存快照")
	}
	if f.countRows("SELECT count(*) FROM agent_capacity_holds WHERE status!='released'") != 1 {
		t.Fatal("结束旧咨询后应只保留最后一份占位")
	}
}

// TestAdvancedSkillsHardScope 验证池和入口必备技能叠加、可选技能排序、跨租户引用拒绝、归档必备技能不放宽已保存规则。
func TestAdvancedSkillsHardScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.owner)
	f.online(f.agent)
	required := f.createRoutingResource("routing-skills", map[string]any{"name": "售后资格"})["id"].(string)
	optional := f.createRoutingResource("routing-skills", map[string]any{"name": "设备经验"})["id"].(string)
	linkSkill := f.createRoutingResource("routing-skills", map[string]any{"name": "入口专项"})["id"].(string)
	f.routingPatch("routing-members/"+f.owner, map[string]any{"skill_ids": []string{required}})
	f.routingPatch("routing-members/"+f.agent, map[string]any{"skill_ids": []string{required, optional}})
	f.routingPatch("routing-pools/"+f.poolID, map[string]any{"algorithm": "least_load", "members_mode": "fixed", "member_ids": []string{f.owner, f.agent}, "required_skill_ids": []string{required}, "optional_skill_ids": []string{optional}})
	token, _, conversation := f.visitor("")
	var chosen string
	if err := f.admin.QueryRow(ctx, "SELECT current_agent_id::text FROM conversations WHERE id=$1", conversation).Scan(&chosen); err != nil || chosen != f.agent {
		t.Fatalf("可选技能应在主算法前优先排序: %s %v", chosen, err)
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/entries/"+f.entry, "", "", nil)
	entry := mustData(t, status, reply)["id"].(string)
	f.routingPatch("access-links/"+entry, map[string]any{"required_skill_ids": []string{linkSkill}})
	_, _, waiting := f.visitor("")
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='queued'", waiting) != 1 {
		t.Fatal("入口必备技能不能被池可选技能绕过")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+waiting+"/assignments/claim", f.agent, "", map[string]any{})
	if status != 409 {
		t.Fatalf("人工领取同样必须满足必备技能: %d %v", status, reply["code"])
	}
	f.routingPatch("routing-members/"+f.owner, map[string]any{"skill_ids": []string{required, linkSkill}})
	_, _, qualified := f.visitor("")
	if err := f.admin.QueryRow(ctx, "SELECT current_agent_id::text FROM conversations WHERE id=$1", qualified).Scan(&chosen); err != nil || chosen != f.owner {
		t.Fatalf("池和入口全部必备技能应由同一合格客服满足: %s %v", chosen, err)
	}
	status, reply = f.request("POST", "/api/v1/tenant/routing-skills", f.otherOwner, "", map[string]any{"name": "其他租户技能"})
	if status != 201 {
		t.Fatal("无法建立隔离租户技能")
	}
	foreign := reply["data"].(map[string]any)["id"].(string)
	for _, path := range []string{"routing-members/" + f.agent, "routing-pools/" + f.poolID, "access-links/" + entry} {
		field := "required_skill_ids"
		if path == "routing-members/"+f.agent {
			field = "skill_ids"
		}
		status, reply = f.request("PATCH", "/api/v1/tenant/"+path, f.owner, "", map[string]any{field: []string{foreign}})
		if status != 400 {
			t.Fatalf("跨租户技能引用必须拒绝: %s %d %v", path, status, reply["code"])
		}
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/routing-pools/"+f.poolID, f.owner, "", map[string]any{"member_weights": map[string]int{f.otherOwner: 10}})
	if status != 400 {
		t.Fatalf("跨租户成员权重必须拒绝: %d %v", status, reply["code"])
	}
	status, reply = f.request("DELETE", "/api/v1/tenant/routing-skills/"+required, f.owner, "", nil)
	mustData(t, status, reply)
	_, _, blocked := f.visitor("")
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='queued'", blocked) != 1 || f.countRows("SELECT count(*) FROM routing_pool_skills WHERE routing_pool_id=$1 AND skill_id=$2 AND is_required", f.poolID, required) != 1 {
		t.Fatal("归档必备技能不能偷偷删除旧约束并放宽路由")
	}
	var visible int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM routing_skills").Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("技能缺失租户上下文不应可读: %d %v", visible, err)
	}
}

// TestHistoryPreferenceAndStrictTarget 验证历史接待在仍合格时优先，示忙时回主算法，严格指定和公开参数不能被历史或人工领取放宽。
func TestHistoryPreferenceAndStrictTarget(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.owner)
	f.online(f.agent)
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET capacity=10 WHERE tenant_id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	status, reply := f.request("GET", "/api/v1/entries/"+f.entry, "", "", nil)
	entry := mustData(t, status, reply)["id"].(string)
	f.routingPatch("routing-pools/"+f.poolID, map[string]any{"algorithm": "least_load", "history_priority": true})
	f.routingPatch("access-links/"+entry, map[string]any{"agent_id": f.agent, "agent_mode": "prefer"})
	token, _, first := f.visitor("")
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	f.visitor("") // 为历史客服建立另一份真实接待，使最少会话算法本来会选择空闲的所有者。
	f.routingPatch("access-links/"+entry, map[string]any{"agent_id": nil})
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	next := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND current_agent_id=$2", next, f.agent) != 1 || f.countRows("SELECT count(*) FROM conversation_assignments WHERE conversation_id=$1 AND decision->>'reason'='history'", next) != 1 {
		t.Fatal("合格的历史客服应优先于最少负载主算法")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+next+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("PATCH", "/api/v1/agent/status", f.agent, "", map[string]any{"busy": true})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	fallback := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND current_agent_id=$2", fallback, f.owner) != 1 {
		t.Fatal("历史客服示忙时必须回到合格候选主算法")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+fallback+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	f.online(f.agent)
	status, reply = f.request("PATCH", "/api/v1/agent/status", f.owner, "", map[string]any{"busy": true})
	mustData(t, status, reply)
	f.routingPatch("access-links/"+entry, map[string]any{"agent_id": f.owner, "agent_mode": "strict", "allow_routing_override": true})
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	if status != 409 || reply["code"] != "candidate_unavailable" {
		t.Fatal("指定客服忙碌应明确拒绝，不得排队或改派")
	}
	f.online(f.owner)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	strict := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='active' AND current_agent_id=$2", strict, f.owner) != 1 {
		t.Fatal("指定客服恢复合格后必须优先接待")
	}

	status, reply = f.request("POST", "/api/v1/conversations/"+strict+"/assignments/claim", f.agent, "", map[string]any{})
	if status != 409 && status != 404 {
		t.Fatalf("人工领取不能放宽严格指定: %d %v", status, reply["code"])
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visit_id": newID(), "routing": map[string]string{"agent_mode": "prefer"}})
	if status != 400 {
		t.Fatalf("公开参数不能放宽严格指定: %d %v", status, reply["code"])
	}
}
