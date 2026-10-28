// 文件作用说明：在真实PostgreSQL和HTTP验证成员接待段首响、等待段平均回复、转接评价与租约区间统计。
package chat

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// statisticsMember 从真实报表接口读取指定成员，不用前端自行合计代替服务端口径。
func statisticsMember(t *testing.T, f *fixture, member string) map[string]any {
	t.Helper()
	status, reply := f.request("GET", "/api/v1/tenant/member-statistics?from=2026-10-26&to=2026-10-26", f.owner, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == member {
			return item
		}
	}
	t.Fatal("没有找到目标成员统计")
	return nil
}

// statisticsMessage 保存真实消息并返回编号，测试只调整隔离数据时间以覆盖精确统计边界。
func statisticsMessage(t *testing.T, f *fixture, conversation, member, token, text, audience string) string {
	t.Helper()
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", member, token, map[string]any{"client_message_id": newID(), "content": text, "audience": audience})
	return mustData(t, status, reply)["id"].(string)
}

// TestMemberStatisticsFollowSegmentsAndWaitEpisodes 验证连续访客问题只计一个等待段，转接首响与评价不重复归属。
func TestMemberStatisticsFollowSegmentsAndWaitEpisodes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	f.online(f.owner)
	token, _, conversation := f.visitor("")
	q1 := statisticsMessage(t, f, conversation, "", token, "首个问题", "public")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	a1 := statisticsMessage(t, f, conversation, f.agent, "", "第一段首答", "public")
	q2 := statisticsMessage(t, f, conversation, "", token, "第二个问题", "public")
	q3 := statisticsMessage(t, f, conversation, "", token, "补充问题", "public")
	internal := statisticsMessage(t, f, conversation, f.agent, "", "内部消息不能截断等待", "internal")
	a2 := statisticsMessage(t, f, conversation, f.agent, "", "第二段回答", "public")
	a3 := statisticsMessage(t, f, conversation, f.agent, "", "主动补充无问题不计回复样本", "public")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	mustData(t, status, reply)
	q4 := statisticsMessage(t, f, conversation, "", token, "转接期间的问题", "public")
	o1 := statisticsMessage(t, f, conversation, f.owner, "", "新客服回答", "public")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/evaluation", "", token, map[string]any{"score": 4, "comment": "真实转接评价"})
	mustData(t, status, reply)
	for _, statement := range []string{"UPDATE tenants SET timezone='Asia/Shanghai' WHERE id=$1", "UPDATE conversations SET created_at='2026-10-26T00:00:00+08:00',closed_at='2026-10-26T00:03:00+08:00' WHERE tenant_id=$1", "UPDATE conversation_evaluations SET created_at='2026-10-26T00:04:00+08:00' WHERE tenant_id=$1"} {
		if _, err := f.admin.Exec(ctx, statement, f.tenant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversation_assignments SET accepted_at=CASE WHEN membership_id=$2 THEN '2026-10-26T00:00:10+08:00'::timestamptz ELSE '2026-10-26T00:01:00+08:00'::timestamptz END,released_at=CASE WHEN membership_id=$2 THEN '2026-10-26T00:00:50+08:00'::timestamptz ELSE '2026-10-26T00:03:00+08:00'::timestamptz END WHERE tenant_id=$1", f.tenant, f.agent); err != nil {
		t.Fatal(err)
	}
	for id, clock := range map[string]string{q1: "00:00:01", a1: "00:00:20", q2: "00:00:25", q3: "00:00:28", internal: "00:00:31", a2: "00:00:45", a3: "00:00:47", q4: "00:00:55", o1: "00:01:30"} {
		if _, err := f.admin.Exec(ctx, "UPDATE messages SET created_at=$2 WHERE id=$1", id, "2026-10-26T"+clock+"+08:00"); err != nil {
			t.Fatal(err)
		}
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO member_presence_intervals(tenant_id,membership_id,started_at,last_seen_at,online_until) VALUES($1,$2,'2026-10-25T23:50:00+08:00','2026-10-26T00:09:30+08:00','2026-10-26T00:10:00+08:00'),($1,$2,'2026-10-26T00:20:00+08:00','2026-10-26T00:29:30+08:00','2026-10-26T00:30:00+08:00')`, f.tenant, f.agent)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	agent := statisticsMember(t, f, f.agent)
	owner := statisticsMember(t, f, f.owner)
	if agent["received_conversations"] != float64(1) || agent["closed_conversations"] != float64(0) || agent["public_messages"] != float64(3) || agent["online_seconds"] != float64(1200) {
		t.Fatal("接待/结束、公开消息或跨日在线裁剪错误", agent)
	}
	if agent["first_response_samples"] != float64(1) || math.Abs(agent["avg_first_response_seconds"].(float64)-10) > 0.0001 || agent["reply_samples"] != float64(2) || math.Abs(agent["avg_reply_seconds"].(float64)-15) > 0.0001 {
		t.Fatal("首响与访客等待段回复统计错误", agent)
	}
	if owner["first_response_samples"] != float64(1) || owner["avg_first_response_seconds"] != float64(30) || owner["reply_samples"] != float64(1) || owner["avg_reply_seconds"] != float64(30) {
		t.Fatal("转接后回复混入前一段或排队时间", owner)
	}
	if owner["closed_conversations"] != float64(1) || owner["evaluation_count"] != float64(1) || owner["average_score"] != float64(4) || owner["satisfaction_percent"] != float64(100) || agent["evaluation_count"] != float64(0) || agent["average_score"] != nil {
		t.Fatal("评价应只归最终接待人，无样本保持空值")
	}
	status, _ = f.request("GET", "/api/v1/tenant/member-statistics", f.agent, "", nil)
	if status != 403 {
		t.Fatal("普通客服不应查看团队统计")
	}
	status, reply = f.request("GET", "/api/v1/tenant/member-statistics", f.otherOwner, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		if raw.(map[string]any)["id"] == f.agent {
			t.Fatal("统计跨租户泄漏")
		}
	}
}

// TestNonServingMemberPresenceMergesTabs 验证仅管理成员仍采集在线，多标签续租不影响业务上班意图且不重复时长。
func TestNonServingMemberPresenceMergesTabs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET can_serve=false WHERE id=$1", f.owner); err != nil {
		t.Fatal(err)
	}
	p := f.principals[f.owner]
	p.CanServe = false
	f.principals[f.owner] = p
	a := actor{TenantID: f.tenant, MemberID: f.owner, UserID: p.UserID, Role: p.Role, CanServe: false}
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); failures <- f.s.heartbeat(ctx, a) }()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.countRows("SELECT count(*) FROM member_presence_intervals WHERE membership_id=$1", f.owner) != 1 {
		t.Fatal("多标签管理成员没有合并为一段在线")
	}
	if f.countRows("SELECT count(*) FROM agent_runtime WHERE membership_id=$1 AND (available OR busy OR online_until>statement_timestamp())", f.owner) != 0 {
		t.Fatal("管理成员探活不应改变接待运行状态")
	}
	status, reply := f.request("GET", "/api/v1/tenant/member-statistics", f.owner, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == f.owner && item["online"] != true {
			t.Fatal("仅管理成员未显示在线")
		}
	}
}

// TestPlatformReceptionCapacityIsAggregate 验证剩余接待量来自在线上班非忙成员，满额成员不能被称为仍可接待。
func TestPlatformReceptionCapacityIsAggregate(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	f.online(f.agent)
	var available, total, load, remaining int64
	if err := f.pool.QueryRow(ctx, "SELECT available_agents,total_capacity,current_load,remaining FROM platform_reception_capacity()").Scan(&available, &total, &load, &remaining); err != nil {
		t.Fatal(err)
	}
	if available != 1 || total != 1 || load != 0 || remaining != 1 {
		t.Fatal("空闲接待容量汇总错误")
	}
	_, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	if err := f.pool.QueryRow(ctx, "SELECT available_agents,total_capacity,current_load,remaining FROM platform_reception_capacity()").Scan(&available, &total, &load, &remaining); err != nil {
		t.Fatal(err)
	}
	if available != 0 || total != 1 || load != 1 || remaining != 0 {
		t.Fatal("满额成员仍被算作可接待或余量不为零")
	}
}
