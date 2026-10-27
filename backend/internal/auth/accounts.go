// 文件作用说明：实现邮箱注册自动建租户、验证码及密码登录、找回密码和设备会话撤销。
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"supportdesk/backend/internal/platform"
)

// passwordHash 限制同时运行的昂贵密码派生数量，避免认证接口耗尽进程内存。
func (s *Service) passwordHash(ctx context.Context, password string) (string, error) {
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return hashPassword(password)
}

// passwordMatches 对未知账号也执行等价密码摘要工作，减少账号存在性时序差异。
func (s *Service) passwordMatches(ctx context.Context, encoded, password string) (bool, error) {
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if encoded == "" {
		encoded = s.dummyHash
	}
	return verifyPassword(encoded, password), nil
}

// registerAccount 在一次事务中消费注册验证码并创建账号、租户、所有者及默认资源。
func (s *Service) registerAccount(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		Password    string `json:"password"`
		TenantName  string `json:"tenant_name"`
		DisplayName string `json:"display_name"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}
	if err := s.limitAuthentication(r, email); err != nil {
		return err
	}
	input.TenantName = strings.TrimSpace(input.TenantName)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" {
		input.DisplayName = strings.Split(email, "@")[0]
	}
	if err := validateName(input.TenantName, 100); err != nil {
		return err
	}
	if err := validateName(input.DisplayName, 80); err != nil {
		return err
	}
	hash, err := s.passwordHash(r.Context(), input.Password)
	if err != nil {
		return err
	}
	var userID, tenantID, token string
	err = s.withCode(r.Context(), email, "register", input.Code, func(tx pgx.Tx) error {
		if err := lockIdentity(r.Context(), tx, email); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)", email).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return platform.Fail(409, "email_registered", "该邮箱已注册，请登录或找回密码")
		}
		if err := tx.QueryRow(r.Context(), "INSERT INTO users(email,display_name,password_hash) VALUES($1,$2,$3) RETURNING id::text", email, input.DisplayName, hash).Scan(&userID); err != nil {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), "INSERT INTO tenants(name,slug) VALUES($1,$2) RETURNING id::text", input.TenantName, "tenant-"+id).Scan(&tenantID); err != nil {
			return err
		}
		var memberID string
		if err := tx.QueryRow(r.Context(), "INSERT INTO tenant_memberships(tenant_id,user_id,role,can_serve) VALUES($1,$2,'owner',true) RETURNING id::text", tenantID, userID).Scan(&memberID); err != nil {
			return err
		}
		if err := s.initialize(r.Context(), tx, tenantID, userID); err != nil {
			return err
		}
		if err := s.seats(r.Context(), tx, tenantID, memberID, "", 1); err != nil {
			return err
		}
		if err := audit(r.Context(), tx, tenantID, userID, "tenant_registered", memberID); err != nil {
			return err
		}
		token, err = s.createSession(r.Context(), tx, userID)
		return err
	})
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "23505" {
			return platform.Fail(409, "email_registered", "该邮箱已注册，请登录")
		}
		return err
	}
	return s.finishLogin(w, r, userID, token, tenantID)
}

// loginCode 仅为已有账号登录，不会通过登录接口隐式创建新租户。
func (s *Service) loginCode(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}
	if err := s.limitAuthentication(r, email); err != nil {
		return err
	}
	var userID, token string
	err = s.withCode(r.Context(), email, "login", input.Code, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), "SELECT id::text FROM users WHERE email=$1 FOR SHARE", email).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(401, "invalid_credentials", "登录信息无效，请检查或先注册")
		} else if err != nil {
			return err
		}
		var err error
		token, err = s.createSession(r.Context(), tx, userID)
		return err
	})
	if err != nil {
		return err
	}
	return s.finishLogin(w, r, userID, token, "")
}

// loginPassword 校验密码并在账号共享锁内建立可撤销会话，避免与重置竞态。
func (s *Service) loginPassword(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}
	if err := s.limitAuthentication(r, email); err != nil {
		return err
	}
	var userID, hash string
	err = s.pool.QueryRow(r.Context(), "SELECT id::text,password_hash FROM users WHERE email=$1", email).Scan(&userID, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	matched, err := s.passwordMatches(r.Context(), hash, input.Password)
	if err != nil {
		return err
	}
	if !matched || userID == "" {
		return platform.Fail(401, "invalid_credentials", "邮箱或密码不正确")
	}
	var token string
	err = pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var currentHash string
		if err := tx.QueryRow(r.Context(), "SELECT password_hash FROM users WHERE id=$1 FOR SHARE", userID).Scan(&currentHash); err != nil {
			return err
		}
		if currentHash != hash {
			return platform.Fail(401, "invalid_credentials", "登录信息已变更，请重新登录")
		}
		var err error
		token, err = s.createSession(r.Context(), tx, userID)
		return err
	})
	if err != nil {
		return err
	}
	return s.finishLogin(w, r, userID, token, "")
}

// resetPassword 验证独立用途验证码后修改密码并撤销所有已有设备会话。
func (s *Service) resetPassword(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}
	if err := s.limitAuthentication(r, email); err != nil {
		return err
	}
	hash, err := s.passwordHash(r.Context(), input.Password)
	if err != nil {
		return err
	}
	err = s.withCode(r.Context(), email, "reset_password", input.Code, func(tx pgx.Tx) error {
		var userID string
		if err := tx.QueryRow(r.Context(), "UPDATE users SET password_hash=$1 WHERE email=$2 RETURNING id::text", hash, email).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(400, "invalid_credentials", "无法重置，请检查邮箱或先注册")
		} else if err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), "UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL", userID)
		return err
	})
	if err != nil {
		return err
	}
	s.clearSessionCookie(w)
	platform.JSON(w, 200, map[string]bool{"reset": true})
	return nil
}

// me 返回当前账号、有效租户列表与当前选中的租户。
func (s *Service) me(w http.ResponseWriter, r *http.Request) error {
	principal := platform.Current(r)
	view, err := s.account(r.Context(), principal.UserID, principal.TenantID)
	if err != nil {
		return err
	}
	platform.JSON(w, 200, view)
	return nil
}

// logout 撤销当前Cookie对应会话，重复凭证不能继续使用。
func (s *Service) logout(w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return platform.Fail(401, "unauthenticated", "请先登录")
	}
	if _, err := s.pool.Exec(r.Context(), "UPDATE auth_sessions SET revoked_at=now() WHERE token_hash=$1", digestToken(cookie.Value)); err != nil {
		return err
	}
	s.clearSessionCookie(w)
	platform.JSON(w, 200, map[string]bool{"logged_out": true})
	return nil
}

// logoutOthers 保留当前设备并撤销本人其他登录凭证。
func (s *Service) logoutOthers(w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return platform.Fail(401, "unauthenticated", "请先登录")
	}
	if _, err := s.pool.Exec(r.Context(), "UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1 AND token_hash<>$2 AND revoked_at IS NULL", platform.Current(r).UserID, digestToken(cookie.Value)); err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]bool{"revoked": true})
	return nil
}
