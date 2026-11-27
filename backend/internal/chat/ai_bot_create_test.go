// 文件作用说明：在真实PostgreSQL隔离schema验证新增本人机器人、默认关闭、独立配置及跨成员租户保护。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// createMemberBotFixture 通过正式新增路由创建本人机器人，检查201及完整data响应，不伪造前端成功状态。
func createMemberBotFixture(t *testing.T, f *fixture, member, name string) map[string]any {
	t.Helper()
	status, response := f.request("POST", "/api/v1/me/ai/bots", member, "", map[string]any{"name": name})
	if status != 201 {
		t.Fatalf("新增机器人失败：HTTP%d code=%v", status, response["code"])
	}
	bot, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatal("新增响应缺少完整机器人data")
	}
	return bot
}

// botCreationProtectedState 记录旧机器人、租户默认接待、入口与成员空间状态，新增或修改新配置不能改变这些数据。
func botCreationProtectedState(t *testing.T, f *fixture, bots []string) string {
	t.Helper()
	var result string
	err := f.admin.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'bots',(SELECT jsonb_agg(to_jsonb(b) ORDER BY id) FROM ai_bots b WHERE id=ANY($2::uuid[])),
 'settings',(SELECT to_jsonb(s) FROM ai_tenant_settings s WHERE tenant_id=$1),
 'links',(SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM access_links l WHERE tenant_id=$1),
 'memberships',(SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1),
 'spaces',(SELECT count(*) FROM service_spaces WHERE tenant_id=$1))::text`, f.tenant, bots).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestAICreateMemberBotsIsolated 新增两个同名配置仍产生独立编号，保留旧机器人、默认接待和入口，并沿用本人编辑权限。
func TestAICreateMemberBotsIsolated(t *testing.T) {
	f := newFixture(t)
	primary, secondary := cloneEntryBot(t, f)
	if _, err := f.admin.Exec(t.Context(), "UPDATE access_links SET ai_bot_id=$1 WHERE entry_key=$2", secondary, f.entry); err != nil {
		t.Fatal(err)
	}
	protected := botCreationProtectedState(t, f, []string{primary, secondary})
	ids := []string{}
	for _, name := range []string{"  新机器人  ", "新机器人"} {
		bot := createMemberBotFixture(t, f, f.owner, name)
		id, ok := bot["id"].(string)
		if !ok || !uuidPattern.MatchString(id) || id == primary || id == secondary || len(ids) > 0 && id == ids[0] {
			t.Fatal("新增机器人没有获得独立服务端编号")
		}
		if bot["owner_membership_id"] != f.owner || bot["tenant_id"] != f.tenant || bot["name"] != "新机器人" || bot["mode"] != "off" || bot["model_id"] != nil || bot["prompt"] != "" || bot["language"] != "auto" || bot["avatar_ref"] != "goodchat" || bot["version"] != float64(1) {
			t.Fatal("新配置没有使用本人归属及安全默认值")
		}
		var decoded aiBot
		if err := json.Unmarshal(jsonValue(bot), &decoded); err != nil || len(decoded.KnowledgeIDs) != 0 || len(decoded.ScriptIDs) != 0 || !reflect.DeepEqual(decoded.Settings, defaultAIBot().Settings) {
			t.Fatal("新配置不应继承旧知识、话术、预算或业务时段")
		}
		ids = append(ids, id)
	}
	status, response := f.request("GET", "/api/v1/tenant/ai/bots", f.owner, "", nil)
	listed := mustData(t, status, response)["items"].([]any)
	if len(listed) != 4 {
		t.Fatal("正常机器人列表未包含原有两套和新增两套配置")
	}
	for _, id := range ids {
		status, response = f.request("GET", "/api/v1/me/ai/bot?bot_id="+id, f.owner, "", nil)
		var edited aiBot
		if err := json.Unmarshal(jsonValue(mustData(t, status, response)), &edited); err != nil {
			t.Fatal(err)
		}
		edited.Name = "新配置已编辑"
		status, response = f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", edited)
		if updated := mustData(t, status, response); updated["id"] != id || updated["version"] != float64(2) || updated["name"] != edited.Name {
			t.Fatal("新增配置不能独立读取与编辑")
		}
		for _, member := range []string{f.agent, f.otherOwner} {
			status, _ = f.request("GET", "/api/v1/me/ai/bot?bot_id="+id, member, "", nil)
			if status != 404 {
				t.Fatal("其他成员或租户不能读取新增的本人配置", status)
			}
			edited.Version = 2
			status, _ = f.request("PUT", "/api/v1/me/ai/bot", member, "", edited)
			if status != 404 {
				t.Fatal("其他成员或租户不能修改新增的本人配置", status)
			}
		}
	}
	if botCreationProtectedState(t, f, []string{primary, secondary}) != protected {
		t.Fatal("新增或编辑新机器人改变了旧配置、默认接待、入口、成员或空间")
	}
}

// TestAICreateMemberBotValidation 新增只接受合法名称，不接受伪造归属、自动启用参数或已经停用的成员。
func TestAICreateMemberBotValidation(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"", " \t\n ", strings.Repeat("中", 61), "非法\x00名称", "换\n行"} {
		status, response := f.request("POST", "/api/v1/me/ai/bots", f.owner, "", map[string]any{"name": name})
		if status != 400 || response["code"] != "ai_bot_invalid" {
			t.Fatal("非法名称应在进入数据库前被拒绝", status, response["code"])
		}
	}
	for _, field := range []string{"tenant_id", "owner_membership_id", "id", "mode"} {
		status, response := f.request("POST", "/api/v1/me/ai/bots", f.owner, "", map[string]any{"name": "伪造配置", field: f.otherTenant})
		if status != 400 || response["code"] != "invalid_request" {
			t.Fatal("新增不能接受客户端指定的身份或接待配置", status, response["code"])
		}
	}
	status, _ := f.request("POST", "/api/v1/me/ai/bots", "", "", map[string]any{"name": "未登录配置"})
	if status != 401 || f.countRows("SELECT count(*) FROM ai_bots") != 0 {
		t.Fatal("未登录或非法请求不能产生机器人")
	}
	name := strings.Repeat("中", 60)
	if bot := createMemberBotFixture(t, f, f.owner, name); bot["name"] != name {
		t.Fatal("合法60个中文字符名称应完整保留")
	}
	// 无接待资格的有效成员仍可准备关闭配置，不会因此开通接待资格或默认接待。
	if _, err := f.admin.Exec(t.Context(), "UPDATE tenant_memberships SET can_serve=false WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	principal := f.principals[f.agent]
	principal.CanServe = false
	f.principals[f.agent] = principal
	settings := f.countRows("SELECT count(*) FROM ai_tenant_settings")
	if bot := createMemberBotFixture(t, f, f.agent, "准备中的机器人"); bot["mode"] != "off" || bot["owner_membership_id"] != f.agent {
		t.Fatal("新增配置不能隐式开通接待资格")
	}
	if f.countRows("SELECT count(*) FROM ai_tenant_settings") != settings || f.countRows("SELECT count(*) FROM tenant_memberships WHERE id=$1 AND can_serve", f.agent) != 0 {
		t.Fatal("新增配置不能改变默认接待或坐席资格")
	}
	if _, err := f.admin.Exec(t.Context(), "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	status, response := f.request("POST", "/api/v1/me/ai/bots", f.agent, "", map[string]any{"name": "停用成员配置"})
	if status != 403 || response["code"] != "membership_revoked" || f.countRows("SELECT count(*) FROM ai_bots WHERE owner_membership_id=$1", f.agent) != 1 {
		t.Fatal("停用成员不能继续创建配置", status, response["code"])
	}
}

// TestAICreateMemberBotKeepsImplicitSelection 验证普通成员新增较小编号的关闭配置后，旧助手与未绑定机器人的AI接待入口仍选择原配置。
func TestAICreateMemberBotKeepsImplicitSelection(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "原机器人继续接待"}}}, "", true)
	})
	bot := defaultAIBot()
	bot.Mode, bot.ModelID = "auto", model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", bot)
	primary := mustData(t, status, data)["id"].(string)
	if f.countRows("SELECT count(*) FROM ai_tenant_settings WHERE tenant_id=$1 AND default_bot_id IS NOT NULL", f.tenant) != 0 || f.countRows("SELECT count(*) FROM access_links WHERE entry_key=$1 AND ai_bot_id IS NOT NULL", f.entry) != 0 {
		t.Fatal("测试必须在没有租户默认与入口机器人绑定时开始")
	}
	f.online(f.agent)
	token, _, active := f.visitor("")
	personalClaim(t, f, active, f.agent)
	status, data = f.request("POST", "/api/v1/conversations/"+active+"/ai", f.agent, "", map[string]any{"action": "resume"})
	mustData(t, status, data)
	status, data = f.request("POST", "/api/v1/conversations/"+active+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "新增配置前的普通成员问题"})
	mustData(t, status, data)
	var pendingJob string
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM ai_jobs WHERE conversation_id=$1 AND bot_id=$2", active, primary).Scan(&pendingJob); err != nil {
		t.Fatal("原成员助手没有建立真实任务", err)
	}
	created := createMemberBotFixture(t, f, f.agent, "新增关闭配置")
	lower := "00000000-0000-0000-0000-000000000001"
	if _, err := f.admin.Exec(ctx, "UPDATE ai_bots SET id=$1 WHERE id=$2", lower, created["id"]); err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM ai_bots old JOIN ai_bots fresh ON fresh.id=$2 WHERE old.id=$1 AND fresh.id<old.id AND fresh.created_at>old.created_at AND fresh.mode='off'", primary, lower) != 1 {
		t.Fatal("测试没有建立较晚创建但编号较小的关闭配置")
	}
	status, data = f.request("GET", "/api/v1/me/ai/bot", f.agent, "", nil)
	if mustData(t, status, data)["id"] != primary {
		t.Fatal("新增关闭配置抢占了普通成员的隐式默认助手")
	}
	err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		job, err := aiJobFromRow(ctx, tx, pendingJob)
		if err != nil {
			return err
		}
		return f.s.aiReplyValid(ctx, tx, &job, false)
	})
	if err != nil {
		t.Fatal("新增配置不应使原助手已排队的任务失效", err)
	}
	// 旧客户端不传编号时仍保存原机器人；updated_at变化也不能让回退选择转向新配置。
	bot.Version, bot.Name = 1, "编辑后的原机器人"
	status, data = f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", bot)
	if mustData(t, status, data)["id"] != primary {
		t.Fatal("旧客户端隐式保存修改了新增配置")
	}
	status, data = f.request("POST", "/api/v1/conversations/"+active+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "新增配置后的普通成员问题"})
	mustData(t, status, data)
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE conversation_id=$1 AND bot_id=$2", active, primary) != 2 {
		t.Fatal("人工接待入口未继续为原助手建立任务")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE access_links SET ai_agent_id=(SELECT id FROM ai_service_agents WHERE tenant_id=$1) WHERE entry_key=$2", f.tenant, f.entry); err != nil {
		t.Fatal(err)
	}
	status, data = f.request("GET", "/api/v1/tenant/ai-service-agent", f.owner, "", nil)
	if mustData(t, status, data)["configured"] != true {
		t.Fatal("新增关闭配置使原AI接待错误显示为未配置")
	}
	queuedToken, _, queued := f.visitor("")
	err = platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		id, err := aiReceptionBot(ctx, tx, queued, true)
		if err == nil && id != primary {
			t.Fatalf("无机器人绑定的AI接待入口选择改变：%s", id)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	status, data = f.request("POST", "/api/v1/conversations/"+queued+"/messages", "", queuedToken, map[string]any{"client_message_id": newID(), "content": "未明确绑定机器人的AI接待问题"})
	mustData(t, status, data)
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE conversation_id=$1 AND bot_id=$2 AND snapshot->>'agent'=''", queued, primary) != 1 || f.countRows("SELECT count(*) FROM ai_jobs WHERE bot_id=$1", lower) != 0 {
		t.Fatal("AI接待入口未继续使用原配置，或新增关闭配置意外参与接待")
	}
}

// TestAIBotCreatedAtMigrationPreservesLegacy 验证历史配置只增加统一时间，不改原配置JSON或同批回退顺序，之后新增仍排在原配置后。
func TestAIBotCreatedAtMigrationPreservesLegacy(t *testing.T) {
	f := newFixture(t)
	first, second := cloneEntryBot(t, f)
	ctx := t.Context()
	var protected, migrated, selected string
	if err := f.admin.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(b)-'created_at' ORDER BY id)::text FROM ai_bots b WHERE tenant_id=$1", f.tenant).Scan(&protected); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, "ALTER TABLE ai_bots DROP COLUMN created_at"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../database/migrations/053_ai_bot_created_at.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(b)-'created_at' ORDER BY id)::text FROM ai_bots b WHERE tenant_id=$1", f.tenant).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if protected != migrated || f.countRows("SELECT count(DISTINCT created_at) FROM ai_bots WHERE tenant_id=$1", f.tenant) != 1 {
		t.Fatal("创建时间迁移改变了历史机器人配置或没有统一历史回填时间")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE ai_tenant_settings SET default_bot_id=NULL WHERE tenant_id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	selected = first
	if second < selected {
		selected = second
	}
	created := createMemberBotFixture(t, f, f.owner, "迁移后的新配置")
	if _, err := f.admin.Exec(ctx, "UPDATE ai_bots SET id='00000000-0000-0000-0000-000000000001' WHERE id=$1", created["id"]); err != nil {
		t.Fatal(err)
	}
	status, data := f.request("GET", "/api/v1/me/ai/bot", f.owner, "", nil)
	if mustData(t, status, data)["id"] != selected {
		t.Fatal("迁移或新增配置改变了历史回退顺序")
	}
}
