// 文件作用说明：按显式开关启动隔离schema的真实登录浏览器验收服务，结束后删除全部测试身份和素材。
package chat

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/auth"
	"supportdesk/backend/internal/billing"
	"supportdesk/backend/internal/decoration"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
)

// TestAIBrowserHarness 只在显式验收开关下监听独立端口，账号不允许用于真实环境。
func TestAIBrowserHarness(t *testing.T) {
	if os.Getenv("GOODCHAT_AI_BROWSER_RUN") != "1" {
		t.Skip("仅显式浏览器验收时启动")
	}
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var e error
	var role string
	if e = f.pool.QueryRow(ctx, "SELECT current_user").Scan(&role); e != nil {
		t.Fatal(e)
	}
	quoted := pgx.Identifier{f.schema}.Sanitize()
	role = pgx.Identifier{role}.Sanitize()
	if _, e = f.admin.Exec(ctx, "GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA "+quoted+" TO "+role+";GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA "+quoted+" TO "+role); e != nil {
		t.Fatal(e)
	}
	config := platform.Config{AuthSecret: strings.Repeat("browser-acceptance-", 3), DataDir: t.TempDir(), PublicURL: "http://127.0.0.1:13227", CodeOrigin: "http://localhost:18228", CallbackAllowLocal: true}
	identity, e := auth.New(f.pool, config)
	if e != nil {
		t.Fatal(e)
	}
	password := "GoodChat-AI-Local-2026!"
	hashed, e := auth.HashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	ownerUser := f.principals[f.owner].UserID
	agentUser := f.principals[f.agent].UserID
	if _, e = f.admin.Exec(ctx, "UPDATE users SET password_hash=$1,is_platform_admin=(id=$2) WHERE id IN($2,$3)", hashed, ownerUser, agentUser); e != nil {
		t.Fatal(e)
	}
	f.server.Close()
	f.s = New(f.pool, identity, config)
	plans := billing.New(f.pool, identity, config)
	if e = pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error { return plans.EnsureTenant(ctx, tx, f.tenant) }); e != nil {
		t.Fatal(e)
	}
	f.s.SetFeatureChecker(plans.CheckFeature)
	designs, e := decoration.New(f.pool, identity, config)
	if e != nil {
		t.Fatal(e)
	}
	// 供应商明确模拟；登录、租户、持久消息、素材及计费使用真实业务代码。
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if len(in.Messages) > 0 && strings.Contains(string(jsonValue(in.Messages[0].Content)), "知识库生成使用场景描述") {
			aiFixtureResponse(w, "", nil, "用于安装、售前和售后问题的隔离验收知识库。", true)
		} else if os.Getenv("GOODCHAT_AI_INBOX_BROWSER") == "1" && len(in.Messages) > 0 && in.Messages[len(in.Messages)-1].Role == "user" && strings.Contains(string(jsonValue(in.Messages[len(in.Messages)-1].Content)), "知识补充验收") {
			aiFixtureResponse(w, "report_knowledge_gap", aiGapInput{Reason: "隔离浏览器验收：缺少公开演示代号"}, "", true)
		} else {
			aiFixtureResponse(w, "submit_reply", aiPlan{Items: []aiReplyItem{{Type: "text", Text: "这是隔离验收供应商返回的AI回复。"}}}, "", true)
		}
	}))
	defer provider.Close()
	modelID := newID()
	model := aiModel{ID: modelID, Name: "浏览器隔离验收模型", BaseURL: provider.URL + "/v1", Model: "isolated-fixture", Enabled: true, Version: 1, CostInput: 1000000, CostOutput: 4000000, SaleInput: 2000000, SaleOutput: 8000000, MaxInput: 64000, MaxOutput: 1024}
	key := "isolated-fixture-key"
	if privatePath := os.Getenv("GOODCHAT_AI_BROWSER_MODEL_CONFIG"); privatePath != "" {
		raw, err := os.ReadFile(privatePath)
		if err != nil {
			t.Fatal("无法读取浏览器私有模型配置")
		}
		var config struct {
			Endpoint       string `json:"endpoint"`
			Key            string `json:"key"`
			Model          string `json:"model"`
			CostInput      int64  `json:"cost_input"`
			CostOutput     int64  `json:"cost_output"`
			SaleInput      int64  `json:"sale_input"`
			SaleOutput     int64  `json:"sale_output"`
			SupportsImages bool   `json:"supports_images"`
		}
		if json.Unmarshal(raw, &config) != nil || config.Key == "" {
			t.Fatal("浏览器私有模型配置无效")
		}
		model.Name = "真实AI浏览器验收（隔离租户）"
		model.BaseURL = strings.TrimSuffix(config.Endpoint, "/chat/completions")
		model.Model = config.Model
		model.CostInput = config.CostInput
		model.CostOutput = config.CostOutput
		model.SaleInput = config.SaleInput
		model.SaleOutput = config.SaleOutput
		model.SupportsImages = config.SupportsImages
		key = config.Key
	}
	secret, e := f.s.aiSecret(key, modelID, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(ctx, "INSERT INTO ai_chat_models(id,name,enabled) VALUES($1,$2,true)", modelID, model.Name); e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(ctx, "INSERT INTO ai_chat_model_versions(model_id,version,config,secret_ciphertext) VALUES($1,1,$2,$3)", modelID, jsonValue(model), secret); e != nil {
		t.Fatal(e)
	}
	if _, e = f.admin.Exec(ctx, "INSERT INTO ai_wallets(tenant_id,balance_micro) VALUES($1,10000000)", f.tenant); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("GOODCHAT_AI_INBOX_BROWSER") == "1" {
		b := defaultAIBot()
		b.Mode = "auto"
		b.ModelID = modelID
		b.Name = "隔离验收机器人"
		var membership string
		if e = f.admin.QueryRow(ctx, "SELECT id::text FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2", f.tenant, ownerUser).Scan(&membership); e != nil {
			t.Fatal(e)
		}
		var bot string
		if e = f.admin.QueryRow(ctx, `INSERT INTO ai_bots(tenant_id,owner_membership_id,model_id,name,mode,settings) VALUES($1,$2,$3,$4,'auto',$5) RETURNING id::text`, f.tenant, membership, modelID, b.Name, jsonValue(b.Settings)).Scan(&bot); e != nil {
			t.Fatal(e)
		}
		if _, e = f.admin.Exec(ctx, `INSERT INTO ai_tenant_settings(tenant_id,default_bot_id,reception_mode) VALUES($1,$2,'ai_first')`, f.tenant, bot); e != nil {
			t.Fatal(e)
		}
	}
	mux := http.NewServeMux()
	identity.Register(mux)
	plans.Register(mux)
	f.s.Register(mux)
	designs.Register(mux)
	listener, e := net.Listen("tcp", "127.0.0.1:18227")
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewUnstartedServer(localization.Middleware(mux))
	server.Listener = listener
	server.Start()
	f.server = server
	f.s.Start(ctx)
	identity.Start(ctx)
	root := os.Getenv("GOODCHAT_AI_BROWSER_OUTPUT")
	if root == "" {
		t.Fatal("缺少浏览器私有输出目录")
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	credentials := map[string]any{"owner_email": "chat-test-" + ownerUser + "@example.invalid", "agent_email": "chat-test-" + agentUser + "@example.invalid", "password": password, "tenant_id": f.tenant, "entry_key": f.entry, "model_id": modelID}
	path := filepath.Join(root, "browser-credentials.json")
	if e = os.WriteFile(path, jsonValue(credentials), 0600); e != nil {
		t.Fatal(e)
	}
	defer os.Remove(path)
	t.Log("隔离浏览器验收后端已启动18227；临时凭证保存在私有目录")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			if _, e = os.Stat(filepath.Join(root, "stop-browser")); e == nil {
				return
			}
		case <-timeout.C:
			t.Fatal("浏览器验收超时")
		}
	}
}
