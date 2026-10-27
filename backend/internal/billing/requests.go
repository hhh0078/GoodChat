// 文件作用说明：保存套餐申请价格权益快照，提供人工核对收款、幂等开通与取消流程，不执行在线支付。
package billing

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"supportdesk/backend/internal/platform"
)

// UpgradeRequest 表示人工开通申请，展示价格和权益始终读取申请时快照。
type UpgradeRequest struct {
	ID               string     `json:"id"`
	TenantID         string     `json:"tenant_id"`
	TenantName       string     `json:"tenant_name"`
	PlanID           string     `json:"plan_id"`
	PlanSnapshot     Plan       `json:"plan_snapshot"`
	Status           string     `json:"status"`
	RequestedBy      string     `json:"requested_by"`
	ActivatedBy      *string    `json:"activated_by"`
	PaymentReference string     `json:"payment_reference"`
	CreatedAt        time.Time  `json:"created_at"`
	ActivatedAt      *time.Time `json:"activated_at"`
}

const requestColumns = "r.id::text,r.tenant_id::text,t.name,r.plan_id::text,r.plan_snapshot,r.status,r.requested_by::text,r.activated_by::text,r.payment_reference,r.created_at,r.activated_at"

// scanRequest 从明确列顺序读取申请及其历史快照。
func scanRequest(row pgx.Row) (UpgradeRequest, error) {
	var item UpgradeRequest
	err := row.Scan(&item.ID, &item.TenantID, &item.TenantName, &item.PlanID, &item.PlanSnapshot, &item.Status, &item.RequestedBy, &item.ActivatedBy, &item.PaymentReference, &item.CreatedAt, &item.ActivatedAt)
	return item, err
}

// subscription 返回当前有效权益、历史订阅和真实席位占用，到期不会删除原订阅。
func (s *Service) subscription(w http.ResponseWriter, r *http.Request) error {
	principal := platform.Current(r)
	var plan Plan
	var sub Subscription
	var usage Usage
	var plans []Plan
	err := platform.TenantTx(r.Context(), s.pool, principal.TenantID, func(tx pgx.Tx) error {
		if err := s.EnsureTenant(r.Context(), tx, principal.TenantID); err != nil {
			return err
		}
		var err error
		plan, sub, err = effectivePlan(r.Context(), tx, principal.TenantID)
		if err != nil {
			return err
		}
		usage, err = seatUsage(r.Context(), tx, principal.TenantID, "", "", plan.SeatLimit)
		if err != nil {
			return err
		}
		plans, err = loadPlans(r.Context(), tx, true)
		return err
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"plan": plan, "subscription": sub, "usage": usage, "available_plans": plans})
	return nil
}

// tenantAdminLock 在同一租户行锁下复核管理员身份，防止并发停用后继续申请。
func tenantAdminLock(ctx context.Context, tx pgx.Tx, p platform.Principal) error {
	if err := tenantLock(ctx, tx, p.TenantID); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRow(ctx, "SELECT status FROM tenants WHERE id=$1", p.TenantID).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return platform.Fail(403, "tenant_inactive", "租户当前不可执行此操作")
	}
	var valid bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE id=$1 AND user_id=$2 AND tenant_id=$3 AND role IN ('owner','admin') AND NOT disabled AND removed_at IS NULL)", p.MembershipID, p.UserID, p.TenantID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return platform.Fail(403, "admin_required", "租户管理员权限已变更")
	}
	return nil
}

// createRequest 锁定已发布套餐并保存不可变申请依据，同套餐仅允许一份待处理申请。
func (s *Service) createRequest(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		PlanID string `json:"plan_id"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	if !validID(input.PlanID) {
		return platform.Fail(400, "invalid_plan", "请选择有效套餐")
	}
	p := platform.Current(r)
	var item UpgradeRequest
	err := platform.TenantTx(r.Context(), s.pool, p.TenantID, func(tx pgx.Tx) error {
		if err := tenantAdminLock(r.Context(), tx, p); err != nil {
			return err
		}
		plan, err := scanPlan(tx.QueryRow(r.Context(), "SELECT "+planColumns+" FROM plans WHERE id=$1 AND is_public AND is_active FOR SHARE", input.PlanID))
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "plan_not_available", "该套餐未发布或已停止申请")
		}
		if err != nil {
			return err
		}
		var id string
		if err = tx.QueryRow(r.Context(), "INSERT INTO upgrade_requests(tenant_id,plan_id,plan_snapshot,requested_by) VALUES($1,$2,$3,$4) RETURNING id::text", p.TenantID, plan.ID, plan, p.UserID).Scan(&id); err != nil {
			var db *pgconn.PgError
			if errors.As(err, &db) && db.Code == "23505" {
				return platform.Fail(409, "request_pending", "该套餐已有待处理申请，请勿重复提交")
			}
			return err
		}
		item, err = scanRequest(tx.QueryRow(r.Context(), "SELECT "+requestColumns+" FROM upgrade_requests r JOIN tenants t ON t.id=r.tenant_id WHERE r.id=$1", id))
		if err != nil {
			return err
		}
		return billingAudit(r.Context(), tx, p.TenantID, p.UserID, "upgrade_requested", id, plan)
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 201, item)
	return nil
}

// loadRequests 限制一次读取的申请数量，可按租户或待处理状态筛选且不返回聊天资料。
func loadRequests(ctx context.Context, tx pgx.Tx, tenantID, status string) ([]UpgradeRequest, error) {
	rows, err := tx.Query(ctx, "SELECT "+requestColumns+" FROM upgrade_requests r JOIN tenants t ON t.id=r.tenant_id WHERE ($1='' OR r.tenant_id=NULLIF($1,'')::uuid) AND ($2='' OR r.status=$2) ORDER BY r.created_at DESC,r.id DESC LIMIT 500", tenantID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []UpgradeRequest{}
	for rows.Next() {
		item, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// requestStatus 校验申请列表支持的状态，拒绝无法解释的筛选值。
func requestStatus(r *http.Request) (string, error) {
	status := r.URL.Query().Get("status")
	if status != "" && status != "pending" && status != "activated" && status != "cancelled" {
		return "", platform.Fail(400, "invalid_status", "申请状态无效")
	}
	return status, nil
}

// tenantRequests 向当前租户管理员返回本租户的申请历史。
func (s *Service) tenantRequests(w http.ResponseWriter, r *http.Request) error {
	status, err := requestStatus(r)
	if err != nil {
		return err
	}
	var items []UpgradeRequest
	err = platform.TenantTx(r.Context(), s.pool, platform.Current(r).TenantID, func(tx pgx.Tx) error {
		var err error
		items, err = loadRequests(r.Context(), tx, platform.Current(r).TenantID, status)
		return err
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// platformRequests 向平台审核人员提供真实申请快照，避免修改套餐后误认原价格。
func (s *Service) platformRequests(w http.ResponseWriter, r *http.Request) error {
	status, err := requestStatus(r)
	if err != nil {
		return err
	}
	var items []UpgradeRequest
	err = s.platformTx(r, func(tx pgx.Tx) error {
		var err error
		items, err = loadRequests(r.Context(), tx, "", status)
		return err
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// cancelRequest 在租户行锁内取消本人租户的待处理申请，已开通申请不能撤销订阅。
func (s *Service) cancelRequest(w http.ResponseWriter, r *http.Request) error {
	if !validID(r.PathValue("id")) {
		return platform.Fail(400, "invalid_request", "申请标识无效")
	}
	p := platform.Current(r)
	err := platform.TenantTx(r.Context(), s.pool, p.TenantID, func(tx pgx.Tx) error {
		if err := tenantAdminLock(r.Context(), tx, p); err != nil {
			return err
		}
		var status string
		err := tx.QueryRow(r.Context(), "SELECT status FROM upgrade_requests WHERE id=$1 AND tenant_id=$2 FOR UPDATE", r.PathValue("id"), p.TenantID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "request_not_found", "申请不存在")
		}
		if err != nil {
			return err
		}
		if status == "cancelled" {
			return nil
		}
		if status != "pending" {
			return platform.Fail(409, "request_processed", "申请已开通，无法取消")
		}
		if _, err = tx.Exec(r.Context(), "UPDATE upgrade_requests SET status='cancelled' WHERE id=$1", r.PathValue("id")); err != nil {
			return err
		}
		return billingAudit(r.Context(), tx, p.TenantID, p.UserID, "upgrade_cancelled", r.PathValue("id"), map[string]string{"status": "cancelled"})
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]bool{"cancelled": true})
	return nil
}

// activateRequest 仅由平台管理员确认实际收款或审批依据后开通申请快照，重复调用不延长或重置订阅。
func (s *Service) activateRequest(w http.ResponseWriter, r *http.Request) error {
	if !validID(r.PathValue("id")) {
		return platform.Fail(400, "invalid_request", "申请标识无效")
	}
	var input struct {
		PaymentConfirmed bool   `json:"payment_confirmed"`
		PaymentReference string `json:"payment_reference"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	input.PaymentReference = strings.TrimSpace(input.PaymentReference)
	if input.PaymentReference == "" || utf8.RuneCountInString(input.PaymentReference) > 500 {
		return platform.Fail(400, "payment_reference_required", "请填写实际收款或审批参考，最多500字")
	}
	var item UpgradeRequest
	err := s.platformTx(r, func(tx pgx.Tx) error {
		var tenantID string
		err := tx.QueryRow(r.Context(), "SELECT tenant_id::text FROM upgrade_requests WHERE id=$1", r.PathValue("id")).Scan(&tenantID)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "request_not_found", "申请不存在")
		}
		if err != nil {
			return err
		}
		if err := tenantLock(r.Context(), tx, tenantID); err != nil {
			return err
		}
		item, err = scanRequest(tx.QueryRow(r.Context(), "SELECT "+requestColumns+" FROM upgrade_requests r JOIN tenants t ON t.id=r.tenant_id WHERE r.id=$1 FOR UPDATE OF r", r.PathValue("id")))
		if err != nil {
			return err
		}
		if item.Status == "activated" {
			return nil
		}
		if item.Status != "pending" {
			return platform.Fail(409, "request_cancelled", "申请已经取消，不能开通")
		}
		if item.PlanSnapshot.PriceAmount > 0 && !input.PaymentConfirmed {
			return platform.Fail(400, "payment_confirmation_required", "请先核对真实收款并明确确认，提交申请不会自动扣款")
		}
		if err := applyPlan(r.Context(), tx, tenantID, item.PlanSnapshot, platform.Current(r).UserID); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE upgrade_requests SET status='activated',activated_by=$1,payment_reference=$2,activated_at=now() WHERE id=$3", platform.Current(r).UserID, input.PaymentReference, item.ID); err != nil {
			return err
		}
		if err = billingAudit(r.Context(), tx, tenantID, platform.Current(r).UserID, "upgrade_activated", item.ID, map[string]any{"plan_snapshot": item.PlanSnapshot, "payment_reference": input.PaymentReference, "payment_confirmed": input.PaymentConfirmed}); err != nil {
			return err
		}
		item, err = scanRequest(tx.QueryRow(r.Context(), "SELECT "+requestColumns+" FROM upgrade_requests r JOIN tenants t ON t.id=r.tenant_id WHERE r.id=$1", item.ID))
		return err
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, item)
	return nil
}

// applyPlan 原子替换有效订阅，允许降级但从不删除超额成员或已有会话。
func applyPlan(ctx context.Context, tx pgx.Tx, tenantID string, plan Plan, actorID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO tenant_subscriptions(tenant_id,plan_id,plan_snapshot,started_at,expires_at,activated_by) VALUES($1,$2,$3,statement_timestamp(),CASE WHEN $4::integer=0 THEN NULL ELSE statement_timestamp()+make_interval(days=>$4::integer) END,$5)
	ON CONFLICT(tenant_id) DO UPDATE SET plan_id=EXCLUDED.plan_id,plan_snapshot=EXCLUDED.plan_snapshot,started_at=EXCLUDED.started_at,expires_at=EXCLUDED.expires_at,activated_by=EXCLUDED.activated_by,updated_at=now()`, tenantID, plan.ID, plan, plan.ValidityDays, actorID)
	return err
}
