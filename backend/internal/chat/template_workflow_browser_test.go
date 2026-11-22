// 文件作用说明：按需提供真实认证、数据库、装修发布和聊天的隔离浏览器验收服务，不接触生产数据。
package chat

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
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

// TestTemplateWorkflowBrowserRuntime 启动可真实导入、发布和发送消息的本机环境，停止后回收随机schema。
func TestTemplateWorkflowBrowserRuntime(t *testing.T) {
	output := os.Getenv("GOODCHAT_TEMPLATE_WORKFLOW_STATE")
	if output == "" {
		t.Skip("仅显式模板流程浏览器验收启动")
	}
	t.Setenv("GOODCHAT_AI_BROWSER_RUN", "1")
	f := newFixture(t)
	ctx := context.Background()
	cfg := platform.Config{AuthSecret: strings.Repeat("template-workflow-local-", 3), DataDir: t.TempDir(), PublicURL: "http://127.0.0.1:13346", CodeOrigin: "http://localhost:18348", CallbackAllowLocal: true}
	identity, err := auth.New(f.pool, cfg)
	if err != nil {
		t.Fatal(err)
	}
	password := "Template-Local-Workflow-2026!"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	user := f.principals[f.owner].UserID
	if _, err = f.admin.Exec(ctx, "UPDATE users SET password_hash=$1 WHERE id=$2", hash, user); err != nil {
		t.Fatal(err)
	}
	plans := billing.New(f.pool, identity, cfg)
	if err = pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error { return plans.EnsureTenant(ctx, tx, f.tenant) }); err != nil {
		t.Fatal(err)
	}
	messaging := New(f.pool, identity, cfg)
	designs, err := decoration.New(f.pool, identity, cfg)
	if err != nil {
		t.Fatal(err)
	}
	messaging.SetEntryDecoration(designs.EntryDecoration)
	mux := http.NewServeMux()
	identity.Register(mux)
	plans.Register(mux)
	messaging.Register(mux)
	designs.Register(mux)
	var servers []*http.Server
	for _, addr := range []string{"127.0.0.1:18347", "127.0.0.1:18348"} {
		listener, e := net.Listen("tcp", addr)
		if e != nil {
			t.Fatal(e)
		}
		handler := http.Handler(localization.Middleware(platform.MethodOverride(mux)))
		if strings.HasSuffix(addr, ":18348") {
			handler = designs.CodeHandler()
		}
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		servers = append(servers, server)
		go server.Serve(listener)
	}
	defer func() {
		for _, server := range servers {
			server.Close()
		}
	}()
	var email, link string
	if err = f.admin.QueryRow(ctx, "SELECT email FROM users WHERE id=$1", user).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if err = f.admin.QueryRow(ctx, "SELECT id::text FROM access_links WHERE tenant_id=$1 AND entry_key=$2", f.tenant, f.entry).Scan(&link); err != nil {
		t.Fatal(err)
	}
	fields := []formField{{ID: newID(), Label: "姓名", Required: true, Type: "text"}, {ID: newID(), Label: "需求说明", Type: "textarea"}}
	form := createQuickReply(t, f, f.owner, map[string]any{"title": "模板流程登记", "scope": "public", "message_type": "form", "form_fields": fields})
	options := createQuickReply(t, f, f.owner, map[string]any{"title": "请选择服务", "scope": "public", "message_type": "text", "content": "请选择服务", "options": []any{map[string]any{"id": "product", "label": "产品咨询", "value": "我想了解产品"}, map[string]any{"id": "support", "label": "售后服务", "value": "我需要售后服务"}}})
	raw, _ := json.Marshal(map[string]any{"email": email, "password": password, "tenant": f.tenant, "entry": f.entry, "link": link, "form": form, "options": options})
	if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, raw, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(output)
	t.Log("模板流程隔离服务就绪，后端18347，渲染18348")
	timer := time.NewTimer(90 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("模板流程验收超时")
		case <-ticker.C:
			if _, e := os.Stat(output + ".done"); e == nil {
				os.Remove(output + ".done")
				return
			}
		}
	}
}
