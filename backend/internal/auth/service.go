// 文件作用说明：装配认证路由、校验Cookie与租户权限，并为首次注册和成员激活提供同事务默认资源初始化。
package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

const sessionCookie = "support_session"
const sessionTTL = 7 * 24 * time.Hour

// Service 保存身份模块依赖；敏感密钥不通过API或日志返回。
type Service struct {
	pool             *pgxpool.Pool
	cfg              platform.Config
	secret           []byte
	mailer           *smtpMailer
	dummyHash        string
	passwordSlots    chan struct{}
	initializeTenant func(context.Context, pgx.Tx, string, string) error
	checkSeats       func(context.Context, pgx.Tx, string, string, string, int) error
}

// Membership 表示当前账号有效的租户关系，角色与接待资格分别返回。
type Membership struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	Role       string `json:"role"`
	CanServe   bool   `json:"can_serve"`
}

// accountView 为认证成功及me提供一致的账号与租户结构。
type accountView struct {
	User struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		DisplayName   string `json:"display_name"`
		PlatformAdmin bool   `json:"is_platform_admin"`
	} `json:"user"`
	Memberships    []Membership `json:"memberships"`
	ActiveTenantID string       `json:"active_tenant_id"`
}

// New 创建认证服务，生产需要显式密钥，开发密钥以0600持久化保存。
func New(pool *pgxpool.Pool, cfg platform.Config) (*Service, error) {
	if pool == nil {
		return nil, errors.New("认证模块需要PostgreSQL连接池")
	}
	secret, err := loadAuthSecret(cfg)
	if err != nil {
		return nil, err
	}
	dummy, err := hashPassword("GoodChat-invalid-account-password")
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, cfg: cfg, secret: secret, mailer: &smtpMailer{cfg: cfg}, dummyHash: dummy, passwordSlots: make(chan struct{}, 2)}, nil
}

// loadAuthSecret 读取独立服务端密钥，避免六位验证码摘要被离线枚举。
func loadAuthSecret(cfg platform.Config) ([]byte, error) {
	if len(cfg.AuthSecret) >= 32 {
		return []byte(cfg.AuthSecret), nil
	}
	if cfg.Production || cfg.AuthSecret != "" {
		return nil, errors.New("SUPPORT_AUTH_SECRET至少需要32字节")
	}
	if cfg.DataDir == "" {
		return nil, errors.New("开发环境需要DataDir以安全保存认证密钥")
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(cfg.DataDir, "auth-secret")
	if value, err := os.ReadFile(path); err == nil {
		if len(value) < 32 {
			return nil, errors.New("开发认证密钥文件无效")
		}
		if err := os.Chmod(path, 0600); err != nil {
			return nil, err
		}
		return value, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	value, err := platform.Token()
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	_, writeErr := file.WriteString(value)
	closeErr := file.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return []byte(value), nil
}

// SetTenantInitializer 设置同事务业务默认资源回调，最后两个参数是租户ID和users.id。
func (s *Service) SetTenantInitializer(initialize func(context.Context, pgx.Tx, string, string) error) {
	s.initializeTenant = initialize
}

// SetSeatChecker 注入套餐席位校验，排除原成员和原邀请后检查新增有效席位。
func (s *Service) SetSeatChecker(check func(context.Context, pgx.Tx, string, string, string, int) error) {
	s.checkSeats = check
}

// seats 在持有租户行锁的事务中检查接待成员及待激活邀请占用。
func (s *Service) seats(ctx context.Context, tx pgx.Tx, tenantID, memberID, inviteID string, additional int) error {
	if s.checkSeats != nil {
		return s.checkSeats(ctx, tx, tenantID, memberID, inviteID, additional)
	}
	return nil
}

// initialize 在当前事务设置租户上下文后初始化默认服务资源和成员接待关系。
func (s *Service) initialize(ctx context.Context, tx pgx.Tx, tenantID, userID string) error {
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID); err != nil {
		return err
	}
	if s.initializeTenant != nil {
		return s.initializeTenant(ctx, tx, tenantID, userID)
	}
	return nil
}

// Start 定期清理过期认证凭证，不触碰账号、租户或聊天历史。
func (s *Service) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.pool.Exec(ctx, "DELETE FROM auth_rate_limits WHERE expires_at < now()-interval '1 day'")
				_, _ = s.pool.Exec(ctx, "DELETE FROM email_challenges WHERE expires_at < now()-interval '7 days'")
				_, _ = s.pool.Exec(ctx, "DELETE FROM auth_sessions WHERE expires_at < now()-interval '7 days'")
			}
		}
	}()
}

// Register 为共享ServeMux注册身份和租户成员接口。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/send-code", s.endpoint(s.sendCode))
	mux.HandleFunc("POST /api/v1/auth/register", s.endpoint(s.registerAccount))
	mux.HandleFunc("POST /api/v1/auth/login-code", s.endpoint(s.loginCode))
	mux.HandleFunc("POST /api/v1/auth/login", s.endpoint(s.loginPassword))
	mux.HandleFunc("POST /api/v1/auth/reset-password", s.endpoint(s.resetPassword))
	mux.HandleFunc("POST /api/v1/auth/accept-invitation", s.endpoint(s.acceptInvitation))
	mux.HandleFunc("GET /api/v1/auth/me", s.RequireUser(s.endpoint(s.me)))
	mux.HandleFunc("POST /api/v1/auth/logout", s.RequireUser(s.endpoint(s.logout)))
	mux.HandleFunc("POST /api/v1/auth/logout-others", s.RequireUser(s.endpoint(s.logoutOthers)))
	mux.HandleFunc("GET /api/v1/tenant/members", s.RequireAdmin(s.endpoint(s.members)))
	mux.HandleFunc("PATCH /api/v1/tenant/members/{id}", s.RequireAdmin(s.endpoint(s.updateMember)))
	mux.HandleFunc("DELETE /api/v1/tenant/members/{id}", s.RequireAdmin(s.endpoint(s.removeMember)))
	mux.HandleFunc("GET /api/v1/tenant/invitations", s.RequireAdmin(s.endpoint(s.invitations)))
	mux.HandleFunc("POST /api/v1/tenant/invitations", s.RequireAdmin(s.endpoint(s.createInvitation)))
	mux.HandleFunc("POST /api/v1/tenant/invitations/{id}/resend", s.RequireAdmin(s.endpoint(s.resendInvitation)))
	mux.HandleFunc("DELETE /api/v1/tenant/invitations/{id}", s.RequireAdmin(s.endpoint(s.revokeInvitation)))
	mux.HandleFunc("POST /api/v1/tenant/transfer-ownership", s.RequireAdmin(s.endpoint(s.transferOwnership)))
	mux.HandleFunc("POST /api/v1/platform/tenants", s.RequirePlatform(s.endpoint(s.platformCreateTenant)))
	mux.HandleFunc("POST /api/v1/platform/tenants/{id}/resend-invitation", s.RequirePlatform(s.endpoint(s.platformResendInitialInvitation)))
}

// endpoint 统一执行写请求来源检查与安全错误输出。
func (s *Service) endpoint(handler func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.checkOrigin(r); err != nil {
			platform.ErrorJSON(w, r, err)
			return
		}
		if err := handler(w, r); err != nil {
			platform.ErrorJSON(w, r, err)
		}
	}
}

// checkOrigin 拒绝跨站写入，直接API调用不因缺少浏览器专用头而被误拒。
func (s *Service) checkOrigin(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return nil
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return platform.Fail(403, "invalid_origin", "请从GoodChat页面执行此操作")
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	got, err := url.Parse(origin)
	want, wantErr := url.Parse(s.cfg.PublicURL)
	if err != nil || wantErr != nil || got.User != nil || got.RawQuery != "" || got.Fragment != "" || got.Path != "" || got.Scheme != want.Scheme || !strings.EqualFold(got.Host, want.Host) {
		return platform.Fail(403, "invalid_origin", "请求来源不被允许")
	}
	return nil
}

// RequireUser 验证服务端登录会话并为请求注入当前账号及有效租户关系。
func (s *Service) RequireUser(next http.HandlerFunc) http.HandlerFunc { return s.require(next, "user") }

// RequireTenant 要求当前账号在所选租户内具有有效成员关系。
func (s *Service) RequireTenant(next http.HandlerFunc) http.HandlerFunc {
	return s.require(next, "tenant")
}

// RequireAdmin 要求当前租户的所有者或管理员权限，不由平台标记隐式绕过。
func (s *Service) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.require(next, "admin")
}

// RequirePlatform 要求受控初始化的平台管理员身份。
func (s *Service) RequirePlatform(next http.HandlerFunc) http.HandlerFunc {
	return s.require(next, "platform")
}

// require 将四种权限要求集中到同一实时数据库校验路径。
func (s *Service) require(next http.HandlerFunc, scope string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.checkOrigin(r); err != nil {
			platform.ErrorJSON(w, r, err)
			return
		}
		principal, err := s.authenticate(r)
		if err == nil && (scope == "tenant" || scope == "admin") && principal.TenantID == "" {
			err = platform.Fail(403, "tenant_required", "没有可用的租户成员身份")
		}
		if err == nil && scope == "admin" && principal.Role != "owner" && principal.Role != "admin" {
			err = platform.Fail(403, "admin_required", "此操作需要租户管理员权限")
		}
		if err == nil && scope == "platform" && !principal.PlatformAdmin {
			err = platform.Fail(403, "platform_required", "此操作需要平台管理员权限")
		}
		if err != nil {
			platform.ErrorJSON(w, r, err)
			return
		}
		next(w, r.WithContext(platform.WithPrincipal(r.Context(), principal)))
	}
}

// authenticate 每次验证会话及成员有效性，停用后旧Cookie不能继续访问租户。
func (s *Service) authenticate(r *http.Request) (platform.Principal, error) {
	principal := platform.Principal{}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 43 {
		return principal, platform.Fail(401, "unauthenticated", "请先登录")
	}
	if _, err := base64.RawURLEncoding.DecodeString(cookie.Value); err != nil {
		return principal, platform.Fail(401, "unauthenticated", "请重新登录")
	}
	err = s.pool.QueryRow(r.Context(), `SELECT u.id::text,u.email,u.display_name,u.is_platform_admin FROM auth_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now()`, digestToken(cookie.Value)).Scan(&principal.UserID, &principal.Email, &principal.DisplayName, &principal.PlatformAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return principal, platform.Fail(401, "unauthenticated", "登录已失效，请重新登录")
	}
	if err != nil {
		return principal, err
	}
	view, err := s.account(r.Context(), principal.UserID, r.Header.Get("X-Tenant-ID"))
	if err != nil {
		return principal, err
	}
	for _, member := range view.Memberships {
		if member.TenantID == view.ActiveTenantID {
			principal.TenantID, principal.MembershipID, principal.TenantName = member.TenantID, member.ID, member.TenantName
			principal.Role, principal.CanServe = member.Role, member.CanServe
		}
	}
	return principal, nil
}

// account 查询本人有效成员列表并校验显式选择的租户，不信任请求体租户标记。
func (s *Service) account(ctx context.Context, userID, selected string) (accountView, error) {
	view := accountView{Memberships: []Membership{}}
	if selected != "" && !validID(selected) {
		return view, platform.Fail(400, "invalid_tenant", "租户标识无效")
	}
	if err := s.pool.QueryRow(ctx, "SELECT id::text,email,display_name,is_platform_admin FROM users WHERE id=$1", userID).Scan(&view.User.ID, &view.User.Email, &view.User.DisplayName, &view.User.PlatformAdmin); err != nil {
		return view, err
	}
	rows, err := s.pool.Query(ctx, `SELECT m.id::text,m.tenant_id::text,t.name,m.role,m.can_serve FROM tenant_memberships m JOIN tenants t ON t.id=m.tenant_id WHERE m.user_id=$1 AND NOT m.disabled AND m.removed_at IS NULL AND t.status='active' ORDER BY m.created_at,m.id`, userID)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var member Membership
		if err := rows.Scan(&member.ID, &member.TenantID, &member.TenantName, &member.Role, &member.CanServe); err != nil {
			return view, err
		}
		view.Memberships = append(view.Memberships, member)
	}
	if err := rows.Err(); err != nil {
		return view, err
	}
	if len(view.Memberships) > 0 {
		view.ActiveTenantID = view.Memberships[0].TenantID
	}
	if selected != "" {
		view.ActiveTenantID = ""
		for _, member := range view.Memberships {
			if member.TenantID == selected {
				view.ActiveTenantID = selected
				break
			}
		}
		if view.ActiveTenantID == "" {
			return view, platform.Fail(403, "tenant_forbidden", "无权访问此租户或成员已停用")
		}
	}
	return view, nil
}

// setSessionCookie 设置host-only且不可被脚本读取的登录Cookie。
func (s *Service) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()), HttpOnly: true, Secure: s.cfg.Production || strings.HasPrefix(s.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode})
}

// clearSessionCookie 清除与设置时范围一致的登录Cookie。
func (s *Service) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.Production || strings.HasPrefix(s.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode})
}

// createSession 在业务事务中创建登录凭证，只把摘要写入数据库。
func (s *Service) createSession(ctx context.Context, tx pgx.Tx, userID string) (string, error) {
	token, err := platform.Token()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, "INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '7 days')", userID, digestToken(token))
	return token, err
}

// finishLogin 在事务提交后设置Cookie并返回完整身份，避免回滚后的假登录。
func (s *Service) finishLogin(w http.ResponseWriter, r *http.Request, userID, token, tenantID string) error {
	view, err := s.account(r.Context(), userID, tenantID)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, token)
	platform.JSON(w, 200, view)
	return nil
}

// audit 将成员操作写入同一事务，不记录认证秘密。
func audit(ctx context.Context, tx pgx.Tx, tenantID, userID, action, targetID string) error {
	_, err := tx.Exec(ctx, "INSERT INTO identity_audit_logs(tenant_id,actor_id,action,target_id) VALUES($1,$2,$3,$4)", tenantID, userID, action, targetID)
	return err
}

// lockIdentity 串行化同一邮箱的注册与邀请激活，配合唯一索引处理竞态。
func lockIdentity(ctx context.Context, tx pgx.Tx, email string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "identity:"+email)
	return err
}

// validateName 统一校验显示名称与租户名称，防止空白及异常长度。
func validateName(value string, limit int) error {
	if strings.TrimSpace(value) == "" || len([]rune(value)) > limit || strings.ContainsAny(value, "\x00\r\n") {
		return platform.Fail(400, "invalid_name", fmt.Sprintf("名称不能为空且最多%d个字符", limit))
	}
	return nil
}
