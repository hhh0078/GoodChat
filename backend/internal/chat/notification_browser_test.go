// 文件作用说明：显式启动邮件通知的隔离数据库与真实登录浏览器验收服务，SMTP受理为本地替身，生产发送另行验收。
package chat

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/auth"
	"supportdesk/backend/internal/billing"
	"supportdesk/backend/internal/decoration"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/notifications"
	"supportdesk/backend/internal/platform"
	"supportdesk/backend/internal/reporting"
)

// TestNotificationBrowserHarness 仅监听本机独立端口，使用随机schema与无法用于生产的测试身份。
func TestNotificationBrowserHarness(t *testing.T) {
	if os.Getenv("GOODCHAT_NOTIFICATION_BROWSER_RUN") != "1" {
		t.Skip("仅显式浏览器验收时启动")
	}
	t.Setenv("GOODCHAT_AI_BROWSER_RUN", "1")
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := platform.Config{AuthSecret: strings.Repeat("notice-browser-local-", 3), DataDir: t.TempDir(), PublicURL: "http://127.0.0.1:13238", CodeOrigin: "http://localhost:18239", CallbackAllowLocal: true, SMTPHost: "smtp.fixture.invalid", SMTPPort: "587", SMTPFrom: "notice@goodchat.test"}
	identity, e := auth.New(f.pool, cfg)
	if e != nil {
		t.Fatal(e)
	}
	password := "GoodChat-Notice-Local-2026!"
	hash, e := auth.HashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	owner := f.principals[f.owner].UserID
	agent := f.principals[f.agent].UserID
	if _, e := f.admin.Exec(ctx, "UPDATE users SET password_hash=$1,is_platform_admin=(id=$2) WHERE id IN($2,$3)", hash, owner, agent); e != nil {
		t.Fatal(e)
	}
	var captureLock sync.Mutex
	capturePath := filepath.Join("..", "..", "..", ".local", "notifications", "browser-mailbox.jsonl")
	notices := notifications.New(f.pool, identity, cfg, func(ctx context.Context, to string, mail notifications.Mail) (string, string) {
		captureLock.Lock()
		defer captureLock.Unlock()
		file, e := os.OpenFile(capturePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if e != nil {
			return "failed", "configuration"
		}
		defer file.Close()
		if e := json.NewEncoder(file).Encode(map[string]any{"recipient": to, "mail": mail}); e != nil {
			return "failed", "configuration"
		}
		return "accepted", ""
	})
	notices.MailStatus = identity.MailStatus
	identity.Notify = notifications.Enqueue
	identity.RenderMail = notices.RenderMail
	plans := billing.New(f.pool, identity, cfg)
	if e := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error { return plans.EnsureTenant(ctx, tx, f.tenant) }); e != nil {
		t.Fatal(e)
	}
	f.server.Close()
	messaging := New(f.pool, identity, cfg)
	messaging.SetLeadNotifier(notices.SendLegacyLead)
	designs, e := decoration.New(f.pool, identity, cfg)
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	identity.Register(mux)
	notices.Register(mux)
	plans.Register(mux)
	messaging.Register(mux)
	designs.Register(mux)
	reporting.New(f.pool, identity).Register(mux)
	listener, e := net.Listen("tcp", "127.0.0.1:13239")
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Handler: localization.Middleware(mux), ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	notices.Start(ctx)
	defer func() { cancel(); notices.Wait() }()
	var email, agentEmail string
	f.admin.QueryRow(ctx, "SELECT email FROM users WHERE id=$1", owner).Scan(&email)
	f.admin.QueryRow(ctx, "SELECT email FROM users WHERE id=$1", agent).Scan(&agentEmail)
	output := filepath.Join("..", "..", "..", ".local", "notifications", "browser-account.json")
	raw, _ := json.Marshal(map[string]string{"email": email, "password": password, "tenant_id": f.tenant, "agent_email": agentEmail, "agent_password": password})
	if e := os.WriteFile(output, raw, 0600); e != nil {
		t.Fatal(e)
	}
	defer os.Remove(output)
	stop := filepath.Join("..", "..", "..", ".local", "notifications", "stop-browser")
	t.Log("本机邮件浏览器验收后端已就绪，端口13239，未发送真实邮件")
	for {
		if _, e := os.Stat(stop); e == nil {
			os.Remove(stop)
			return
		}
		time.Sleep(time.Second)
	}
}
