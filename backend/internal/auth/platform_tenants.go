// 文件作用说明：实现平台代开租户、幂等防重和初始管理员邮件邀请，发信失败不会重复创建租户。
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata" // 将IANA时区随二进制提供，容器无需依赖宿主时区文件。

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// platformTenantInput 限定平台开通时可写字段，不接受所有者账号或平台角色。
type platformTenantInput struct {
	Name       string `json:"name"`
	AdminEmail string `json:"admin_email"`
	AdminName  string `json:"admin_name"`
	Timezone   string `json:"timezone"`
	CanServe   bool   `json:"can_serve"`
}

// platformTenantView 描述已真实创建的租户和初始邀请，不含邀请令牌或初始密码。
type platformTenantView struct {
	Tenant         map[string]any `json:"tenant"`
	Invitation     invitationView `json:"invitation"`
	DeliveryStatus string         `json:"delivery_status"`
	Replayed       bool           `json:"replayed"`
}

// normalizePlatformTenant 校验并规范化幂等请求，防止空名称及不可解析时区。
func normalizePlatformTenant(input *platformTenantInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if err := validateName(input.Name, 100); err != nil {
		return err
	}
	email, err := normalizeEmail(input.AdminEmail)
	if err != nil {
		return err
	}
	input.AdminEmail = email
	input.AdminName = strings.TrimSpace(input.AdminName)
	if input.AdminName == "" {
		input.AdminName = strings.Split(email, "@")[0]
	}
	if err := validateName(input.AdminName, 80); err != nil {
		return err
	}
	input.Timezone = strings.TrimSpace(input.Timezone)
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	if input.Timezone == "Local" {
		return platform.Fail(400, "invalid_timezone", "请使用明确的IANA时区，例如Asia/Shanghai")
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return platform.Fail(400, "invalid_timezone", "时区无效，请使用Asia/Shanghai等IANA时区")
	}
	return nil
}

// platformCreateTenant 在管理员幂等锁内原子创建租户、默认资源和初始管理员邀请。
func (s *Service) platformCreateTenant(w http.ResponseWriter, r *http.Request) error {
	var input platformTenantInput
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	if err := normalizePlatformTenant(&input); err != nil {
		return err
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		return platform.Fail(400, "idempotency_key_required", "创建租户必须提供1至128字符的Idempotency-Key")
	}
	p := platform.Current(r)
	raw, _ := json.Marshal(input)
	requestHash := digestToken(string(raw))
	var tenantID, invitationID, deliveryID, token string
	replayed := false
	err := pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if err := lockPlatformActor(r.Context(), tx, p.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "platform-tenant:"+p.UserID+":"+key); err != nil {
			return err
		}
		var previous []byte
		err := tx.QueryRow(r.Context(), "SELECT tenant_id::text,invitation_id::text,request_hash FROM platform_tenant_creations WHERE actor_id=$1 AND idempotency_key=$2", p.UserID, key).Scan(&tenantID, &invitationID, &previous)
		if err == nil {
			if !bytes.Equal(previous, requestHash) {
				return platform.Fail(409, "idempotency_conflict", "相同幂等标识已用于其他租户开通请求")
			}
			replayed = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := s.mailer.validate(); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), "INSERT INTO tenants(name,slug,timezone) VALUES($1,$2,$3) RETURNING id::text", input.Name, "platform-"+keyDigest(p.UserID, key), input.Timezone).Scan(&tenantID); err != nil {
			return err
		}
		if err := s.initialize(r.Context(), tx, tenantID, ""); err != nil {
			return err
		}
		if input.CanServe {
			if err := s.seats(r.Context(), tx, tenantID, "", "", 1); err != nil {
				return err
			}
		}
		token, err = platform.Token()
		if err != nil {
			return err
		}
		invitationID, deliveryID, err = insertPlatformInvitation(r.Context(), tx, tenantID, p.UserID, input, token)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO platform_tenant_creations(actor_id,idempotency_key,request_hash,tenant_id,invitation_id) VALUES($1,$2,$3,$4,$5)", p.UserID, key, requestHash, tenantID, invitationID); err != nil {
			return err
		}
		return audit(r.Context(), tx, tenantID, p.UserID, "platform_tenant_created", tenantID)
	})
	if err != nil {
		return err
	}
	if !replayed {
		if err := s.sendPlatformInvitation(r, tenantID, invitationID, deliveryID, input.AdminEmail, input.Name, token); err != nil {
			return err
		}
	}
	view, err := s.platformTenantResult(r.Context(), tenantID, invitationID, replayed)
	if err != nil {
		return err
	}
	status := 201
	if replayed {
		status = 200
	} else if view.DeliveryStatus == "unknown" {
		status = 202
	}
	platform.JSON(w, status, view)
	return nil
}

// keyDigest 为租户slug生成稳定且不暴露业务输入的短摘要。
func keyDigest(actor, key string) string {
	return fmt.Sprintf("%x", digestToken(actor + ":" + key)[:16])
}

// lockPlatformActor 在业务事务内再次验证平台身份，权限撤销不能使用旧请求完成开通。
func lockPlatformActor(ctx context.Context, tx pgx.Tx, userID string) error {
	var active bool
	if err := tx.QueryRow(ctx, "SELECT is_platform_admin FROM users WHERE id=$1 FOR SHARE", userID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return platform.Fail(403, "platform_required", "平台管理员权限已失效")
	}
	return nil
}

// insertPlatformInvitation 保存初始管理员邀请及独立SMTP投递记录。
func insertPlatformInvitation(ctx context.Context, tx pgx.Tx, tenantID, actorID string, input platformTenantInput, token string) (string, string, error) {
	var invitationID, deliveryID string
	if err := tx.QueryRow(ctx, `INSERT INTO member_invitations(tenant_id,email,display_name,role,can_serve,capacity,token_hash,invited_by,expires_at) VALUES($1,$2,$3,'admin',$4,5,$5,$6,now()+interval '7 days') RETURNING id::text`, tenantID, input.AdminEmail, input.AdminName, input.CanServe, digestToken(token), actorID).Scan(&invitationID); err != nil {
		return "", "", err
	}
	err := tx.QueryRow(ctx, "INSERT INTO email_delivery_records(kind,source_id,recipient,purpose) VALUES('invitation',$1,$2,'invitation') RETURNING id::text", invitationID, input.AdminEmail).Scan(&deliveryID)
	return invitationID, deliveryID, err
}

// sendPlatformInvitation 在事务提交后发信，只报告真实SMTP边界且不会回滚已创建租户。
func (s *Service) sendPlatformInvitation(r *http.Request, tenantID, invitationID, deliveryID, email, name, token string) error {
	link := strings.TrimRight(s.cfg.PublicURL, "/") + "/accept-invitation?token=" + url.QueryEscape(token)
	result := s.mailer.send(r.Context(), email, "GoodChat 租户管理员开通邀请", fmt.Sprintf("平台已为您开通GoodChat租户「%s」。\n\n请打开链接，验证受邀邮箱后设置密码并激活管理员身份：\n%s\n\n邀请7天有效。此邮件不包含初始密码；首次接受当前初始管理员邀请会成为本租户所有者。", name, link))
	if err := s.recordDelivery(r.Context(), "invitation", invitationID, deliveryID, result); err != nil {
		return err
	}
	if result.Status == "failed" {
		return platform.Fail(502, "invitation_mail_failed", "租户与初始管理员邀请已保存，但邮件发送失败；请在平台租户列表重发初始管理员邀请，不要重复新建租户")
	}
	return nil
}

// platformTenantResult 读取真实创建及投递状态，幂等重放不触发邮件二次发送。
func (s *Service) platformTenantResult(ctx context.Context, tenantID, invitationID string, replayed bool) (platformTenantView, error) {
	view := platformTenantView{Tenant: map[string]any{}, Replayed: replayed}
	var id, name, status, timezone string
	if err := s.pool.QueryRow(ctx, "SELECT id::text,name,status,timezone FROM tenants WHERE id=$1", tenantID).Scan(&id, &name, &status, &timezone); err != nil {
		return view, err
	}
	view.Tenant = map[string]any{"id": id, "name": name, "status": status, "timezone": timezone}
	err := s.pool.QueryRow(ctx, `SELECT id::text,email,display_name,role,can_serve,capacity,CASE WHEN accepted_at IS NOT NULL THEN 'accepted' WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at<=now() THEN 'expired' ELSE 'pending' END,expires_at,delivery_status,created_at FROM member_invitations WHERE id=$1 AND tenant_id=$2`, invitationID, tenantID).Scan(&view.Invitation.ID, &view.Invitation.Email, &view.Invitation.DisplayName, &view.Invitation.Role, &view.Invitation.CanServe, &view.Invitation.Capacity, &view.Invitation.Status, &view.Invitation.ExpiresAt, &view.Invitation.DeliveryStatus, &view.Invitation.CreatedAt)
	view.DeliveryStatus = view.Invitation.DeliveryStatus
	return view, err
}

// platformResendInitialInvitation 仅为尚无所有者的代开租户轮换初始邀请，旧凭证即时失效。
func (s *Service) platformResendInitialInvitation(w http.ResponseWriter, r *http.Request) error {
	tenantID := r.PathValue("id")
	if !validID(tenantID) {
		return platform.Fail(400, "invalid_tenant", "租户编号无效")
	}
	if err := s.mailer.validate(); err != nil {
		return err
	}
	p := platform.Current(r)
	token, err := platform.Token()
	if err != nil {
		return err
	}
	var invitationID, deliveryID string
	var input platformTenantInput
	err = pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(r.Context(), "SELECT status='active',name,timezone FROM tenants WHERE id=$1 FOR UPDATE", tenantID).Scan(&active, &input.Name, &input.Timezone); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return platform.Fail(404, "tenant_not_found", "没有找到此租户")
			}
			return err
		}
		if err := lockPlatformActor(r.Context(), tx, p.UserID); err != nil {
			return err
		}
		if !active {
			return platform.Fail(403, "tenant_inactive", "租户停用时不能重发激活邀请")
		}
		var hasOwner bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=$1 AND role='owner' AND removed_at IS NULL)", tenantID).Scan(&hasOwner); err != nil {
			return err
		}
		if hasOwner {
			return platform.Fail(409, "tenant_already_activated", "租户已有所有者，请由租户管理员管理邀请")
		}
		var oldID string
		var previous time.Time
		err := tx.QueryRow(r.Context(), `SELECT i.id::text,i.email,i.display_name,i.can_serve,i.created_at FROM platform_tenant_creations c JOIN member_invitations i ON i.id=c.invitation_id AND i.tenant_id=c.tenant_id WHERE c.tenant_id=$1 AND i.accepted_at IS NULL FOR UPDATE OF c,i`, tenantID).Scan(&oldID, &input.AdminEmail, &input.AdminName, &input.CanServe, &previous)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "initial_invitation_not_found", "没有可重发的初始管理员邀请")
		}
		if err != nil {
			return err
		}
		if time.Since(previous) < 60*time.Second {
			return platform.Fail(429, "invitation_cooldown", "请等待60秒后重发初始管理员邀请")
		}
		if _, err := tx.Exec(r.Context(), "SELECT set_config('app.tenant_id',$1,true)", tenantID); err != nil {
			return err
		}
		if input.CanServe {
			if err := s.seats(r.Context(), tx, tenantID, "", oldID, 1); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), "UPDATE member_invitations SET revoked_at=now() WHERE id=$1", oldID); err != nil {
			return err
		}
		invitationID, deliveryID, err = insertPlatformInvitation(r.Context(), tx, tenantID, p.UserID, input, token)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE platform_tenant_creations SET invitation_id=$2,updated_at=now() WHERE tenant_id=$1", tenantID, invitationID); err != nil {
			return err
		}
		return audit(r.Context(), tx, tenantID, p.UserID, "platform_initial_invitation_resent", invitationID)
	})
	if err != nil {
		return err
	}
	if err := s.sendPlatformInvitation(r, tenantID, invitationID, deliveryID, input.AdminEmail, input.Name, token); err != nil {
		return err
	}
	view, err := s.platformTenantResult(r.Context(), tenantID, invitationID, false)
	if err != nil {
		return err
	}
	status := 200
	if view.DeliveryStatus == "unknown" {
		status = 202
	}
	platform.JSON(w, status, view)
	return nil
}
