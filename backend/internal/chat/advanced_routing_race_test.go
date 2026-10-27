// 文件作用说明：用真实PostgreSQL竞争验证跨池统一容量、确认接单及转接资格变更，确保高级规则不能绕过基础接待约束。
package chat

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestWeightedRoutingCrossPoolCapacity 验证两个加权池共享同一客服时，16个并发入口只能占用一个容量且失败分配不推进积分。
func TestWeightedRoutingCrossPoolCapacity(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	f.routingPatch("routing-pools/"+f.poolID, map[string]any{"algorithm": "weighted_round_robin", "members_mode": "fixed", "member_ids": []string{f.agent}, "member_weights": map[string]int{f.agent: 7}})
	var groupID string
	if err := f.admin.QueryRow(context.Background(), "SELECT service_group_id::text FROM routing_pools WHERE id=$1", f.poolID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	pool := f.createRoutingResource("routing-pools", map[string]any{"name": "共享客服第二池", "service_group_id": groupID, "algorithm": "weighted_round_robin", "members_mode": "fixed", "member_ids": []string{f.agent}, "member_weights": map[string]int{f.agent: 3}})
	link := f.createRoutingResource("access-links", map[string]any{"name": "第二加权入口", "routing_pool_id": pool["id"], "service_group_id": groupID})
	entries := []string{f.entry, link["entry_key"].(string)}
	start := make(chan struct{})
	statuses := make(chan int, 16)
	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			code, _ := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": entries[index%2], "visit_id": newID()})
			statuses <- code
		}(index)
	}
	close(start)
	group.Wait()
	close(statuses)
	for code := range statuses {
		if code != 200 {
			t.Fatalf("容量竞争应安全排队而非请求错误: %d", code)
		}
	}
	if f.countRows("SELECT count(*) FROM conversations WHERE status='active'") != 1 || f.countRows("SELECT count(*) FROM conversations WHERE status='queued'") != 15 || f.countRows("SELECT count(*) FROM agent_capacity_holds WHERE status!='released'") != 1 {
		t.Fatal("跨池分配出现超卖、漏排队或重复占位")
	}
	if f.countRows("SELECT current_load FROM agent_runtime WHERE membership_id=$1", f.agent) != 1 || f.countRows("SELECT count(*) FROM routing_weight_state") != 1 || f.countRows("SELECT count(*) FROM conversation_assignments") != 1 {
		t.Fatal("只有成功分配的池可以推进积分和容量")
	}
}

// TestAdvancedRulesRecheckedOnConfirmation 验证已预留名额在确认和转接时仍受当前必备技能限制，恢复资格后不重复占位或推进算法。
func TestAdvancedRulesRecheckedOnConfirmation(t *testing.T) {
	f := newFixture(t)
	f.online(f.owner)
	f.online(f.agent)
	skill := f.createRoutingResource("routing-skills", map[string]any{"name": "确认资格"})["id"].(string)
	f.routingPatch("routing-members/"+f.agent, map[string]any{"skill_ids": []string{skill}})
	f.routingPatch("routing-pools/"+f.poolID, map[string]any{"algorithm": "weighted_round_robin", "assignment_mode": "confirm", "members_mode": "fixed", "member_ids": []string{f.owner, f.agent}, "required_skill_ids": []string{skill}})
	_, _, conversation := f.visitor("")
	base := "/api/v1/conversations/" + conversation + "/assignments/"
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='assigned' AND current_agent_id=$2", conversation, f.agent) != 1 {
		t.Fatal("首个合格客服应得到待确认预留")
	}
	f.routingPatch("routing-members/"+f.agent, map[string]any{"skill_ids": []string{}})
	status, reply := f.request("POST", base+"accept", f.agent, "", map[string]any{})
	if status != 409 {
		t.Fatalf("预留期间失去技能不能确认接待: %d %v", status, reply["code"])
	}
	f.routingPatch("routing-members/"+f.agent, map[string]any{"skill_ids": []string{skill}})
	status, reply = f.request("POST", base+"accept", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", base+"transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	if status != 409 {
		t.Fatalf("转接目标必须满足同一技能资格: %d %v", status, reply["code"])
	}
	f.routingPatch("routing-members/"+f.owner, map[string]any{"skill_ids": []string{skill}})
	status, reply = f.request("POST", base+"transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	mustData(t, status, reply)
	var request string
	if err := f.admin.QueryRow(context.Background(), "SELECT id::text FROM transfer_requests WHERE conversation_id=$1 AND status='pending'", conversation).Scan(&request); err != nil {
		t.Fatal(err)
	}
	f.routingPatch("routing-members/"+f.owner, map[string]any{"skill_ids": []string{}})
	status, reply = f.request("POST", base+"accept", f.owner, "", map[string]any{"transfer_request_id": request})
	if status != 409 {
		t.Fatalf("转接预留期间资格变更必须阻止确认: %d %v", status, reply["code"])
	}
	f.routingPatch("routing-members/"+f.owner, map[string]any{"skill_ids": []string{skill}})
	status, reply = f.request("POST", base+"accept", f.owner, "", map[string]any{"transfer_request_id": request})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM agent_capacity_holds WHERE status!='released'") != 1 || f.countRows("SELECT current_load FROM agent_runtime WHERE membership_id=$1", f.owner) != 1 || f.countRows("SELECT current_load FROM agent_runtime WHERE membership_id=$1", f.agent) != 0 || f.countRows("SELECT count(*) FROM routing_weight_state") != 1 {
		t.Fatal("确认或转接重复计入容量或加权积分")
	}
}

// waitRoutingLocks 用数据库实际等待状态建立测试屏障，避免依赖调度快慢来碰运气复现交叉锁。
func (f *fixture) waitRoutingLocks(count int) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock'", f.schema).Scan(&waiting); err != nil {
			f.t.Fatal(err)
		}
		if waiting >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatal("未能建立路由锁等待屏障")
}

// TestQueuedRoutingReleasesCapacityBeforeNextPool 复现访客先等池B、队列先分配池A后等池B的交叉时序，验证调度不会持有共享容量反向等待。
func TestQueuedRoutingReleasesCapacityBeforeNextPool(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var groupID string
	if err := f.admin.QueryRow(ctx, "SELECT service_group_id::text FROM routing_pools WHERE id=$1", f.poolID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	f.routingPatch("routing-pools/"+f.poolID, map[string]any{"algorithm": "weighted_round_robin", "members_mode": "fixed", "member_ids": []string{f.agent}})
	pool := f.createRoutingResource("routing-pools", map[string]any{"name": "队列并发池", "service_group_id": groupID, "algorithm": "weighted_round_robin", "members_mode": "fixed", "member_ids": []string{f.agent}})
	link := f.createRoutingResource("access-links", map[string]any{"name": "队列并发入口", "routing_pool_id": pool["id"], "service_group_id": groupID})
	higher, entry := f.poolID, f.entry
	if pool["id"].(string) > higher {
		higher, entry = pool["id"].(string), link["entry_key"].(string)
	}
	for _, key := range []string{f.entry, link["entry_key"].(string)} {
		status, reply := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": key, "visit_id": newID()})
		mustData(t, status, reply)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET capacity=2 WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	f.online(f.agent)
	hold, err := f.admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(context.Background())
	if _, err := hold.Exec(ctx, "SELECT id FROM routing_pools WHERE id=$1 FOR UPDATE", higher); err != nil {
		t.Fatal(err)
	}
	visitorDone, queueDone := make(chan int, 1), make(chan error, 1)
	go func() {
		status, _ := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": entry, "visit_id": newID()})
		visitorDone <- status
	}()
	f.waitRoutingLocks(1)
	go func() { queueDone <- f.s.routeQueues(ctx, f.tenant) }()
	f.waitRoutingLocks(2)
	if err := hold.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if status := <-visitorDone; status != 200 {
		t.Fatalf("访客与排队调度交叉等待不应发生死锁: %d", status)
	}
	if err := <-queueDone; err != nil {
		t.Fatalf("跨池队列调度应完成独立提交: %v", err)
	}
	if f.countRows("SELECT current_load FROM agent_runtime WHERE membership_id=$1", f.agent) != 2 || f.countRows("SELECT count(*) FROM agent_capacity_holds WHERE status!='released'") != 2 || f.countRows("SELECT count(*) FROM conversation_assignments") != 2 {
		t.Fatal("队列与访客并发分配后容量、占位及接待历史不一致")
	}
}
