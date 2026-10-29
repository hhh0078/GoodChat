// 文件作用说明：以真实隔离PostgreSQL与HTTP验证个人自动回复归属、只读共享、接待触发、优先级与延迟撤销。
package chat

import (
	"context"
	"testing"
)

// createPersonalWelcome 通过成员HTTP接口创建指定场景的个人问候。
func createPersonalWelcome(t *testing.T, f *fixture, member, scenario, content string, delay int) map[string]any {
	t.Helper()
	status, reply := f.request("POST", "/api/v1/me/welcome-rules", member, "", map[string]any{"name": "个人问候", "scenario": scenario, "enabled": true, "contents": []any{map[string]any{"type": "text", "content": content, "delay_seconds": delay}}})
	return mustData(t, status, reply)
}

// createPersonalKeyword 通过成员HTTP接口创建个人关键词，配置编号不由测试直接写入。
func createPersonalKeyword(t *testing.T, f *fixture, member, pattern, reply string, priority int) map[string]any {
	t.Helper()
	status, response := f.request("POST", "/api/v1/me/keyword-rules", member, "", map[string]any{"name": "个人关键词", "pattern": pattern, "reply": reply, "priority": priority, "cooldown_seconds": 0})
	return mustData(t, status, response)
}

// personalClaim 通过正式接单接口建立当前客服归属。
func personalClaim(t *testing.T, f *fixture, conversation, member string) {
	t.Helper()
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", member, "", map[string]any{})
	mustData(t, status, reply)
}

// TestPersonalAutomationOwnershipAndReadonly 验证只可编辑本人规则、普通成员可读开启团队规则、管理员旧入口不能触碰个人规则。
func TestPersonalAutomationOwnershipAndReadonly(t *testing.T) {
	f := newFixture(t)
	ownWelcome := createPersonalWelcome(t, f, f.agent, "all", "客服个人欢迎", 0)
	otherWelcome := createPersonalWelcome(t, f, f.owner, "all", "其他客服个人欢迎", 0)
	ownKeyword := createPersonalKeyword(t, f, f.agent, "本人", "仅本人关键词", 4)
	otherKeyword := createPersonalKeyword(t, f, f.owner, "他人", "其他客服关键词", 4)
	status, reply := f.request("POST", "/api/v1/tenant/welcome-rules", f.owner, "", map[string]any{"name": "团队欢迎", "scope_type": "tenant", "contents": []any{map[string]any{"type": "text", "content": "团队欢迎正文"}}})
	sharedWelcome := mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/tenant/keyword-rules", f.owner, "", map[string]any{"name": "团队关键词", "pattern": "团队", "reply": "团队回复"})
	sharedKeyword := mustData(t, status, reply)
	for _, test := range []struct {
		resource           string
		own, other, shared map[string]any
	}{{"welcome-rules", ownWelcome, otherWelcome, sharedWelcome}, {"keyword-rules", ownKeyword, otherKeyword, sharedKeyword}} {
		status, reply = f.request("GET", "/api/v1/me/"+test.resource, f.agent, "", nil)
		items := mustData(t, status, reply)["items"].([]any)
		foundOwn, foundShared := false, false
		for _, raw := range items {
			item := raw.(map[string]any)
			if item["id"] == test.other["id"] {
				t.Fatal("个人列表泄露他人规则")
			}
			if item["id"] == test.own["id"] {
				foundOwn = item["editable"] == true && item["source_scope"] == "personal"
			}
			if item["id"] == test.shared["id"] {
				foundShared = item["editable"] == false && item["source_scope"] == "tenant"
			}
		}
		if !foundOwn || !foundShared {
			t.Fatal("个人或只读共享标记不正确")
		}
		for _, target := range []map[string]any{test.other, test.shared} {
			status, _ = f.request("DELETE", "/api/v1/me/"+test.resource+"/"+target["id"].(string), f.agent, "", map[string]any{"version": target["version"]})
			if status != 403 {
				t.Fatalf("不能通过个人入口删除他人或团队规则: %d", status)
			}
		}
		status, reply = f.request("GET", "/api/v1/tenant/"+test.resource, f.owner, "", nil)
		for _, raw := range mustData(t, status, reply)["items"].([]any) {
			if raw.(map[string]any)["id"] == test.own["id"] {
				t.Fatal("管理员公共接口泄露个人规则")
			}
		}
		status, _ = f.request("DELETE", "/api/v1/tenant/"+test.resource+"/"+test.own["id"].(string), f.owner, "", map[string]any{"version": test.own["version"]})
		if status < 400 {
			t.Fatal("管理员公共入口不得删除个人规则")
		}
		status, _ = f.request("DELETE", "/api/v1/me/"+test.resource+"/"+test.own["id"].(string), f.otherOwner, "", map[string]any{"version": test.own["version"]})
		if status != 403 {
			t.Fatal("跨租户个人规则未隔离")
		}
	}
	status, reply = f.request("PATCH", "/api/v1/me/keyword-rules/"+ownKeyword["id"].(string), f.agent, "", map[string]any{"version": ownKeyword["version"], "reply": "个人新回复"})
	updated := mustData(t, status, reply)
	status, _ = f.request("PATCH", "/api/v1/me/keyword-rules/"+ownKeyword["id"].(string), f.agent, "", map[string]any{"version": ownKeyword["version"], "reply": "旧版本覆盖"})
	if status != 409 || updated["version"] != float64(2) {
		t.Fatal("个人关键词缺少并发版本保护")
	}
	status, reply = f.request("DELETE", "/api/v1/me/keyword-rules/"+ownKeyword["id"].(string), f.agent, "", map[string]any{"version": updated["version"]})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/me/keyword-rules", f.agent, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		if raw.(map[string]any)["id"] == ownKeyword["id"] {
			t.Fatal("删除规则仍在个人管理中出现")
		}
	}
}

// TestPersonalWelcomeFirstReceptionAndTransfer 验证团队开关不关闭个人欢迎，接单幂等且转接取消旧任务并不追加新问候。
func TestPersonalWelcomeFirstReceptionAndTransfer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	f.online(f.owner)
	status, reply := f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": false})
	mustData(t, status, reply)
	createPersonalWelcome(t, f, f.agent, "all", "第一客服问候", 60)
	createPersonalWelcome(t, f, f.owner, "all", "转接不得追加", 0)
	_, _, conversation := f.visitor("")
	personalClaim(t, f, conversation, f.agent)
	personalClaim(t, f, conversation, f.agent)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND trigger_key LIKE 'personal-welcome:%'", conversation) != 1 {
		t.Fatal("个人欢迎没有在首次接待幂等创建")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1", conversation) != 1 {
		t.Fatal("转接不应追加新个人欢迎")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='cancelled'", conversation) != 1 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='system'", conversation) != 0 {
		t.Fatal("转接后旧客服问候没有取消")
	}
}

// TestPersonalWelcomeWithoutFirstRuleNeverAddedOnTransfer 验证首次接待没配欢迎语时，转接也不能补发目标客服的个人问候。
func TestPersonalWelcomeWithoutFirstRuleNeverAddedOnTransfer(t *testing.T) {
	f := newFixture(t)
	f.online(f.owner)
	f.online(f.agent)
	createPersonalWelcome(t, f, f.agent, "all", "不应在转接触发", 0)
	_, _, conversation := f.visitor("")
	personalClaim(t, f, conversation, f.owner)
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND trigger_key LIKE 'personal-welcome:%'", conversation) != 0 {
		t.Fatal("首次没配置的会话被转接追加个人欢迎")
	}
}

// TestPersonalWelcomeImmediateConfirmAndScenario 验证自动与确认接待入口、具体营业场景优先、重复执行不重发、规则更新后旧任务取消。
func TestPersonalWelcomeImmediateConfirmAndScenario(t *testing.T) {
	for _, mode := range []string{"auto", "confirm"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			f.online(f.agent)
			setAutomationPolicy(t, f, map[string]any{"business_hours_enabled": true, "weekly_hours": []any{}})
			status, reply := f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": false})
			mustData(t, status, reply)
			createPersonalWelcome(t, f, f.agent, "all", "全部场景", 0)
			rule := createPersonalWelcome(t, f, f.agent, "outside", "非营业具体场景", 0)
			if _, err := f.admin.Exec(ctx, "UPDATE routing_pools SET algorithm='round_robin',assignment_mode=$2 WHERE id=$1", f.poolID, mode); err != nil {
				t.Fatal(err)
			}
			_, _, conversation := f.visitor("")
			if mode == "confirm" {
				if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1", conversation) != 0 {
					t.Fatal("未确认接待不得发送个人欢迎")
				}
				status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/accept", f.agent, "", map[string]any{})
				mustData(t, status, reply)
			}
			runWorkers(t, f.s, f.tenant)
			runWorkers(t, f.s, f.tenant)
			if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND content='非营业具体场景'", conversation) != 1 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND content='全部场景'", conversation) != 0 {
				t.Fatal("个人场景优先级或重试去重错误")
			}
			status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", f.agent, "", map[string]any{})
			mustData(t, status, reply)
			_, _, next := f.visitor("")
			if mode == "confirm" {
				status, reply = f.request("POST", "/api/v1/conversations/"+next+"/assignments/accept", f.agent, "", map[string]any{})
				mustData(t, status, reply)
			}
			status, reply = f.request("PATCH", "/api/v1/me/welcome-rules/"+rule["id"].(string), f.agent, "", map[string]any{"name": "更新欢迎", "scenario": "outside", "enabled": true, "version": rule["version"], "contents": []any{map[string]any{"type": "text", "content": "新欢迎"}}})
			mustData(t, status, reply)
			runWorkers(t, f.s, f.tenant)
			if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='cancelled'", next) != 1 {
				t.Fatal("个人欢迎改版旧任务没有取消")
			}
		})
	}
}

// TestPersonalKeywordPriorityIndependenceAndRevoke 验证个人关键词独立开关、统一优先级、同级公共优先、他人排除和撤权后旧任务撤销。
func TestPersonalKeywordPriorityIndependenceAndRevoke(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	createPersonalKeyword(t, f, f.agent, "价格", "个人高优先", 20)
	createPersonalKeyword(t, f, f.owner, "价格", "他人不得使用", 100)
	status, reply := f.request("POST", "/api/v1/tenant/keyword-rules", f.owner, "", map[string]any{"name": "公共价格", "pattern": "价格", "reply": "公共低优先", "priority": 10, "cooldown_seconds": 0})
	team := mustData(t, status, reply)
	token, _, conversation := f.visitor("")
	personalClaim(t, f, conversation, f.agent)
	send := func(text string) {
		status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": text})
		mustData(t, status, reply)
	}
	send("价格")
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND content='个人高优先'", conversation) != 1 {
		t.Fatal("个人高优先级未胜出或错误用了他人规则")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/keyword-rules/"+team["id"].(string), f.owner, "", map[string]any{"priority": 20})
	mustData(t, status, reply)
	send("价格")
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND content='公共低优先'", conversation) != 1 {
		t.Fatal("相同优先级没有优先公共规则")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"keywords_enabled": false})
	mustData(t, status, reply)
	send("价格")
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND content='个人高优先'", conversation) != 2 {
		t.Fatal("团队总开关错误关闭了个人规则")
	}
	send("价格")
	if _, err := f.admin.Exec(ctx, "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND job_type='keyword' AND status='cancelled'", conversation) != 1 {
		t.Fatal("客服撤权后的个人关键词任务未撤销")
	}
}

// TestPersonalWelcomeMediaOwnershipAndPublicPreview 验证普通客服可精确预览启用公共欢迎图片，却不能将他人附件转入个人欢迎。
func TestPersonalWelcomeMediaOwnershipAndPublicPreview(t *testing.T) {
	f := newFixture(t)
	image := uploadWelcomeImage(t, f, f.owner)
	status, _ := f.request("GET", "/api/v1/attachments/"+image, f.agent, "", nil)
	if status == 200 {
		t.Fatal("未共享的图片不应可读取")
	}
	status, reply := f.request("POST", "/api/v1/tenant/welcome-rules", f.owner, "", map[string]any{"name": "图片欢迎", "scope_type": "tenant", "contents": []any{map[string]any{"type": "image", "attachment_id": image}}})
	shared := mustData(t, status, reply)
	status, _ = f.request("GET", "/api/v1/attachments/"+image, f.agent, "", nil)
	if status != 200 {
		t.Fatal("开启公共欢迎图片无法只读预览")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": false})
	mustData(t, status, reply)
	status, _ = f.request("GET", "/api/v1/attachments/"+image, f.agent, "", nil)
	if status == 200 {
		t.Fatal("关闭公共欢迎总开关后媒体授权没有撤回")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": true})
	mustData(t, status, reply)
	status, _ = f.request("POST", "/api/v1/me/welcome-rules", f.agent, "", map[string]any{"name": "窃取公共附件", "contents": []any{map[string]any{"type": "image", "attachment_id": image}}})
	if status != 403 {
		t.Fatal("个人欢迎不能借用他人上传的媒体")
	}
	status, reply = f.request("DELETE", "/api/v1/tenant/welcome-rules/"+shared["id"].(string), f.owner, "", map[string]any{"version": shared["version"]})
	mustData(t, status, reply)
	status, _ = f.request("GET", "/api/v1/attachments/"+image, f.agent, "", nil)
	if status == 200 {
		t.Fatal("归档公共规则后图片授权未撤回")
	}
}

// TestPersonalTasksNeverReviveAfterTransferBack 验证转回原客服也不能恢复原接待记录的迟到个人欢迎和关键词任务。
func TestPersonalTasksNeverReviveAfterTransferBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	f.online(f.owner)
	status, reply := f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"welcome_enabled": false, "keywords_enabled": false})
	mustData(t, status, reply)
	createPersonalWelcome(t, f, f.agent, "all", "原接待过期欢迎", 60)
	createPersonalKeyword(t, f, f.agent, "转回", "原接待过期关键词", 1)
	token, _, conversation := f.visitor("")
	personalClaim(t, f, conversation, f.agent)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "转回"})
	mustData(t, status, reply)
	for _, pair := range [][2]string{{f.agent, f.owner}, {f.owner, f.agent}} {
		status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", pair[0], "", map[string]any{"agent_id": pair[1]})
		mustData(t, status, reply)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND status='cancelled'", conversation) != 2 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='system'", conversation) != 0 {
		t.Fatal("转回原客服后迟到的旧个人任务重新生效")
	}
}

// TestPersonalWelcomeUpdateDisableAndScenarioCancellation 验证本人开关完整更新、版本冲突，以及营业场景变化后旧问候撤销。
func TestPersonalWelcomeUpdateDisableAndScenarioCancellation(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	setAutomationPolicy(t, f, map[string]any{"business_hours_enabled": true, "weekly_hours": []any{}})
	rule := createPersonalWelcome(t, f, f.agent, "outside", "场景变化应取消", 60)
	status, reply := f.request("PATCH", "/api/v1/me/welcome-rules/"+rule["id"].(string), f.agent, "", map[string]any{"version": rule["version"], "name": "已关闭", "scenario": "outside", "enabled": false, "contents": rule["contents"]})
	disabled := mustData(t, status, reply)
	if disabled["enabled"] != false || disabled["version"] != float64(2) {
		t.Fatal("欢迎语开关或版本未保存")
	}
	status, _ = f.request("DELETE", "/api/v1/me/welcome-rules/"+rule["id"].(string), f.agent, "", map[string]any{"version": rule["version"]})
	if status != 409 {
		t.Fatal("过期欢迎语版本不能删除新版本")
	}
	status, reply = f.request("PATCH", "/api/v1/me/welcome-rules/"+rule["id"].(string), f.agent, "", map[string]any{"version": disabled["version"], "name": "已开启", "scenario": "outside", "enabled": true, "contents": rule["contents"]})
	mustData(t, status, reply)
	_, _, conversation := f.visitor("")
	personalClaim(t, f, conversation, f.agent)
	setAutomationPolicy(t, f, map[string]any{"business_hours_enabled": false})
	if _, err := f.admin.Exec(context.Background(), "UPDATE chat_operation_jobs SET run_after=now()-interval '1 second' WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	runWorkers(t, f.s, f.tenant)
	if f.countRows("SELECT count(*) FROM chat_operation_jobs WHERE conversation_id=$1 AND trigger_key LIKE 'personal-welcome:%' AND status='cancelled'", conversation) != 1 {
		t.Fatal("营业场景切换未取消旧个人欢迎")
	}
}
