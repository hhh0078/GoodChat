// 文件作用说明：管理本租户成员、权限和接待资格，保护所有者及最后管理员并记录审计。
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// memberView 返回租户成员及独立接待属性，不暴露密码和认证凭证。
type memberView struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	CanServe    bool      `json:"can_serve"`
	Disabled    bool      `json:"disabled"`
	Capacity    int       `json:"capacity"`
	CreatedAt   time.Time `json:"created_at"`
	AvatarRef   string    `json:"avatar_ref"`
	AvatarURL   string    `json:"avatar_url"`
}

// members 查询认证上下文指定租户的成员，包括停用成员但不包含已移出成员。
func (s *Service) members(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.pool.Query(r.Context(), `SELECT m.id::text,m.user_id::text,u.email,COALESCE(NULLIF(m.display_name,''),u.display_name),m.role,m.can_serve,m.disabled,m.capacity,m.created_at,m.avatar_ref,`+platform.AvatarURLSQL("m")+` FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.removed_at IS NULL ORDER BY m.created_at,m.id LIMIT 1000`, platform.Current(r).TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []memberView{}
	for rows.Next() {
		var member memberView
		if err := rows.Scan(&member.ID, &member.UserID, &member.Email, &member.DisplayName, &member.Role, &member.CanServe, &member.Disabled, &member.Capacity, &member.CreatedAt, &member.AvatarRef, &member.AvatarURL); err != nil {
			return err
		}
		items = append(items, member)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// lockTenantAdmin 在租户行锁内再次校验操作者，串行化成员管理和最后管理员检查。
func lockTenantAdmin(ctx context.Context, tx pgx.Tx, principal platform.Principal) (string, error) {
	var status string
	if err := tx.QueryRow(ctx, "SELECT status FROM tenants WHERE id=$1 FOR UPDATE", principal.TenantID).Scan(&status); err != nil {
		return "", err
	}
	if status != "active" {
		return "", platform.Fail(403, "tenant_inactive", "租户当前不可执行此操作")
	}
	var role string
	err := tx.QueryRow(ctx, "SELECT role FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2 AND NOT disabled AND removed_at IS NULL FOR UPDATE", principal.TenantID, principal.UserID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (role != "owner" && role != "admin") {
		return "", platform.Fail(403, "admin_required", "管理员权限已变更，请刷新后重试")
	}
	return role, err
}

// updateMember 修改本租户成员属性，不能通过角色字段创建平台管理员或移交所有者。
func (s *Service) updateMember(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Role        *string `json:"role"`
		CanServe    *bool   `json:"can_serve"`
		Disabled    *bool   `json:"disabled"`
		Capacity    *int    `json:"capacity"`
		DisplayName *string `json:"display_name"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	if !validID(r.PathValue("id")) {
		return platform.Fail(400, "invalid_member", "成员标识无效")
	}
	if input.Role != nil && *input.Role != "admin" && *input.Role != "agent" {
		return platform.Fail(400, "invalid_role", "成员角色只能为租户管理员或客服")
	}
	if input.Capacity != nil && (*input.Capacity < 1 || *input.Capacity > 100) {
		return platform.Fail(400, "invalid_capacity", "接待上限需要为1至100")
	}
	if input.DisplayName != nil {
		*input.DisplayName = strings.TrimSpace(*input.DisplayName)
		if err := validateName(*input.DisplayName, 80); err != nil {
			return err
		}
	}
	principal := platform.Current(r)
	err := pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := lockTenantAdmin(r.Context(), tx, principal); err != nil {
			return err
		}
		var userID, role string
		var disabled, canServe bool
		err := tx.QueryRow(r.Context(), "SELECT user_id::text,role,disabled,can_serve FROM tenant_memberships WHERE id=$1 AND tenant_id=$2 AND removed_at IS NULL FOR UPDATE", r.PathValue("id"), principal.TenantID).Scan(&userID, &role, &disabled, &canServe)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "member_not_found", "成员不存在")
		}
		if err != nil {
			return err
		}
		if role == "owner" && (input.Role != nil || input.Disabled != nil && *input.Disabled) {
			return platform.Fail(409, "owner_protected", "请先完成所有者移交，不能直接降级或停用所有者")
		}
		losingAdmin := (role == "admin" || role == "owner") && !disabled && (input.Role != nil && *input.Role == "agent" || input.Disabled != nil && *input.Disabled)
		if losingAdmin {
			if err := ensureOtherAdmin(r.Context(), tx, principal.TenantID, r.PathValue("id")); err != nil {
				return err
			}
		}
		newDisabled, newCanServe := disabled, canServe
		if input.Disabled != nil {
			newDisabled = *input.Disabled
		}
		if input.CanServe != nil {
			newCanServe = *input.CanServe
		}
		if newCanServe && !newDisabled && (!canServe || disabled) {
			if err := s.seats(r.Context(), tx, principal.TenantID, r.PathValue("id"), "", 1); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), `UPDATE tenant_memberships SET role=COALESCE($1,role),can_serve=COALESCE($2,can_serve),disabled=COALESCE($3,disabled),capacity=COALESCE($4,capacity),display_name=COALESCE($5,display_name) WHERE id=$6 AND tenant_id=$7`, input.Role, input.CanServe, input.Disabled, input.Capacity, input.DisplayName, r.PathValue("id"), principal.TenantID); err != nil {
			return err
		}
		if input.Disabled != nil && *input.Disabled || input.Role != nil && *input.Role != role {
			if _, err := tx.Exec(r.Context(), "UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL", userID); err != nil {
				return err
			}
		}
		if err := s.initialize(r.Context(), tx, principal.TenantID, userID); err != nil {
			return err
		}
		return audit(r.Context(), tx, principal.TenantID, principal.UserID, "member_updated", r.PathValue("id"))
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]bool{"updated": true})
	return nil
}

// ensureOtherAdmin 在已持有租户锁时验证至少另一名有效管理者存在。
func ensureOtherAdmin(ctx context.Context, tx pgx.Tx, tenantID, excludingID string) error {
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND id<>$2 AND role IN ('owner','admin') AND NOT disabled AND removed_at IS NULL", tenantID, excludingID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return platform.Fail(409, "last_admin", "至少需要保留一名有效租户管理员")
	}
	return nil
}

// removeMember 软移出成员并撤销其登录，历史作者及聊天数据仍然保留。
func (s *Service) removeMember(w http.ResponseWriter, r *http.Request) error {
	if !validID(r.PathValue("id")) {
		return platform.Fail(400, "invalid_member", "成员标识无效")
	}
	principal := platform.Current(r)
	err := pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := lockTenantAdmin(r.Context(), tx, principal); err != nil {
			return err
		}
		var userID, role string
		err := tx.QueryRow(r.Context(), "SELECT user_id::text,role FROM tenant_memberships WHERE id=$1 AND tenant_id=$2 AND removed_at IS NULL FOR UPDATE", r.PathValue("id"), principal.TenantID).Scan(&userID, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "member_not_found", "成员不存在")
		}
		if err != nil {
			return err
		}
		if role == "owner" {
			return platform.Fail(409, "owner_protected", "请先移交所有者，不能直接移出所有者")
		}
		if role == "admin" {
			if err := ensureOtherAdmin(r.Context(), tx, principal.TenantID, r.PathValue("id")); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), "UPDATE tenant_memberships SET disabled=true,can_serve=false,removed_at=now() WHERE id=$1 AND tenant_id=$2", r.PathValue("id"), principal.TenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE auth_sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL", userID); err != nil {
			return err
		}
		return audit(r.Context(), tx, principal.TenantID, principal.UserID, "member_removed", r.PathValue("id"))
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]bool{"removed": true})
	return nil
}

// transferOwnership 通过所有者本人独立验证码将所有权移交给已激活管理员。
func (s *Service) transferOwnership(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		MemberID string `json:"member_id"`
		Code     string `json:"code"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	if !validID(input.MemberID) {
		return platform.Fail(400, "invalid_member", "目标成员标识无效")
	}
	principal := platform.Current(r)
	if principal.Role != "owner" {
		return platform.Fail(403, "owner_required", "仅当前所有者可以移交")
	}
	if err := s.limitAuthentication(r, principal.Email); err != nil {
		return err
	}
	err := s.withCode(r.Context(), principal.Email, "ownership", input.Code, func(tx pgx.Tx) error {
		role, err := lockTenantAdmin(r.Context(), tx, principal)
		if err != nil {
			return err
		}
		if role != "owner" {
			return platform.Fail(403, "owner_required", "所有者已变更")
		}
		var targetRole string
		if err := tx.QueryRow(r.Context(), "SELECT role FROM tenant_memberships WHERE id=$1 AND tenant_id=$2 AND NOT disabled AND removed_at IS NULL FOR UPDATE", input.MemberID, principal.TenantID).Scan(&targetRole); errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "member_not_found", "目标成员不可用")
		} else if err != nil {
			return err
		}
		if targetRole != "admin" {
			return platform.Fail(400, "active_admin_required", "请先将目标成员设为有效管理员")
		}
		if _, err := tx.Exec(r.Context(), "UPDATE tenant_memberships SET role='admin' WHERE id=$1 AND tenant_id=$2", principal.MembershipID, principal.TenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE tenant_memberships SET role='owner' WHERE id=$1 AND tenant_id=$2", input.MemberID, principal.TenantID); err != nil {
			return err
		}
		return audit(r.Context(), tx, principal.TenantID, principal.UserID, "ownership_transferred", input.MemberID)
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]bool{"transferred": true})
	return nil
}
