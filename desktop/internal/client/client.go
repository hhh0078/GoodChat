// 文件作用说明：维护每次服务器选择的独立生命周期、系统凭证与已认证客服身份。
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/zalando/go-keyring"
	"golang.org/x/net/publicsuffix"
)

// Vault 将会话Cookie保存在系统钥匙串或Windows凭据管理器中。
type Vault interface {
	Get(string, string) (string, error)
	Set(string, string, string) error
	Delete(string, string) error
}

type systemVault struct{}

// Get 从操作系统读取认证秘密，不回传网页。
func (systemVault) Get(service, key string) (string, error) { return keyring.Get(service, key) }

// Set 只把认证秘密写入系统安全存储。
func (systemVault) Set(service, key, value string) error { return keyring.Set(service, key, value) }

// Delete 删除退出账号的设备凭据。
func (systemVault) Delete(service, key string) error { return keyring.Delete(service, key) }

// State 表示原生消息模块的真实连接和恢复状态。
type State struct {
	Scope   string `json:"scope"`
	Status  string `json:"status"`
	Detail  string `json:"detail"`
	Pending int    `json:"pending"`
	Failed  int    `json:"failed"`
}

// Client 持有本机配置与原生任务，所有网络请求绑定固定的会话对象。
type Client struct {
	mu                          sync.Mutex
	dir, version, defaultServer string
	store                       *Store
	vault                       Vault
	session                     *session
	emit                        func(string, any)
	notify                      func(string, bool, string, float64) error
	notificationsAllowed        func() bool
	background                  bool
	wg                          sync.WaitGroup
}

type session struct {
	ctx                         context.Context
	cancel                      context.CancelFunc
	server                      string
	http                        *http.Client
	mu                          sync.Mutex
	identityMu                  sync.Mutex
	credentialMu                sync.Mutex
	flushMu                     sync.Mutex
	syncMu                      sync.Mutex
	user, tenant, member, scope string
	runCancel                   context.CancelFunc
	conn                        *websocket.Conn
	ready                       bool
	dirty                       map[string]uint64
	revision                    uint64
	rescan                      bool
	state                       State
	wake                        chan struct{}
}

// New 创建本机客户端；测试可以注入内存凭据库与事件接收器。
func New(dir, version, defaultServer string, vault Vault, emit func(string, any)) (*Client, error) {
	server, err := NormalizeServer(defaultServer)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	store, err := openStore(dir)
	if err != nil {
		return nil, err
	}
	if vault == nil {
		vault = systemVault{}
	}
	if emit == nil {
		emit = func(string, any) {}
	}
	return &Client{dir: dir, version: version, defaultServer: server, store: store, vault: vault, emit: emit}, nil
}

// Startup 每次显示服务器选择，已保存的合法选择优先于打包默认值。
func (c *Client) Startup() (Startup, error) {
	result := Startup{Version: c.version, DefaultServer: c.defaultServer, Server: c.defaultServer}
	raw, err := os.ReadFile(filepath.Join(c.dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, errors.New("无法读取本机服务器配置")
	}
	var cfg Config
	if json.Unmarshal(raw, &cfg) != nil {
		return result, errors.New("本机服务器配置损坏，请重新选择并保存")
	}
	if cfg.Server, err = NormalizeServer(cfg.Server); err != nil {
		return result, err
	}
	result.Server = cfg.Server
	return result, nil
}

// SelectServer 停止旧服务器任务并保存新选择，不向新服务器转发旧Cookie。
func (c *Client) SelectServer(value string) error {
	server, err := NormalizeServer(value)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err = writeConfig(c.dir, server); err != nil {
		return errors.New("保存服务器地址失败")
	}
	if c.session != nil {
		c.session.cancel()
	}
	s := newSession(server)
	c.session = s
	if value, err := c.vault.Get("GoodChat", scopeHash(server)); err == nil {
		var cookies []*http.Cookie
		if json.Unmarshal([]byte(value), &cookies) == nil {
			u, _ := url.Parse(server)
			for _, cookie := range cookies {
				if cookie.Name == "support_session" && (cookie.Expires.IsZero() || cookie.Expires.After(time.Now())) {
					s.http.Jar.SetCookies(u, []*http.Cookie{cookie})
				}
			}
		}
	}
	return nil
}

// newSession 创建独立Cookie容器与取消上下文，身份切换不能复用旧任务的认证信息。
func newSession(server string) *session {
	ctx, cancel := context.WithCancel(context.Background())
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	s := &session{ctx: ctx, cancel: cancel, server: server, wake: make(chan struct{}, 1), state: State{Status: "idle"}}
	s.http = &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return s
}

// beginLogin 在账号认证请求之前取消旧任务并换新Cookie容器，防止账号切换时混发。
func (c *Client) beginLogin() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return errors.New("请先选择服务器")
	}
	old := c.session
	old.cancel()
	c.session = newSession(old.server)
	return nil
}

// current 取得固定会话引用，切换服务器不会改变正在执行请求的目标。
func (c *Client) current() (*session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return nil, errors.New("请先选择服务器")
	}
	return c.session, nil
}

// SetIdentity 经服务端核验当前账号与所选租户后启动原生消息处理。
func (c *Client) SetIdentity(tenant string) error {
	s, err := c.current()
	if err != nil {
		return err
	}
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	result, err := c.request(s, http.MethodGet, "/api/v1/auth/me", tenant, nil, "")
	if err != nil {
		return err
	}
	if result.Status != 200 {
		return errors.New("登录已失效，请重新登录")
	}
	var payload struct {
		Data struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Memberships []struct {
				ID     string `json:"id"`
				Tenant string `json:"tenant_id"`
			} `json:"memberships"`
		} `json:"data"`
	}
	if json.Unmarshal(result.Body, &payload) != nil || payload.Data.User.ID == "" {
		return errors.New("服务器返回的账号信息无效")
	}
	member := ""
	for _, item := range payload.Data.Memberships {
		if item.Tenant == tenant {
			member = item.ID
		}
	}
	if member == "" {
		return errors.New("当前账号无权使用所选工作空间")
	}
	s.mu.Lock()
	if s.scope != "" && s.user == payload.Data.User.ID && s.tenant == tenant && s.state.Status != "denied" {
		s.mu.Unlock()
		return nil
	}
	if s.runCancel != nil {
		s.runCancel()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.runCancel = cancel
	s.user, s.tenant, s.member = payload.Data.User.ID, tenant, member
	s.scope = scopeHash(s.server, s.user, tenant)
	s.ready = false
	s.dirty = map[string]uint64{}
	s.rescan = true
	s.state = State{Status: "connecting", Detail: "正在连接消息服务"}
	s.mu.Unlock()
	c.mu.Lock()
	if c.session != s || s.ctx.Err() != nil {
		c.mu.Unlock()
		cancel()
		return errors.New("服务器已经切换，请重新选择工作空间")
	}
	c.wg.Add(1)
	c.mu.Unlock()
	go func() { defer c.wg.Done(); c.run(ctx, s, tenant, scopeHash(s.server, payload.Data.User.ID, tenant)) }()
	return nil
}

// Status 返回连接状态和当前身份的持久队列数量。
func (c *Client) Status() State {
	s, err := c.current()
	if err != nil {
		return State{Status: "idle"}
	}
	s.mu.Lock()
	state, scope := s.state, s.scope
	s.mu.Unlock()
	state.Scope = scope
	items, err := c.store.listPending(scope)
	if err != nil {
		state.Detail = "无法读取本地发送队列"
		return state
	}
	for _, item := range items {
		if item.State == "failed" {
			state.Failed++
		} else {
			state.Pending++
		}
	}
	return state
}

// Pending 返回当前已认证身份的本地未确认消息。
func (c *Client) Pending() ([]Pending, error) {
	s, err := c.current()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	scope := s.scope
	s.mu.Unlock()
	if scope == "" {
		return []Pending{}, nil
	}
	return c.store.listPending(scope)
}

// SetBackground 记录原生窗口是否可见，通知逻辑不依赖网页定时器。
func (c *Client) SetBackground(value bool) { c.mu.Lock(); c.background = value; c.mu.Unlock() }

// SetNotifier 安装由桌面平台提供的系统通知出口。
func (c *Client) SetNotifier(allowed func() bool, notify func(string, bool, string, float64) error) {
	c.mu.Lock()
	c.notify = notify
	c.notificationsAllowed = allowed
	c.mu.Unlock()
}

// Reconnect 通过取消当前连接立即重新申请票据，不复用一次性票据。
func (c *Client) Reconnect() {
	if s, err := c.current(); err == nil {
		s.mu.Lock()
		conn := s.conn
		s.mu.Unlock()
		if conn != nil {
			conn.CloseNow()
		}
		signal(s.wake)
	}
}

// Close 停止原生任务后关闭本机数据库，待发消息已持久化。
func (c *Client) Close() {
	c.mu.Lock()
	if c.session != nil {
		c.session.cancel()
	}
	c.mu.Unlock()
	c.wg.Wait()
	c.store.db.Close()
}

// signal 合并唤醒提示，避免网络抖动时积压重复同步任务。
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// setState 只发布当前身份的状态，旧任务不能覆盖切换后的界面。
func (c *Client) setState(ctx context.Context, s *session, status, detail string) {
	if ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	if ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	unchanged := s.state.Status == status && s.state.Detail == detail
	s.state = State{Status: status, Detail: detail}
	s.mu.Unlock()
	c.mu.Lock()
	current := c.session == s
	c.mu.Unlock()
	if current && !unchanged {
		c.emit("desktop:state", c.Status())
	}
}

// publish 过滤切换服务器或身份之前排队的旧事件，避免旧任务更新新工作空间。
func (c *Client) publish(ctx context.Context, s *session, scope, name string, value any) {
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	current := c.session == s
	c.mu.Unlock()
	s.mu.Lock()
	same := s.scope == scope
	s.mu.Unlock()
	if current && same {
		c.emit(name, value)
	}
}
