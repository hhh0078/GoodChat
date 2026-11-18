// 文件作用说明：验证AI钱包预占竞争、充值幂等、未知用量核对及共享媒体访问边界。
package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiPlatformAuthorizer 只为显式标记的隔离测试账号开放平台路由，租户授权仍走真实RLS。
type aiPlatformAuthorizer struct{ testAuthorizer }

// RequirePlatform 验证受控测试身份的平台标记。
func (a aiPlatformAuthorizer) RequirePlatform(next http.HandlerFunc) http.HandlerFunc {
	return a.RequireTenant(func(w http.ResponseWriter, r *http.Request) {
		if !platform.Current(r).PlatformAdmin {
			platform.ErrorJSON(w, r, platform.Fail(403, "forbidden", "没有权限"))
			return
		}
		next(w, r)
	})
}

// enableAITestPlatform 在隔离HTTP服务重建路由，普通客服保持无平台权限。
func enableAITestPlatform(f *fixture) {
	p := f.principals[f.owner]
	p.PlatformAdmin = true
	f.principals[f.owner] = p
	f.s.auth = aiPlatformAuthorizer{testAuthorizer{f.principals}}
	f.server.Close()
	mux := http.NewServeMux()
	f.s.Register(mux)
	f.server = httptest.NewServer(mux)
}

// TestAIOperationsUnlimited 验证运营租户余额为零也能按真实模拟用量结算，普通成员不能开启且欠费不能切回预付费。
func TestAIOperationsUnlimited(t *testing.T) {
	f := newFixture(t)
	setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		aiFixtureResponse(w, "", nil, "运营知识描述", true)
	})
	enableAITestPlatform(f)
	if _, err := f.admin.Exec(context.Background(), "UPDATE ai_wallets SET balance_micro=0"); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/platform/ai/wallets/" + f.tenant + "/mode"
	status, _ := f.request("PUT", path, f.agent, "", map[string]any{"unlimited": true})
	if status != 403 {
		t.Fatal("普通客服开启了不限制余额模式")
	}
	status, reply := f.request("PUT", path, f.owner, "", map[string]any{"unlimited": true})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/tenant/ai/knowledge", f.owner, "", map[string]any{"title": "运营知识", "body": "实际产品事实", "enabled": true})
	mustData(t, status, reply)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_wallets WHERE unlimited AND balance_micro=-800 AND reserved_micro=0") != 1 {
		t.Fatal("零余额运营费用未正确记账")
	}
	status, _ = f.request("PUT", path, f.owner, "", map[string]any{"unlimited": false})
	if status != 409 {
		t.Fatal("欠费不应切回要求非负余额的预付模式")
	}
}

// TestAIWalletCreditAndReconciliation 验证平台充值幂等及未知成本的实际核对路径。
func TestAIWalletCreditAndReconciliation(t *testing.T) {
	f := newFixture(t)
	setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) { aiFixtureResponse(w, "", nil, "描述", false) })
	enableAITestPlatform(f)
	key := newID()
	body := map[string]any{"amount_micro": 123456, "reason": "隔离测试入账", "client_key": key}
	for i := 0; i < 2; i++ {
		status, data := f.request("POST", "/api/v1/platform/ai/wallets/"+f.tenant+"/credit", f.owner, "", body)
		mustData(t, status, data)
	}
	if f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE client_key=$1", key) != 1 {
		t.Fatal("充值重复入账")
	}
	status, _ := f.request("POST", "/api/v1/platform/ai/wallets/"+f.tenant+"/credit", f.agent, "", body)
	if status != 403 {
		t.Fatalf("非平台账号充值被允许:%d", status)
	}
	body["amount_micro"] = 999
	status, _ = f.request("POST", "/api/v1/platform/ai/wallets/"+f.tenant+"/credit", f.owner, "", body)
	if status != 409 {
		t.Fatal("同编号不同金额未拒绝")
	}
	status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.owner, "", map[string]any{"title": "核对测试", "body": "售后政策", "enabled": true})
	mustData(t, status, data)
	runAITestJob(t, f)
	var job string
	if e := f.admin.QueryRow(context.Background(), "SELECT id::text FROM ai_jobs WHERE status='uncertain'").Scan(&job); e != nil {
		t.Fatal(e)
	}
	status, data = f.request("GET", "/api/v1/platform/ai/tenants/"+f.tenant+"/jobs", f.owner, "", nil)
	mustData(t, status, data)
	resolve := map[string]any{"action": "report", "reason": "供应商账单已确认", "client_key": newID(), "attempts": []any{map[string]any{"call_index": 1, "input_tokens": 100, "output_tokens": 25}}}
	for i := 0; i < 2; i++ {
		status, data = f.request("POST", "/api/v1/platform/ai/tenants/"+f.tenant+"/jobs/"+job+"/reconcile", f.owner, "", resolve)
		mustData(t, status, data)
	}
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE id=$1 AND status='failed' AND billing_status='reconciled' AND reserved_micro=0 AND cost_micro=200", job) != 1 {
		t.Fatal("实际成本未正确核对")
	}
	if f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE kind='charge'") != 0 {
		t.Fatal("失败任务不应补扣租户")
	}
	status, data = f.request("GET", "/api/v1/platform/ai/usage", f.owner, "", nil)
	mustData(t, status, data)
}

// TestAIConcurrentReservations 验证并发任务只能预占现有余额，不依赖工作器内存锁。
func TestAIConcurrentReservations(t *testing.T) {
	f := newFixture(t)
	setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) { aiFixtureResponse(w, "", nil, "描述", true) })
	if _, e := f.admin.Exec(context.Background(), "UPDATE ai_wallets SET balance_micro=1000"); e != nil {
		t.Fatal(e)
	}
	var group sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for n := 0; n < 8; n++ {
		group.Add(1)
		go func() {
			defer group.Done()
			e := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
				j := aiJobData{ID: newID(), Tenant: f.tenant, Kind: "summary", Snapshot: aiSnapshot{Model: aiModel{MaxInput: 400, MaxOutput: 100, SaleInput: 1000000, SaleOutput: 1000000}}}
				if _, e := tx.Exec(context.Background(), "INSERT INTO ai_jobs(id,tenant_id,kind,owner_membership_id,snapshot,status) VALUES($1,$2,'summary',$3,'{}','running')", j.ID, f.tenant, f.owner); e != nil {
					return e
				}
				return reserveAIWallet(context.Background(), tx, &j)
			})
			if e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	group.Wait()
	if success != 2 {
		t.Fatalf("1000微元应仅允许两次500微元预占，得到%d", success)
	}
}

// TestAIMediaSharingAndVisitorScope 验证团队媒体复用不会向其他租户或无关访客开放附件。
func TestAIMediaSharingAndVisitorScope(t *testing.T) {
	f := newFixture(t)
	raw, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==")
	status, data := uploadQuickReplyMedia(t, f, f.owner, "fixture.png", raw)
	if status != 201 {
		t.Fatalf("上传失败:%d", status)
	}
	attachment := data["data"].(map[string]any)["id"].(string)
	status, data = f.request("POST", "/api/v1/tenant/ai/media", f.owner, "", map[string]any{"title": "团队图片", "attachment_id": attachment})
	mustData(t, status, data)
	status, data = f.request("GET", "/api/v1/tenant/ai/media", f.agent, "", nil)
	if len(mustData(t, status, data)["items"].([]any)) != 1 {
		t.Fatal("同租户成员看不到团队素材")
	}
	status, data = f.request("GET", "/api/v1/tenant/ai/media", f.otherOwner, "", nil)
	items := mustData(t, status, data)["items"]
	if items != nil && len(items.([]any)) != 0 {
		t.Fatal("跨租户素材泄露")
	}
	for _, member := range []string{f.owner, f.agent} {
		status, _ = f.request("GET", "/api/v1/attachments/"+attachment, member, "", nil)
		if status != 200 {
			t.Fatalf("团队素材预览授权失败:%d", status)
		}
	}
	token, _, _ := f.visitor("")
	status, _ = f.request("GET", "/api/v1/attachments/"+attachment, "", token, nil)
	if status != 404 {
		t.Fatal("未收到素材的访客获得了读取权限")
	}
	status, _ = f.request("DELETE", "/api/v1/tenant/ai/media/"+attachment, f.agent, "", nil)
	if status != 403 {
		t.Fatal("成员删除了他人的素材")
	}
	// 普通私有上传不主动加入媒体库，其他成员不能读取。
	status, data = uploadQuickReplyMedia(t, f, f.owner, "private.png", raw)
	if status != 201 {
		t.Fatalf("上传失败:%d", status)
	}
	private := data["data"].(map[string]any)["id"].(string)
	status, _ = f.request("GET", "/api/v1/attachments/"+private, f.agent, "", nil)
	if status != 404 {
		t.Fatal("私有附件被媒体库授权扩大")
	}
}

// TestAIProviderRejectionDoesNotHoldFunds 验证明确拒绝调用时释放预算而不虚构未知费用。
func TestAIProviderRejectionDoesNotHoldFunds(t *testing.T) {
	f := newFixture(t)
	setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "Arrearage", "message": "fixture rejected"}})
	})
	status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.owner, "", map[string]any{"title": "售前", "body": "资料", "enabled": true})
	mustData(t, status, data)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE status='failed' AND reserved_micro=0 AND error_code='ai_provider_rejected'") != 1 || f.countRows("SELECT count(*) FROM ai_wallets WHERE reserved_micro=0") != 1 {
		var state string
		_ = f.admin.QueryRow(context.Background(), "SELECT status||':'||error_code||':'||reserved_micro::text FROM ai_jobs LIMIT 1").Scan(&state)
		t.Fatalf("明确拒绝不应长期冻结余额: %s", state)
	}
}

// TestAIModelVersionAndSecret 验证平台保存密钥不会回显，历史价格版本可继续审计。
func TestAIModelVersionAndSecret(t *testing.T) {
	f := newFixture(t)
	setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "OK"}}}, "", true)
	})
	enableAITestPlatform(f)
	status, data := f.request("GET", "/api/v1/platform/ai/models", f.owner, "", nil)
	items := mustData(t, status, data)["items"].([]any)
	if len(items) != 1 {
		t.Fatal("平台模型列表错误")
	}
	encoded := jsonValue(data)
	if bytes.Contains(encoded, []byte("fixture-key")) || bytes.Contains(encoded, []byte("secret_ciphertext")) {
		t.Fatal("平台接口泄露密钥")
	}
	item := items[0].(map[string]any)
	item["sale_input"] = float64(4000000)
	item["api_key"] = ""
	id := item["id"].(string)
	status, data = f.request("PATCH", "/api/v1/platform/ai/models/"+id, f.owner, "", item)
	mustData(t, status, data)
	status, _ = f.request("PATCH", "/api/v1/platform/ai/models/"+id, f.owner, "", item)
	if status != 409 {
		t.Fatal("模型配置未检查并发版本")
	}
	if f.countRows("SELECT count(*) FROM ai_chat_model_versions WHERE model_id=$1", id) != 2 {
		t.Fatal("未保留历史配置版本")
	}
	status, data = f.request("POST", "/api/v1/platform/ai/models/"+id+"/test", f.owner, "", map[string]any{})
	mustData(t, status, data)
}

// TestAIKnowledgeRestoreAndProtocolRepair 验证知识回滚、不可变账本和由平台承担的单次协议修复。
func TestAIKnowledgeRestoreAndProtocolRepair(t *testing.T) {
	f := newFixture(t)
	var calls int
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			aiFixtureResponse(w, "", nil, "适用于隔离知识库的使用描述。", true)
			return
		}
		if calls == 3 {
			aiFixtureResponse(w, "", nil, "未按工具格式输出的模拟回答", true)
			return
		}
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "修复后的合法回答"}}}, "", true)
	})
	status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.agent, "", map[string]any{"title": "售前", "body": "版本一", "enabled": true})
	id := mustData(t, status, data)["id"].(string)
	runAITestJob(t, f)
	status, data = f.request("PATCH", "/api/v1/tenant/ai/knowledge/"+id, f.agent, "", map[string]any{"title": "售前", "body": "版本二", "enabled": true, "version": 1})
	mustData(t, status, data)
	runAITestJob(t, f)
	status, data = f.request("GET", "/api/v1/tenant/ai/knowledge/"+id+"/versions", f.agent, "", nil)
	if len(mustData(t, status, data)["items"].([]any)) != 2 {
		t.Fatal("知识发布历史不完整")
	}
	status, data = f.request("POST", "/api/v1/tenant/ai/knowledge/"+id+"/restore", f.agent, "", map[string]any{"version": 1, "expected_version": 2})
	mustData(t, status, data)
	if f.countRows("SELECT count(*) FROM ai_knowledge_bases WHERE id=$1 AND draft_body='版本一' AND published_body='版本一' AND version=3 AND published_version=3", id) != 1 {
		t.Fatal("知识回滚未原子恢复")
	}
	b := defaultAIBot()
	b.Mode = "assistant"
	b.ModelID = model
	status, data = f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", b)
	mustData(t, status, data)
	status, data = f.request("POST", "/api/v1/me/ai/test", f.agent, "", map[string]any{"text": "问候"})
	if status != 202 {
		t.Fatal("调试任务创建失败")
	}
	job := data["data"].(map[string]any)["id"].(string)
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE id=$1 AND status='succeeded' AND input_tokens=400 AND cost_micro=800 AND sale_micro=800", job) != 1 {
		t.Fatal("协议修复未保留全部成本或重复向租户计费")
	}
	if f.countRows("SELECT count(*) FROM ai_call_attempts WHERE job_id=$1 AND sale_micro=0 AND response_meta->>'platform_covered'='true'", job) != 1 {
		t.Fatal("无效调用未标记平台承担")
	}
	if _, e := f.admin.Exec(context.Background(), "UPDATE ai_chat_model_versions SET config=config WHERE model_id=$1", model); e == nil {
		t.Fatal("模型价格版本允许原地覆盖")
	}
	if _, e := f.admin.Exec(context.Background(), "UPDATE ai_wallet_entries SET reason='rewrite' WHERE tenant_id=$1", f.tenant); e == nil {
		t.Fatal("资金流水允许原地覆盖")
	}
}

// TestAIVisitorImageInputIsBilled 验证访客图片触发真实消息链并进入Eino多模态请求，输入Token含图片且无按张重复扣款。
func TestAIVisitorImageInputIsBilled(t *testing.T) {
	f := newFixture(t)
	var imageSeen bool
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		_ = json.NewDecoder(r.Body).Decode(&raw)
		imageSeen = strings.Contains(string(jsonValue(raw)), "image_url")
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "模拟识别：图片为红色。"}}}, "", true)
	})
	b := defaultAIBot()
	b.Mode = "auto"
	b.ModelID = model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", b)
	mustData(t, status, data)
	token, _, conversation := f.visitor("")
	f.online(f.agent)
	personalClaim(t, f, conversation, f.agent)
	image, _ := base64.StdEncoding.DecodeString(aiProbeImage())
	id := uploadAIVisitorImage(t, f, token, image)
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "message_type": "image", "attachment_id": id, "content": "是什么颜色？"})
	mustData(t, status, data)
	runAITestJob(t, f)
	if !imageSeen || f.countRows("SELECT count(*) FROM ai_jobs WHERE conversation_id=$1 AND status='succeeded' AND image_inputs=1 AND input_tokens=200 AND sale_micro=800 AND snapshot::text NOT LIKE '%base64%'", conversation) != 1 {
		t.Fatal("图片输入、Token计费或私有快照不正确")
	}
	if f.countRows("SELECT count(*) FROM ai_wallet_entries WHERE kind='charge' AND delta_micro=-800") != 1 {
		t.Fatal("图片被额外按张收费或遗漏输入计费")
	}
}

// TestAIDefaultBotHonorsSpecifiedAgent 验证租户默认AI不能覆盖入口明确指定的其他客服。
func TestAIDefaultBotHonorsSpecifiedAgent(t *testing.T) {
	f := newFixture(t)
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "测试"}}}, "", true)
	})
	b := defaultAIBot()
	b.Mode = "auto"
	b.ModelID = model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	bot := mustData(t, status, data)["id"].(string)
	status, data = f.request("PUT", "/api/v1/tenant/ai/settings", f.owner, "", map[string]any{"default_bot_id": bot, "reception_mode": "ai_first"})
	mustData(t, status, data)
	token, _, conversation := f.visitor("")
	if _, e := f.admin.Exec(context.Background(), "UPDATE conversations SET desired_agent_id=$2,agent_mode='strict',context=context-'ai_receiving' WHERE id=$1", conversation, f.agent); e != nil {
		t.Fatal(e)
	}
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "指定客服的问题"})
	mustData(t, status, data)
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE conversation_id=$1", conversation) != 0 {
		t.Fatal("默认AI抢占了指定客服的会话")
	}
	// 移除业务组权限后，即使不再指定其他客服，默认AI也不可读取该入口消息。
	if _, e := f.admin.Exec(context.Background(), "UPDATE conversations SET desired_agent_id=NULL WHERE id=$1", conversation); e != nil {
		t.Fatal(e)
	}
	if _, e := f.admin.Exec(context.Background(), "DELETE FROM agent_group_members WHERE membership_id=$1", f.owner); e != nil {
		t.Fatal(e)
	}
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "业务组权限已撤销"})
	mustData(t, status, data)
	if f.countRows("SELECT count(*) FROM ai_jobs WHERE conversation_id=$1", conversation) != 0 {
		t.Fatal("默认AI越过业务组权限")
	}
	f.online(f.agent)
	personalClaim(t, f, conversation, f.agent)
}

// TestAIBudgetLockAllowsNewMessages 验证预算锁不阻塞引用同一机器人的新消息任务，避免与结算外键形成死锁。
func TestAIBudgetLockAllowsNewMessages(t *testing.T) {
	f := newFixture(t)
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "测试"}}}, "", true)
	})
	b := defaultAIBot()
	b.Mode = "auto"
	b.ModelID = model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	bot := mustData(t, status, data)["id"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	j := aiJobData{ID: newID(), Tenant: f.tenant, Kind: "test", BotID: bot, Snapshot: aiSnapshot{Model: aiModel{MaxInput: 400, MaxOutput: 100, SaleInput: 1000000, SaleOutput: 1000000}}}
	if _, e := f.admin.Exec(ctx, `INSERT INTO ai_jobs(id,tenant_id,kind,bot_id,owner_membership_id,snapshot,status) VALUES($1,$2,'test',$3,$4,'{}','running')`, j.ID, f.tenant, bot, f.owner); e != nil {
		t.Fatal(e)
	}
	tx, e := f.admin.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if e = reserveAIWallet(ctx, tx, &j); e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(ctx, `INSERT INTO ai_jobs(tenant_id,kind,bot_id,owner_membership_id,snapshot) VALUES($1,'test',$2,$3,'{}')`, f.tenant, bot, f.owner); e != nil {
		t.Fatalf("预算锁阻塞了新任务的外键: %v", e)
	}
}

// TestAIBudgetUsesReservationDay 验证昨天排队但今天才调用的任务也占用今天业务时区的日预算。
func TestAIBudgetUsesReservationDay(t *testing.T) {
	f := newFixture(t)
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "测试"}}}, "", true)
	})
	b := defaultAIBot()
	b.Mode = "auto"
	b.ModelID = model
	b.Settings.DailyBudget = 1000
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", b)
	bot := mustData(t, status, data)["id"].(string)
	ctx := context.Background()
	if _, e := f.admin.Exec(ctx, `INSERT INTO ai_jobs(tenant_id,kind,bot_id,owner_membership_id,snapshot,status,sale_micro,created_at) VALUES($1,'test',$2,$3,jsonb_build_object('budget_at',now()),'succeeded',700,now()-interval '1 day')`, f.tenant, bot, f.owner); e != nil {
		t.Fatal(e)
	}
	e := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		j := aiJobData{ID: newID(), Tenant: f.tenant, Kind: "test", BotID: bot, Snapshot: aiSnapshot{Model: aiModel{MaxInput: 100, MaxOutput: 25, SaleInput: 1000000, SaleOutput: 1000000}}}
		if _, e := tx.Exec(ctx, `INSERT INTO ai_jobs(id,tenant_id,kind,bot_id,owner_membership_id,snapshot,status) VALUES($1,$2,'test',$3,$4,'{}','running')`, j.ID, f.tenant, bot, f.owner); e != nil {
			return e
		}
		return reserveAIWallet(ctx, tx, &j)
	})
	var typed *platform.APIError
	if !errors.As(e, &typed) || typed.Code != "ai_daily_limit" {
		t.Fatalf("跨午夜任务绕过当日预算: %v", e)
	}
}

// TestAIKnowledgeReadLimitRepair 验证模型超量选库时不读取正文，仅修复一次且无效调用不向租户收费。
func TestAIKnowledgeReadLimitRepair(t *testing.T) {
	f := newFixture(t)
	ids := []string{}
	reads := 0
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Tools []any `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		if len(input.Tools) == 0 {
			aiFixtureResponse(w, "", nil, "产品使用说明。", true)
			return
		}
		reads++
		if reads == 1 {
			aiFixtureResponse(w, "", nil, "<function_call>\n"+string(jsonValue(map[string]any{"name": "read_knowledge_bases", "arguments": map[string]any{"ids": ids}}))+"\n</function>\n</tool_call>", true)
		} else if reads == 2 {
			aiFixtureResponse(w, "read_knowledge_bases", map[string]any{"ids": ids[:3]}, "", true)
		} else {
			aiFixtureResponse(w, "", nil, "<tool_call>"+string(jsonValue(map[string]any{"name": "submit_reply", "arguments": aiPlan{Items: []aiReplyItem{{Type: "text", Text: "已根据相关知识回答"}}}}))+"</tool_call>", true)
		}
	})
	for i := 0; i < 4; i++ {
		status, data := f.request("POST", "/api/v1/tenant/ai/knowledge", f.agent, "", map[string]any{"title": "产品知识", "body": "已发布的产品事实", "enabled": true})
		ids = append(ids, mustData(t, status, data)["id"].(string))
		runAITestJob(t, f)
	}
	bot := defaultAIBot()
	bot.Mode = "assistant"
	bot.ModelID = model
	bot.KnowledgeIDs = ids
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.agent, "", bot)
	mustData(t, status, data)
	status, data = f.request("POST", "/api/v1/me/ai/test", f.agent, "", map[string]any{"text": "产品怎么使用？"})
	if status != 202 {
		t.Fatal("调试任务未创建")
	}
	job := data["data"].(map[string]any)["id"].(string)
	runAITestJob(t, f)
	if reads != 3 || f.countRows("SELECT count(*) FROM ai_jobs WHERE id=$1 AND status='succeeded' AND cost_micro=1200 AND sale_micro=1600", job) != 1 {
		t.Fatal("超量选库未在有界调用内恢复，或费用未正确归属")
	}
	if f.countRows("SELECT count(*) FROM ai_call_attempts WHERE job_id=$1 AND call_index=1 AND sale_micro=0 AND response_meta->>'platform_covered'='true' AND response_meta->'knowledge_id_counts'='[4]'::jsonb", job) != 1 {
		t.Fatal("无效读取的安全诊断或费用豁免缺失")
	}
	var raw []byte
	if err := f.admin.QueryRow(context.Background(), "SELECT result FROM ai_jobs WHERE id=$1", job).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan aiPlan
	if json.Unmarshal(raw, &plan) != nil || len(plan.ReadIDs) != 3 {
		t.Fatal("超量读取不应产生知识读取副作用")
	}
}

// TestAIThinkingLifecycle 验证入队立即发布固定200秒输入态，完成和失败清除，旧任务不能清除新提示。
func TestAIThinkingLifecycle(t *testing.T) {
	f := newFixture(t)
	broken := false
	model := setupAITest(t, f, func(w http.ResponseWriter, r *http.Request) {
		if broken {
			aiFixtureResponse(w, "", nil, "不符合工具协议的模拟响应", true)
			return
		}
		aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "您好，可以帮助您配置客服。"}}}, "", true)
	})
	bot := defaultAIBot()
	bot.Mode = "auto"
	bot.ModelID = model
	status, data := f.request("PUT", "/api/v1/me/ai/bot", f.owner, "", bot)
	botID := mustData(t, status, data)["id"].(string)
	status, data = f.request("PUT", "/api/v1/tenant/ai/settings", f.owner, "", map[string]any{"default_bot_id": botID, "reception_mode": "ai_first"})
	mustData(t, status, data)
	token, _, conversation := f.visitor("")
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "你好"})
	mustData(t, status, data)
	if f.countRows(`SELECT count(*) FROM conversations WHERE id=$1 AND context#>>'{ai_thinking,mode}'='auto' AND (context#>>'{ai_thinking,expires_at}')::timestamptz BETWEEN now()+interval '195 seconds' AND now()+interval '201 seconds'`, conversation) != 1 {
		t.Fatal("入队未建立固定200秒输入态")
	}
	var oldID string
	if e := f.admin.QueryRow(context.Background(), "SELECT context#>>'{ai_thinking,job_id}' FROM conversations WHERE id=$1", conversation).Scan(&oldID); e != nil {
		t.Fatal(e)
	}
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND NOT(context ? 'ai_thinking') AND NOT ai_paused", conversation) != 1 {
		t.Fatal("成功回复后未结束输入态")
	}
	broken = true
	status, data = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "再问一个问题"})
	mustData(t, status, data)
	if e := platform.TenantTx(context.Background(), f.s.pool, f.tenant, func(tx pgx.Tx) error {
		return f.s.aiThinking(context.Background(), tx, &aiJobData{ID: oldID, Tenant: f.tenant, Kind: "reply", Conversation: conversation}, false)
	}); e != nil {
		t.Fatal(e)
	}
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND context ? 'ai_thinking'", conversation) != 1 {
		t.Fatal("旧任务清除了更新的输入态")
	}
	runAITestJob(t, f)
	if f.countRows("SELECT count(*) FROM conversations WHERE id=$1 AND NOT(context ? 'ai_thinking') AND ai_paused", conversation) != 1 {
		t.Fatal("失败转人工后输入态未结束")
	}
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND source='ai' AND content LIKE '请稍等%'", conversation) != 1 {
		t.Fatal("转人工未发送面向客户的等待话术")
	}
}
