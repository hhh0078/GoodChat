// 文件作用说明：通过真实本地HTTP与WebSocket验证桌面配置、幂等重试、凭证隔离和原子恢复，不向生产发送测试消息。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type memoryVault struct {
	mu    sync.Mutex
	items map[string]string
}

// Get 提供测试专用凭据库，不访问用户的系统钥匙串。
func (v *memoryVault) Get(service, key string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	value, ok := v.items[service+key]
	if !ok {
		return "", errors.New("不存在")
	}
	return value, nil
}

// Set 保存测试Cookie，验证它不会进入配置或消息数据库。
func (v *memoryVault) Set(service, key, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.items == nil {
		v.items = map[string]string{}
	}
	v.items[service+key] = value
	return nil
}

// Delete 删除测试设备凭据。
func (v *memoryVault) Delete(service, key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.items, service+key)
	return nil
}

// newTestClient 创建完整本地存储和原生网络模块，结束时停止全部后台任务。
func newTestClient(t *testing.T, server string, emit func(string, any)) *Client {
	t.Helper()
	c, err := New(t.TempDir(), "2.3.4", server, &memoryVault{}, emit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if err = c.SelectServer(server); err != nil {
		t.Fatal(err)
	}
	return c
}

// activateForQueue 将测试身份绑定到确定的分区，单独检验持久发送而不启动后台任务。
func activateForQueue(t *testing.T, c *Client, tenant string) *session {
	t.Helper()
	s, err := c.current()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.user = "account-a"
	s.tenant = tenant
	s.member = "member-a"
	s.scope = scopeHash(s.server, s.user, tenant)
	s.mu.Unlock()
	return s
}

// sendLocal 模拟嵌入页面的同源请求，消息仍通过真实原生HTTP连接发送。
func sendLocal(c *Client, method, path, tenant string, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://wails.localhost"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tenant-ID", tenant)
	w := httptest.NewRecorder()
	c.Middleware(http.NotFoundHandler()).ServeHTTP(w, r)
	return w
}

// TestServerSelection 验证版本、安装包默认值、记住的地址及安全地址边界。
func TestServerSelection(t *testing.T) {
	for _, input := range []string{"https://user:pass@example.com", "http://example.com", "https://example.com/api", "https://example.com?secret=1", "https://example.com/#x", "file:///tmp/secret", "https://example.com/%2f", "https://example.com\\evil"} {
		if _, err := NormalizeServer(input); err == nil {
			t.Errorf("错误接受地址 %q", input)
		}
	}
	c := newTestClient(t, "https://default.example", nil)
	initial, err := c.Startup()
	if err != nil || initial.Version != "2.3.4" || initial.Server != "https://default.example" {
		t.Fatalf("默认配置错误：%+v %v", initial, err)
	}
	if err = c.SelectServer("https://Custom.Example/"); err != nil {
		t.Fatal(err)
	}
	next, err := c.Startup()
	if err != nil || next.Server != "https://custom.example" || next.DefaultServer != "https://default.example" {
		t.Fatalf("自定义配置错误：%+v %v", next, err)
	}
	if err = c.SelectServer("https://new.example"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Startup(); err != nil {
		t.Fatal("第二次原子替换失败：", err)
	}
}

// TestDurableRetryAfterLostAcknowledgement 验证服务器已保存但响应中断时重启仍只产生一条业务消息。
func TestDurableRetryAfterLostAcknowledgement(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	var attempts atomic.Int32
	var saved sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/conversations/conversation-a/messages" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			ID      string `json:"client_message_id"`
			Content string `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		saved.LoadOrStore(body.ID, body.Content)
		if attempts.Add(1) == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		fmt.Fprintf(w, `{"data":{"id":"saved-message","client_message_id":%q,"content":%q}}`, body.ID, body.Content)
	}))
	defer server.Close()
	dir := t.TempDir()
	vault := &memoryVault{}
	c, err := New(dir, "1.0.0", server.URL, vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.SelectServer(server.URL); err != nil {
		t.Fatal(err)
	}
	s := activateForQueue(t, c, "tenant-a")
	body := fmt.Sprintf(`{"client_message_id":%q,"content":"真实网络响应中断测试","message_type":"text","audience":"public"}`, id)
	w := sendLocal(c, "POST", "/api/v1/conversations/conversation-a/messages", "tenant-a", body)
	if w.Code != 503 {
		t.Fatalf("首次发送应保留未确认：%d %s", w.Code, w.Body.String())
	}
	items, err := c.Pending()
	if err != nil || len(items) != 1 || items[0].ID != id {
		t.Fatalf("消息未落盘：%+v %v", items, err)
	}
	oldScope := s.scope
	c.Close()
	c, err = New(dir, "1.0.1", server.URL, vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.SelectServer(server.URL); err != nil {
		t.Fatal(err)
	}
	s = activateForQueue(t, c, "tenant-a")
	if s.scope != oldScope {
		t.Fatal("升级后分区发生变化")
	}
	if err = c.flush(s.ctx, s, "tenant-a", s.scope); err != nil {
		t.Fatal(err)
	}
	items, err = c.Pending()
	if err != nil || len(items) != 0 {
		t.Fatalf("确认后未清理队列：%+v %v", items, err)
	}
	count := 0
	saved.Range(func(any, any) bool { count++; return true })
	if attempts.Load() != 2 || count != 1 {
		t.Fatalf("发送次数=%d，业务消息数=%d", attempts.Load(), count)
	}
}

// TestIsolationAndDeterministicFailure 验证切换服务器或租户不发送旧队列，业务拒绝后不自动重试。
func TestIsolationAndDeterministicFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(403)
		fmt.Fprint(w, `{"code":"forbidden","message":"已失去接待权限"}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL, nil)
	s := activateForQueue(t, c, "tenant-a")
	w := sendLocal(c, "POST", "/api/v1/conversations/a/messages", "tenant-a", `{"client_message_id":"11111111-1111-4111-8111-111111111111","content":"a"}`)
	if w.Code != 403 {
		t.Fatalf("未保留业务拒绝：%d", w.Code)
	}
	if err := c.flush(s.ctx, s, "tenant-a", s.scope); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("业务拒绝后仍自动重试")
	}
	oldScope := s.scope
	s = activateForQueue(t, c, "tenant-b")
	if err := c.flush(s.ctx, s, "tenant-b", s.scope); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("租户之间混发")
	}
	if err := c.SelectServer("https://new.example"); err != nil {
		t.Fatal(err)
	}
	items, err := c.Pending()
	if err != nil || len(items) != 0 {
		t.Fatal("新服务器看到旧消息")
	}
	old, err := c.store.listPending(oldScope)
	if err != nil || len(old) != 1 || old[0].State != "failed" {
		t.Fatal("原分区失败消息丢失")
	}
}

// TestCredentialAndHeaderBoundary 验证Cookie只保存在安全存储，不跟随跳转或跨服务器转移。
func TestCredentialAndHeaderBoundary(t *testing.T) {
	secret := "test-only-session-cookie"
	var leaked atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			leaked.Store(true)
		}
		fmt.Fprint(w, `{"data":{}}`)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "support_session", Value: secret, Path: "/", HttpOnly: true, MaxAge: 3600})
			fmt.Fprint(w, `{"data":{}}`)
			return
		}
		if r.URL.Path == "/api/v1/redirect" {
			http.Redirect(w, r, destination.URL+"/api/v1/probe", 302)
			return
		}
		if r.Header.Get("Idempotency-Key") != "stable-key" {
			t.Error("幂等请求头未透传")
		}
		fmt.Fprint(w, `{"data":{}}`)
	}))
	defer source.Close()
	c := newTestClient(t, source.URL, nil)
	w := sendLocal(c, "POST", "/api/v1/auth/login", "", `{}`)
	if w.Code != 200 || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("Cookie泄露到界面或登录失败")
	}
	r := httptest.NewRequest("POST", "http://wails.localhost/api/v1/probe", bytes.NewReader([]byte("{}")))
	r.Header.Set("Idempotency-Key", "stable-key")
	c.Middleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), r)
	s, _ := c.current()
	result, err := c.request(s, "GET", "/api/v1/redirect", "", nil, "")
	if err != nil || result.Status != 302 {
		t.Fatal("重定向未被阻止")
	}
	if err = c.SelectServer(destination.URL); err != nil {
		t.Fatal(err)
	}
	sendLocal(c, "GET", "/api/v1/probe", "", "")
	if leaked.Load() {
		t.Fatal("Cookie跨服务器泄露")
	}
	for _, name := range []string{"settings.json", "messages.sqlite"} {
		raw, err := os.ReadFile(filepath.Join(c.dir, name))
		if err != nil || bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("本地普通文件包含凭证：%s", name)
		}
	}
	r = httptest.NewRequest("POST", "http://wails.localhost/api/v1/auth/login", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	c.Middleware(http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("未拒绝跨来源本机调用")
	}
}

// TestAtomicRecoveryCheckpoint 验证无效事件导致整个事务回滚，游标不会跨过未保存的数据。
func TestAtomicRecoveryCheckpoint(t *testing.T) {
	c := newTestClient(t, "https://example.com", nil)
	if err := c.store.checkpoint("scope", "conversation", "first", []json.RawMessage{json.RawMessage(`{"id":"a"}`)}, true); err != nil {
		t.Fatal(err)
	}
	err := c.store.checkpoint("scope", "conversation", "second", []json.RawMessage{json.RawMessage(`{"id":"b"}`), json.RawMessage(`{}`)}, false)
	if err == nil {
		t.Fatal("无效事件没有中止事务")
	}
	cursor, err := c.store.cursor("scope", "conversation")
	if err != nil || cursor != "first" {
		t.Fatal("事务失败后游标错误前进")
	}
	var count int
	c.store.db.QueryRow("SELECT count(*) FROM events WHERE scope='scope'").Scan(&count)
	if count != 1 {
		t.Fatal("事务未完整回滚")
	}
}

// TestNativeWebSocketWithoutRenderer 验证没有任何网页时仍可接收、同步，并在票据拒绝后停止重连。
func TestNativeWebSocketWithoutRenderer(t *testing.T) {
	var tickets atomic.Int32
	var deny atomic.Bool
	var connections atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"user":{"id":"account"},"memberships":[{"id":"member","tenant_id":"tenant"}]}}`)
	})
	mux.HandleFunc("/api/v1/realtime/ticket", func(w http.ResponseWriter, r *http.Request) {
		tickets.Add(1)
		if deny.Load() {
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, `{"data":{"url":"/ws/v1?ticket=test-once"}}`)
	})
	mux.HandleFunc("/api/v1/agent/conversations", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":{"items":[],"has_more":false}}`) })
	mux.HandleFunc("/ws/v1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		connections.Add(1)
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"ready"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"event","data":{"id":"event","event_type":"message.created","conversation_id":"conversation"}}`))
		for {
			if _, _, err = conn.Read(r.Context()); err != nil {
				return
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	received := make(chan struct{}, 1)
	c := newTestClient(t, server.URL, func(name string, _ any) {
		if name == "desktop:event" {
			select {
			case received <- struct{}{}:
			default:
			}
		}
	})
	if err := c.SetIdentity("tenant"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("没有界面时原生模块未收到消息")
	}
	if connections.Load() != 1 {
		t.Fatal("原生连接重复创建")
	}
	deny.Store(true)
	c.Reconnect()
	deadline := time.Now().Add(5 * time.Second)
	for c.Status().Status != "denied" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if c.Status().Status != "denied" {
		t.Fatal("权限失效未停止连接")
	}
	before := tickets.Load()
	time.Sleep(1200 * time.Millisecond)
	if tickets.Load() != before {
		t.Fatal("权限失效后继续重连")
	}
}

// TestSyncCursorExpiryRecovery 验证过期游标重建快照，再补拉快照水位之后的事件。
func TestSyncCursorExpiryRecovery(t *testing.T) {
	var snapshot atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			snapshot.Add(1)
			fmt.Fprint(w, `{"data":{"items":[{"id":"message-a"}],"sync_cursor":"fresh"}}`)
			return
		}
		if r.URL.Query().Get("after_cursor") == "expired" {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"code":"resync_required"}`)
			return
		}
		fmt.Fprint(w, `{"data":{"items":[{"id":"event-b","event_type":"message.created"}],"next_cursor":"done","has_more":false}}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL, nil)
	s := activateForQueue(t, c, "tenant")
	if err := c.store.checkpoint(s.scope, "conversation", "expired", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := c.syncConversation(context.Background(), s, "tenant", s.scope, "conversation"); err != nil {
		t.Fatal(err)
	}
	cursor, _ := c.store.cursor(s.scope, "conversation")
	if cursor != "done" || snapshot.Load() != 1 {
		t.Fatal("未按新快照恢复游标")
	}
}

// TestLoginCancelsPreviousAccount 验证新账号登录开始前已经取消旧账号全部网络任务。
func TestLoginCancelsPreviousAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("新登录请求携带旧账号Cookie")
		}
		fmt.Fprint(w, `{"data":{}}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL, nil)
	old := activateForQueue(t, c, "tenant")
	u, _ := http.NewRequest("GET", server.URL, nil)
	old.http.Jar.SetCookies(u.URL, []*http.Cookie{{Name: "support_session", Value: "old-account", Path: "/"}})
	response := sendLocal(c, "POST", "/api/v1/auth/login", "", `{}`)
	if response.Code != 200 {
		t.Fatal("新登录请求失败")
	}
	if old.ctx.Err() == nil {
		t.Fatal("旧账号任务未取消")
	}
	next, _ := c.current()
	if next == old || next.scope != "" {
		t.Fatal("旧账号身份被复用")
	}
}
