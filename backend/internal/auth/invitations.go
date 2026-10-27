// 文件作用说明：实现绑定租户邮箱的成员邀请、重发撤销及原子激活，受邀注册不另建租户。
package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// invitationView 描述邀请管理列表，不包含原始邀请凭证。
type invitationView struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	DisplayName    string    `json:"display_name"`
	Role           string    `json:"role"`
	CanServe       bool      `json:"can_serve"`
	Capacity       int       `json:"capacity"`
	Status         string    `json:"status"`
	ExpiresAt      time.Time `json:"expires_at"`
	DeliveryStatus string    `json:"delivery_status"`
	CreatedAt      time.Time `json:"created_at"`
}

// invitationInput 限定邀请可配置字段，平台角色与租户由服务端决定。
type invitationInput struct {
	Email       string `json:"email"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name"`
	CanServe    *bool  `json:"can_serve"`
	Capacity    *int   `json:"capacity"`
}

// invitations 返回当前租户邀请历史，状态与SMTP结果分别展示。
func (s *Service) invitations(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.pool.Query(r.Context(), `SELECT id::text,email,display_name,role,can_serve,capacity,CASE WHEN accepted_at IS NOT NULL THEN 'accepted' WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at<=now() THEN 'expired' ELSE 'pending' END,expires_at,delivery_status,created_at FROM member_invitations WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 1000`, platform.Current(r).TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []invitationView{}
	for rows.Next() {
		var invite invitationView
		if err := rows.Scan(&invite.ID, &invite.Email, &invite.DisplayName, &invite.Role, &invite.CanServe, &invite.Capacity, &invite.Status, &invite.ExpiresAt, &invite.DeliveryStatus, &invite.CreatedAt); err != nil {
			return err
		}
		items = append(items, invite)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// createInvitation 校验请求后创建本租户的管理员或客服邀请。
func (s *Service) createInvitation(w http.ResponseWriter, r *http.Request) error {
	var input invitationInput
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	return s.issueInvitation(w, r, input)
}

// issueInvitation 在事务提交后发送邀请，受理失败不会伪装成已发送。
func (s *Service) issueInvitation(w http.ResponseWriter, r *http.Request, input invitationInput) error {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}
	if input.Role != "admin" && input.Role != "agent" {
		return platform.Fail(400, "invalid_role", "只能邀请租户管理员或客服")
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName != "" {
		if err := validateName(input.DisplayName, 80); err != nil {
			return err
		}
	}
	capacity := 5
	if input.Capacity != nil {
		capacity = *input.Capacity
	}
	if capacity < 1 || capacity > 100 {
		return platform.Fail(400, "invalid_capacity", "接待上限需要为1至100")
	}
	canServe := input.Role == "agent"
	if input.CanServe != nil {
		canServe = *input.CanServe
	}
	if err := s.mailer.validate(); err != nil {
		return err
	}
	principal := platform.Current(r)
	if err := s.limit(r.Context(), "invite_ip", requestIP(r), time.Hour, 60); err != nil {
		return err
	}
	if err := s.limit(r.Context(), "invite_email", email, time.Hour, 10); err != nil {
		return err
	}
	token, err := platform.Token()
	if err != nil {
		return err
	}
	invite := invitationView{Email: email, DisplayName: input.DisplayName, Role: input.Role, CanServe: canServe, Capacity: capacity, Status: "pending", DeliveryStatus: "pending"}
	var deliveryID string
	err = pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := lockTenantAdmin(r.Context(), tx, principal); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND u.email=$2 AND m.removed_at IS NULL)`, principal.TenantID, email).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return platform.Fail(409, "already_member", "此邮箱已是租户成员，请直接管理现有成员")
		}
		var previous time.Time
		err := tx.QueryRow(r.Context(), "SELECT created_at FROM member_invitations WHERE tenant_id=$1 AND email=$2 ORDER BY created_at DESC LIMIT 1", principal.TenantID, email).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && time.Since(previous) < 60*time.Second {
			return platform.Fail(429, "invitation_cooldown", "请等待60秒后重新发送邀请")
		}
		if _, err := tx.Exec(r.Context(), "UPDATE member_invitations SET revoked_at=now() WHERE tenant_id=$1 AND email=$2 AND accepted_at IS NULL AND revoked_at IS NULL", principal.TenantID, email); err != nil {
			return err
		}
		if canServe {
			if err := s.seats(r.Context(), tx, principal.TenantID, "", "", 1); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(r.Context(), `INSERT INTO member_invitations(tenant_id,email,display_name,role,can_serve,capacity,token_hash,invited_by,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()+interval '7 days') RETURNING id::text,expires_at,created_at`, principal.TenantID, email, input.DisplayName, input.Role, canServe, capacity, digestToken(token), principal.UserID).Scan(&invite.ID, &invite.ExpiresAt, &invite.CreatedAt); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), "INSERT INTO email_delivery_records(kind,source_id,recipient,purpose) VALUES('invitation',$1,$2,'invitation') RETURNING id::text", invite.ID, email).Scan(&deliveryID); err != nil {
			return err
		}
		return audit(r.Context(), tx, principal.TenantID, principal.UserID, "member_invited", invite.ID)
	})
	if err != nil {
		return err
	}
	link := strings.TrimRight(s.cfg.PublicURL, "/") + "/accept-invitation?token=" + url.QueryEscape(token)
	result := s.mailer.send(r.Context(), email, "GoodChat 租户成员邀请", fmt.Sprintf("您受邀加入GoodChat租户「%s」。\n\n请打开以下链接，验证受邀邮箱后加入：\n%s\n\n邀请7天有效；注册受邀账号不会创建其他租户。邀请邮件不包含初始密码。如果并非您期待的邀请，请忽略。", principal.TenantName, link))
	if err := s.recordDelivery(r.Context(), "invitation", invite.ID, deliveryID, result); err != nil {
		return err
	}
	if result.Status == "failed" {
		return platform.Fail(502, "invitation_mail_failed", "邀请已保存，但邮件发送失败；请在邀请列表核对后重发")
	}
	invite.DeliveryStatus = result.Status
	status := 201
	if result.Status == "unknown" {
		status = 202
	}
	platform.JSON(w, status, map[string]any{"invitation": invite, "delivery_status": result.Status})
	return nil
}

// resendInvitation 复用既有邀请设置并轮换凭证，旧邀请立即失效。
func (s *Service) resendInvitation(w http.ResponseWriter, r *http.Request) error {
	if !validID(r.PathValue("id")) {
		return platform.Fail(400, "invalid_invitation", "邀请标识无效")
	}
	var input invitationInput
	var canServe bool
	var capacity int
	err := s.pool.QueryRow(r.Context(), "SELECT email,role,display_name,can_serve,capacity FROM member_invitations WHERE id=$1 AND tenant_id=$2 AND accepted_at IS NULL AND revoked_at IS NULL", r.PathValue("id"), platform.Current(r).TenantID).Scan(&input.Email, &input.Role, &input.DisplayName, &canServe, &capacity)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.Fail(404, "invitation_not_found", "此邀请不能重发")
	}
	if err != nil {
		return err
	}
	input.CanServe = &canServe
	input.Capacity = &capacity
	return s.issueInvitation(w, r, input)
}

// revokeInvitation 在当前租户管理锁内撤销未接受邀请，保持历史记录。
func (s *Service) revokeInvitation(w http.ResponseWriter, r *http.Request) error {
	if !validID(r.PathValue("id")) {
		return platform.Fail(400, "invalid_invitation", "邀请标识无效")
	}
	principal := platform.Current(r)
	err := pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := lockTenantAdmin(r.Context(), tx, principal); err != nil {
			return err
		}
		result, err := tx.Exec(r.Context(), "UPDATE member_invitations SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1 AND tenant_id=$2 AND accepted_at IS NULL", r.PathValue("id"), principal.TenantID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return platform.Fail(404, "invitation_not_found", "邀请不存在或已接受")
		}
		return audit(r.Context(), tx, principal.TenantID, principal.UserID, "invitation_revoked", r.PathValue("id"))
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]bool{"revoked": true})
	return nil
}

// acceptInvitation 验证受邀邮箱后原子加入已有租户，不信任前端提供角色或租户。
func (s *Service) acceptInvitation(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Token       string `json:"token"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		Code        string `json:"code"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	if len(input.Token) != 43 {
		return platform.Fail(400, "invalid_invitation", "邀请无效或已过期")
	}
	var inviteID, email, tenantID string
	var acceptedBy *string
	err := s.pool.QueryRow(r.Context(), "SELECT id::text,email,tenant_id::text,accepted_by::text FROM member_invitations WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now() AND delivery_status IN ('accepted','unknown')", digestToken(input.Token)).Scan(&inviteID, &email, &tenantID, &acceptedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.Fail(400, "invalid_invitation", "邀请无效、已使用或已过期")
	}
	if err != nil {
		return err
	}
	if err := s.limitAuthentication(r, email); err != nil {
		return err
	}
	principal, authErr := s.authenticate(r)
	verified := authErr == nil
	if verified && principal.Email != email {
		return platform.Fail(403, "invitation_email_mismatch", "请使用受邀邮箱账号接受邀请")
	}
	if acceptedBy != nil {
		if !verified || principal.UserID != *acceptedBy {
			return platform.Fail(400, "invalid_invitation", "邀请已使用，请直接登录")
		}
		view, err := s.account(r.Context(), principal.UserID, tenantID)
		if err != nil {
			return err
		}
		platform.JSON(w, 200, view)
		return nil
	}
	var exists bool
	if err := s.pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)", email).Scan(&exists); err != nil {
		return err
	}
	var hash string
	if !exists {
		hash, err = s.passwordHash(r.Context(), input.Password)
		if err != nil {
			return err
		}
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName != "" {
		if err := validateName(input.DisplayName, 80); err != nil {
			return err
		}
	}
	var userID, token string
	action := func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(r.Context(), "SELECT status FROM tenants WHERE id=$1 FOR UPDATE", tenantID).Scan(&status); err != nil {
			return err
		}
		if status != "active" {
			return platform.Fail(403, "tenant_inactive", "租户当前不可加入")
		}
		var role, displayName, inviter string
		var canServe bool
		var capacity int
		err := tx.QueryRow(r.Context(), `SELECT role,display_name,can_serve,capacity,invited_by::text FROM member_invitations WHERE id=$1 AND token_hash=$2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now() FOR UPDATE`, inviteID, digestToken(input.Token)).Scan(&role, &displayName, &canServe, &capacity, &inviter)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(400, "invalid_invitation", "邀请无效、已使用或已过期")
		}
		if err != nil {
			return err
		}
		var authorized, platformInviter, initialInvitation bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin') AND NOT disabled AND removed_at IS NULL),EXISTS(SELECT 1 FROM users WHERE id=$2 AND is_platform_admin),EXISTS(SELECT 1 FROM platform_tenant_creations WHERE tenant_id=$1 AND invitation_id=$3)`, tenantID, inviter, inviteID).Scan(&authorized, &platformInviter, &initialInvitation); err != nil {
			return err
		}
		if !authorized && !(platformInviter && initialInvitation) {
			return platform.Fail(403, "inviter_inactive", "邀请人权限已失效，请联系租户管理员")
		}
		if platformInviter && initialInvitation {
			if err := lockPlatformActor(r.Context(), tx, inviter); err != nil {
				return err
			}
		}
		if initialInvitation && platformInviter && role == "admin" {
			var hasOwner bool
			if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=$1 AND role='owner' AND removed_at IS NULL)", tenantID).Scan(&hasOwner); err != nil {
				return err
			}
			if !hasOwner {
				role = "owner"
			}
		}
		if err := lockIdentity(r.Context(), tx, email); err != nil {
			return err
		}
		if input.DisplayName != "" {
			displayName = input.DisplayName
		}
		if displayName == "" {
			displayName = strings.Split(email, "@")[0]
		}
		if err := validateName(displayName, 80); err != nil {
			return err
		}
		err = tx.QueryRow(r.Context(), "SELECT id::text FROM users WHERE email=$1 FOR UPDATE", email).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			if hash == "" {
				return platform.Fail(409, "account_changed", "账号状态已变化，请重新操作")
			}
			if err := tx.QueryRow(r.Context(), "INSERT INTO users(email,display_name,password_hash) VALUES($1,$2,$3) RETURNING id::text", email, displayName, hash).Scan(&userID); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		var memberID string
		if canServe {
			if err := s.seats(r.Context(), tx, tenantID, "", inviteID, 1); err != nil {
				return err
			}
		}
		err = tx.QueryRow(r.Context(), `INSERT INTO tenant_memberships(tenant_id,user_id,role,display_name,can_serve,capacity) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,user_id) DO UPDATE SET role=EXCLUDED.role,display_name=EXCLUDED.display_name,can_serve=EXCLUDED.can_serve,capacity=EXCLUDED.capacity,disabled=false,removed_at=NULL WHERE tenant_memberships.removed_at IS NOT NULL RETURNING id::text`, tenantID, userID, role, displayName, canServe, capacity).Scan(&memberID)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(409, "already_member", "已经加入租户，请直接登录")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE member_invitations SET accepted_by=$1,accepted_at=now() WHERE id=$2", userID, inviteID); err != nil {
			return err
		}
		if err := s.initialize(r.Context(), tx, tenantID, userID); err != nil {
			return err
		}
		if err := audit(r.Context(), tx, tenantID, userID, "invitation_accepted", memberID); err != nil {
			return err
		}
		token, err = s.createSession(r.Context(), tx, userID)
		return err
	}
	if verified {
		err = pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, action)
	} else {
		err = s.withCode(r.Context(), email, "invitation", input.Code, action)
	}
	if err != nil {
		return err
	}
	return s.finishLogin(w, r, userID, token, tenantID)
}
