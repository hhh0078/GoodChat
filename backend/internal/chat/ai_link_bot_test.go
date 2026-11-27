// 文件作用说明：在隔离PostgreSQL验证同成员双机器人编辑、入口隔离、独立身份及转人工保护；供应商沿用显式模拟夹具。
package chat

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// cloneEntryBot 模拟租户合并后保留的第二套配置，稳定保留机器人编号与独立提示词。
func cloneEntryBot(t *testing.T, f *fixture) (string, string) {
	t.Helper()
	prepareAIEmployee(t, f, "manual")
	var primary string
	ctx := context.Background()
	if err := f.admin.QueryRow(ctx, "SELECT default_bot_id::text FROM ai_tenant_settings WHERE tenant_id=$1", f.tenant).Scan(&primary); err != nil {
		t.Fatal(err)
	}
	other := newID()
	knowledge := newID()
	if _, err := f.admin.Exec(ctx, `INSERT INTO ai_knowledge_bases(id,tenant_id,owner_membership_id,title,draft_body,published_body,description,status,published_version) SELECT $1,tenant_id,owner_membership_id,'GoodHR独立知识','只提供GoodHR业务事实','只提供GoodHR业务事实','仅供GoodHR入口使用','ready',1 FROM ai_bots WHERE id=$2`, knowledge, primary); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, `INSERT INTO ai_bots(id,tenant_id,owner_membership_id,model_id,name,mode,prompt,language,knowledge_ids,script_ids,settings,avatar_ref) SELECT $1,tenant_id,owner_membership_id,NULL,'GoodHR独立顾问',mode,'只回答GoodHR业务',language,ARRAY[$3::uuid],script_ids,jsonb_set(settings,'{daily_budget_micro}','10000000'::jsonb),'default-03' FROM ai_bots WHERE id=$2`, other, primary, knowledge); err != nil {
		t.Fatal(err)
	}
	return primary, other
}

// TestAIMultipleBotSelectionAndOwnership 验证明确定义和旧客户端编辑各自配置，跨成员与跨租户选择不泄漏。
func TestAIMultipleBotSelectionAndOwnership(t *testing.T) {
	f := newFixture(t)
	primary, other := cloneEntryBot(t, f)
	migration, err := os.ReadFile("../database/migrations/052_link_ai_bot.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(context.Background(), string(migration)); err != nil {
		t.Fatal("双配置准备后重复执行正式迁移应保持幂等", err)
	}
	status, data := f.request("GET", "/api/v1/me/ai/bot", f.owner, "", nil)
	if mustData(t, status, data)["id"] != primary {
		t.Fatal("无编号请求没有稳定选择原默认机器人")
	}
	status, data = f.request("GET", "/api/v1/me/ai/bot?bot_id="+other, f.owner, "", nil)
	if mustData(t, status, data)["prompt"] != "只回答GoodHR业务" {
		t.Fatal("明确选择丢失独立配置")
	}
	b := defaultAIBot()
	b.ID, b.Version, b.Name, b.Prompt = other, 1, "GoodHR更新顾问", "仅更新GoodHR配置"
	status, data = f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	if mustData(t, status, data)["id"] != other || f.countRows("SELECT count(*) FROM ai_bots WHERE id=$1 AND prompt='仅更新GoodHR配置'", other) != 1 {
		t.Fatal("明确更新没有落在选定机器人")
	}
	b.ID, b.Version, b.Name, b.Prompt = "", 1, "原默认机器人", "只更新默认配置"
	status, data = f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	if mustData(t, status, data)["id"] != primary || f.countRows("SELECT count(*) FROM ai_bots WHERE id=$1 AND prompt='仅更新GoodHR配置'", other) != 1 {
		t.Fatal("旧客户端更新覆盖了其他机器人")
	}
	for _, member := range []string{f.agent, f.otherOwner} {
		status, _ = f.request("GET", "/api/v1/me/ai/bot?bot_id="+other, member, "", nil)
		if status != 404 {
			t.Fatal("读取他人机器人未被拒绝", status)
		}
		b.ID, b.Version = other, 2
		status, _ = f.request("PUT", "/api/v1/me/ai/bot", member, "", b)
		if status != 404 {
			t.Fatal("修改他人机器人未被拒绝", status)
		}
	}
	status, data = f.request("POST", "/api/v1/me/ai/test", f.owner, "", map[string]any{"text": "隔离测试问题", "bot_id": other})
	if status != 202 || data["data"] == nil || f.countRows("SELECT count(*) FROM ai_jobs WHERE kind='test' AND bot_id=$1 AND snapshot#>>'{bot,prompt}'='仅更新GoodHR配置'", other) != 1 {
		t.Fatal("调试没有使用选中的机器人", status)
	}
}

// TestAILinkBotIsolationAndHandoff 验证两个入口保留独立机器人身份与角色，换绑拒绝迟到结果，转人工不会被机器人抢回。
func TestAILinkBotIsolationAndHandoff(t *testing.T) {
	f := newFixture(t)
	primary, other := cloneEntryBot(t, f)
	ctx := context.Background()
	otherKey := newID()
	if _, err := f.admin.Exec(ctx, `INSERT INTO access_links(tenant_id,service_space_id,service_group_id,routing_pool_id,entry_key,name,reception_mode,ai_agent_id,ai_bot_id) SELECT tenant_id,service_space_id,service_group_id,routing_pool_id,$1,'GoodHR独立入口','manual',ai_agent_id,$2 FROM access_links WHERE entry_key=$3`, otherKey, other, f.entry); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ key, bot string }{{f.entry, primary}, {otherKey, other}} {
		status, data := f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": entry.key, "visit_id": newID()})
		visitor := mustData(t, status, data)
		conversation := visitor["conversation"].(map[string]any)["id"].(string)
		token := visitor["token"].(string)
		status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "请帮我处理业务问题"})
		mustData(t, status, data)
		if f.countRows("SELECT count(*) FROM ai_jobs WHERE conversation_id=$1 AND bot_id=$2", conversation, entry.bot) != 1 {
			t.Fatal("入口使用了其他配置")
		}
		if entry.bot == other {
			if f.countRows("SELECT count(*) FROM ai_jobs WHERE conversation_id=$1 AND snapshot#>>'{bot,name}'='GoodHR独立顾问' AND snapshot#>>'{bot,avatar_ref}'='default-03' AND snapshot#>>'{bot,prompt}'='只回答GoodHR业务' AND snapshot#>>'{bot,model_id}'='' AND snapshot#>>'{bot,settings,daily_budget_micro}'='10000000' AND jsonb_array_length(snapshot#>'{bot,knowledge_ids}')=1", conversation) != 1 {
				t.Fatal("入口独立身份被全局AI员工覆盖")
			}
			var jobID string
			if err := f.admin.QueryRow(ctx, "SELECT id::text FROM ai_jobs WHERE conversation_id=$1", conversation).Scan(&jobID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.admin.Exec(ctx, "UPDATE access_links SET ai_bot_id=$1 WHERE entry_key=$2", primary, otherKey); err != nil {
				t.Fatal(err)
			}
			err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
				j, err := aiJobFromRow(ctx, tx, jobID)
				if err != nil {
					return err
				}
				return f.s.aiReplyValid(ctx, tx, &j, false)
			})
			if err == nil || err.Error() != "ai_stale" {
				t.Fatal("入口换绑没有阻止旧机器人迟到回复", err)
			}
			if _, err := f.admin.Exec(ctx, "UPDATE access_links SET ai_bot_id=$1 WHERE entry_key=$2", other, otherKey); err != nil {
				t.Fatal(err)
			}
		}
		runAITestJob(t, f)
		if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND ai_paused AND status='queued'", conversation) != 1 {
			t.Fatal("入口机器人没有按人工领取模式转接")
		}
		err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			id, err := aiReceptionBot(ctx, tx, conversation, false)
			if err == nil && id != "" {
				t.Fatal("暂停后的机器人重新接回人工咨询")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestAIActiveLinkRebindRejectsPreviousBot 验证同一人工接待及版本未变时，入口换绑仍会阻止原机器人迟到发送。
func TestAIActiveLinkRebindRejectsPreviousBot(t *testing.T) {
	f := newFixture(t)
	primary, other := cloneEntryBot(t, f)
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, "UPDATE access_links SET ai_bot_id=$1 WHERE entry_key=$2", primary, f.entry); err != nil {
		t.Fatal(err)
	}
	f.online(f.owner)
	token, _, conversation := f.visitor("")
	personalClaim(t, f, conversation, f.owner)
	status, data := f.request("POST", "/api/v1/conversations/"+conversation+"/ai", f.owner, "", map[string]any{"action": "resume"})
	mustData(t, status, data)
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "人工接待中的隔离问题"})
	mustData(t, status, data)
	var jobID string
	if err := f.admin.QueryRow(ctx, "SELECT id::text FROM ai_jobs WHERE conversation_id=$1 AND bot_id=$2 AND snapshot->>'agent'=$3", conversation, primary, f.owner).Scan(&jobID); err != nil {
		t.Fatal("没有创建当前成员的入口A机器人任务", err)
	}
	// 换绑前任务应有效，排除测试因归属、暂停或历史消息变化而意外通过。
	err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		j, err := aiJobFromRow(ctx, tx, jobID)
		if err != nil {
			return err
		}
		return f.s.aiReplyValid(ctx, tx, &j, true)
	})
	if err != nil {
		t.Fatal("换绑前任务应当有效", err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE access_links SET ai_bot_id=$1 WHERE entry_key=$2", other, f.entry); err != nil {
		t.Fatal(err)
	}
	for _, lock := range []bool{false, true} {
		err = platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			j, err := aiJobFromRow(ctx, tx, jobID)
			if err != nil {
				return err
			}
			return f.s.aiReplyValid(ctx, tx, &j, lock)
		})
		if err == nil || err.Error() != "ai_stale" {
			t.Fatal("人工接待入口A换绑B后未拒绝A迟到回复", lock, err)
		}
	}
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM messages WHERE ai_job_id=$1", jobID) != 0 {
		t.Fatal("过期入口机器人实际发送了消息")
	}
}
