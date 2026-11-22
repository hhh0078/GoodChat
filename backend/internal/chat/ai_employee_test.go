// 文件作用说明：用真实PostgreSQL和HTTP验证默认AI成员、入口接待与人工转接规则，供应商在本文件中明确模拟。
package chat

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"supportdesk/backend/internal/platform"
	"testing"
)

// prepareAIEmployee 创建可自动接待的真实配置并显式选择默认AI成员，不创建额外人工坐席。
func prepareAIEmployee(t *testing.T, f *fixture, mode string) string {
	t.Helper()
	ctx := context.Background()
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		aiFixtureResponse(w, "request_handoff", map[string]any{"reason": "访客要求人工处理"}, "", true)
	})
	bot := defaultAIBot()
	bot.Mode = "auto"
	bot.ModelID = model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", bot)
	id := mustData(t, status, data)["id"].(string)
	status, data = f.request("PUT", "/api/v1/tenant/ai/settings", f.owner, "", map[string]any{"default_bot_id": id, "reception_mode": "human_first"})
	mustData(t, status, data)
	var employee, link string
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM ai_service_agents WHERE tenant_id=$1", f.tenant).Scan(&employee); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM access_links WHERE entry_key=$1", f.entry).Scan(&link); err != nil {
		t.Fatal(err)
	}
	status, data = f.request("PATCH", "/api/v1/tenant/access-links/"+link, f.owner, "", map[string]any{"ai_agent_id": employee, "reception_mode": mode})
	mustData(t, status, data)
	if _, err := f.admin.Exec(ctx, "UPDATE routing_pools SET algorithm='least_load',assignment_mode='auto' WHERE tenant_id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	return employee
}

// TestAIEmployeeProfileAndHandoff 验证资料、不可删除与跨租户保护，AI先接待，转人工严格遵循入口模式且不会再次接回AI。
func TestAIEmployeeProfileAndHandoff(t *testing.T) {
	for _, mode := range []string{"auto", "manual", "specified"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			routingMode := mode
			if mode == "specified" {
				routingMode = "auto"
			}
			employee := prepareAIEmployee(t, f, routingMode)
			if mode == "specified" {
				if _, err := f.admin.Exec(ctx, "UPDATE access_links SET agent_id=$2 WHERE entry_key=$1", f.entry, f.agent); err != nil {
					t.Fatal(err)
				}
			}
			f.online(f.agent)
			status, data := f.request("GET", "/api/v1/tenant/ai-service-agent", f.owner, "", nil)
			profile := mustData(t, status, data)
			if profile["id"] != employee || profile["can_delete"] != false || profile["avatar_url"] != "/logo/icon-192.png" {
				t.Fatal("默认AI资料不正确", profile)
			}
			status, data = f.request("PUT", "/api/v1/tenant/ai-service-agent", f.owner, "", map[string]any{"name": "专属AI顾问", "avatar_ref": "default-02", "enabled": true, "priority": 100, "description": "产品咨询", "version": profile["version"]})
			mustData(t, status, data)
			status, _ = f.request("PUT", "/api/v1/tenant/ai-service-agent", f.agent, "", map[string]any{})
			if status != 403 {
				t.Fatal("普通客服不得编辑默认AI", status)
			}
			status, _ = f.request("DELETE", "/api/v1/tenant/ai-service-agent", f.owner, "", nil)
			if status != 405 {
				t.Fatal("默认AI不能删除", status)
			}
			token, _, conversation := f.visitor("")
			if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND status='queued' AND context->>'ai_receiving'='true' AND NOT ai_paused", conversation) != 1 {
				t.Fatal("已选择AI成员的入口没有自动AI接待")
			}
			status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "请转人工"})
			mustData(t, status, data)
			runAITestJob(t, f)
			want := "active"
			if mode == "manual" {
				want = "queued"
			}
			if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND ai_paused AND status=$2 AND NOT(context ? 'ai_receiving')", conversation, want) != 1 {
				t.Fatal("转人工没有遵循入口模式", mode)
			}
			if mode == "manual" {
				personalClaim(t, f, conversation, f.agent)
			}
			err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error { return f.s.assignLocked(ctx, tx, f.tenant, conversation, "", false) })
			if err != nil {
				t.Fatal(err)
			}
			if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND current_agent_id=$2 AND ai_paused", conversation, f.agent) != 1 {
				t.Fatal("AI抢回转人工会话")
			}
		})
	}
}

// TestAIQueueMetricsAndFairness 验证在线AI计为接待，离线旧咨询不计排队，AI队首不会阻塞后续人工自动分配。
func TestAIQueueMetricsAndFairness(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prepareAIEmployee(t, f, "auto")
	_, _, ai := f.visitor("")
	if _, err := f.admin.Exec(ctx, "UPDATE access_links SET ai_agent_id=NULL WHERE entry_key=$1", f.entry); err != nil {
		t.Fatal(err)
	}
	_, _, human := f.visitor("")
	_, _, offline := f.visitor("")
	if _, err := f.admin.Exec(ctx, "UPDATE contacts SET online_until=now()+interval '5 minutes' WHERE id IN(SELECT contact_id FROM conversations WHERE id IN($1,$2))", ai, human); err != nil {
		t.Fatal(err)
	}
	status, data := f.request("GET", "/api/v1/tenant/dashboard", f.owner, "", nil)
	metrics := mustData(t, status, data)
	if metrics["queued"] != float64(1) || metrics["active"] != float64(1) || metrics["offline_open"] != float64(1) {
		t.Fatal("AI与在线队列统计不正确", metrics, offline)
	}
	status, data = f.request("GET", "/api/v1/conversations?status=active", f.owner, "", nil)
	items := mustData(t, status, data)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != ai {
		t.Fatal("AI没有进入工作台接待中")
	}
	status, data = f.request("GET", "/api/v1/conversations?status=queued", f.owner, "", nil)
	items = mustData(t, status, data)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != human {
		t.Fatal("工作台排队混入AI或离线历史")
	}
	f.online(f.agent)
	var pool string
	if err := f.admin.QueryRow(ctx, "SELECT routing_pool_id::text FROM conversations WHERE id=$1", human).Scan(&pool); err != nil {
		t.Fatal(err)
	}
	err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error { return f.s.routePool(ctx, tx, f.tenant, pool) })
	if err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND current_agent_id=$2 AND status='active'", human, f.agent) != 1 {
		t.Fatal("AI队首阻塞人工自动分配")
	}
}

// TestAIReplyItemRepair 验证真实提交链路中无效文字计划只重试一次，不发送错误计划且不重复扣费。
func TestAIReplyItemRepair(t *testing.T) {
	f := newFixture(t)
	calls := 0
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		calls++
		version := int64(1)
		if calls > 1 {
			version = 0
		}
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "合法修复回复", Version: version}}}, "", true)
	})
	b := defaultAIBot()
	b.Mode = "auto"
	b.ModelID = model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	bot := mustData(t, status, data)["id"].(string)
	status, data = f.request("PUT", "/api/v1/tenant/ai/settings", f.owner, "", map[string]any{"default_bot_id": bot, "reception_mode": "ai_first"})
	mustData(t, status, data)
	token, _, conversation := f.visitor("")
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "你好"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if calls != 2 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai' AND content='合法修复回复'", conversation) != 1 || f.countRows("SELECT count(*) FROM ai_call_attempts WHERE response_meta->>'platform_covered'='true'") != 1 {
		t.Fatal("修复次数、真实消息或无效调用费用不正确")
	}
}

// TestDisabledAIReleasesQueue 验证停用AI成员会暂停旧任务并交给入口原有人工队列，不留下永久AI接待标记。
func TestDisabledAIReleasesQueue(t *testing.T) {
	f := newFixture(t)
	prepareAIEmployee(t, f, "auto")
	_, _, conversation := f.visitor("")
	status, data := f.request("GET", "/api/v1/tenant/ai-service-agent", f.owner, "", nil)
	profile := mustData(t, status, data)
	status, data = f.request("PUT", "/api/v1/tenant/ai-service-agent", f.owner, "", map[string]any{"name": profile["name"], "avatar_ref": profile["avatar_ref"], "enabled": false, "priority": 100, "description": "", "version": profile["version"]})
	mustData(t, status, data)
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND ai_paused AND NOT(context ? 'ai_receiving')", conversation) != 1 {
		t.Fatal("停用AI后旧会话仍被AI占用")
	}
}

// TestAIEmployeeUploadedAvatar 验证AI成员可用本租户上传头像，不能引用其他租户的私有上传资源。
func TestAIEmployeeUploadedAvatar(t *testing.T) {
	f := newFixture(t)
	status, data := uploadAvatarFixture(t, f, f.owner, avatarTestPNG(t, 20, 20))
	if status != 201 {
		t.Fatal("上传本租户头像失败", status)
	}
	own := data["data"].(map[string]any)["id"].(string)
	status, data = uploadAvatarFixture(t, f, f.otherOwner, avatarTestPNG(t, 20, 20))
	if status != 201 {
		t.Fatal("上传隔离租户头像失败", status)
	}
	foreign := data["data"].(map[string]any)["id"].(string)
	status, data = f.request("GET", "/api/v1/tenant/ai-service-agent", f.owner, "", nil)
	profile := mustData(t, status, data)
	body := map[string]any{"name": "品牌AI", "avatar_ref": foreign, "enabled": true, "priority": 100, "description": "", "version": profile["version"]}
	status, _ = f.request("PUT", "/api/v1/tenant/ai-service-agent", f.owner, "", body)
	if status != 400 {
		t.Fatal("AI成员引用了其他租户头像")
	}
	body["avatar_ref"] = own
	status, data = f.request("PUT", "/api/v1/tenant/ai-service-agent", f.owner, "", body)
	updated := mustData(t, status, data)
	if updated["avatar_url"] != "/api/v1/avatar-assets/"+own {
		t.Fatal("上传头像没有正确显示")
	}
}

// TestAIEmployeeIndependentOfHumanSeat 验证管理员选中的AI员工无需占用配置者的人工坐席或组成员资格，旧机器人授权规则不变。
func TestAIEmployeeIndependentOfHumanSeat(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	prepareAIEmployee(t, f, "auto")
	if _, err := f.admin.Exec(ctx, "DELETE FROM agent_group_members WHERE membership_id=$1", f.owner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET can_serve=false WHERE id=$1", f.owner); err != nil {
		t.Fatal(err)
	}
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND context->>'ai_receiving'='true'", conversation) != 1 {
		t.Fatal("AI员工错误地依赖人工坐席资格")
	}
	status, data := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "请转人工"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND ai_paused AND status='active' AND current_agent_id=$2", conversation, f.agent) != 1 {
		t.Fatal("AI员工未执行真实任务或转接未遵循自动匹配")
	}
}
