// 文件作用说明：提供平台租户清单、状态调整与人工套餐分配，不读取聊天正文或删除超额历史资源。
package billing

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TenantView 是平台运营清单，仅包含租户基础信息与有效套餐、席位统计。
type TenantView struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Slug         string        `json:"slug"`
	Status       string        `json:"status"`
	Timezone     string        `json:"timezone"`
	CreatedAt    time.Time     `json:"created_at"`
	Plan         Plan          `json:"plan"`
	Subscription *Subscription `json:"subscription"`
	Usage        Usage         `json:"usage"`
}

// tenants 为平台返回有限数量的租户与套餐，筛选仅使用参数绑定。
func (s *Service) tenants(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	if utf8.RuneCountInString(q) > 100 {
		return platform.Fail(400, "invalid_query", "查询词最多100字")
	}
	if status != "" && status != "active" && status != "suspended" && status != "expired" {
		return platform.Fail(400, "invalid_status", "租户状态无效")
	}
	items := []TenantView{}
	err := s.platformTx(r, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), "SELECT id::text,name,slug,status,timezone,created_at FROM tenants WHERE ($1='' OR name ILIKE '%'||$1||'%' OR slug ILIKE '%'||$1||'%') AND ($2='' OR status=$2) ORDER BY created_at DESC,id LIMIT 200", q, status)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item TenantView
			if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Status, &item.Timezone, &item.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			items = append(items, item)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for index := range items {
			item := &items[index]
			plan, sub, err := effectivePlan(r.Context(), tx, item.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				plan, err = scanPlan(tx.QueryRow(r.Context(), "SELECT "+planColumns+" FROM plans WHERE code='free'"))
			} else if err == nil {
				item.Subscription = &sub
			}
			if err != nil {
				return err
			}
			item.Plan = plan
			item.Usage, err = seatUsage(r.Context(), tx, item.ID, "", "", plan.SeatLimit)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// updateTenant 原子记录状态或人工套餐分配；收费套餐必须明确确认实际收款依据。
func (s *Service) updateTenant(w http.ResponseWriter, r *http.Request) error {
	if !validID(r.PathValue("id")) {
		return platform.Fail(400, "invalid_tenant", "租户标识无效")
	}
	var input struct {
		Name             *string `json:"name"`
		Status           *string `json:"status"`
		PlanID           *string `json:"plan_id"`
		PaymentConfirmed bool    `json:"payment_confirmed"`
		PaymentReference string  `json:"payment_reference"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	if input.Name == nil && input.Status == nil && input.PlanID == nil {
		return platform.Fail(400, "empty_update", "请指定租户名称、状态或套餐")
	}
	if input.Name != nil {
		*input.Name = strings.TrimSpace(*input.Name)
		if utf8.RuneCountInString(*input.Name) < 1 || utf8.RuneCountInString(*input.Name) > 100 {
			return platform.Fail(400, "invalid_name", "租户名称应为1至100字")
		}
	}
	if input.Status != nil && *input.Status != "active" && *input.Status != "suspended" && *input.Status != "expired" {
		return platform.Fail(400, "invalid_status", "租户状态无效")
	}
	input.PaymentReference = strings.TrimSpace(input.PaymentReference)
	if input.PlanID != nil && (!validID(*input.PlanID) || input.PaymentReference == "" || utf8.RuneCountInString(input.PaymentReference) > 500) {
		return platform.Fail(400, "invalid_plan_assignment", "分配套餐需要有效版本及实际收款或审批参考")
	}
	err := s.platformTx(r, func(tx pgx.Tx) error {
		if err := tenantLock(r.Context(), tx, r.PathValue("id")); err != nil {
			return err
		}
		if input.PlanID != nil {
			plan, err := scanPlan(tx.QueryRow(r.Context(), "SELECT "+planColumns+" FROM plans WHERE id=$1 AND is_active FOR SHARE", *input.PlanID))
			if errors.Is(err, pgx.ErrNoRows) {
				return platform.Fail(404, "plan_not_available", "套餐不存在或已停用")
			}
			if err != nil {
				return err
			}
			if plan.PriceAmount > 0 && !input.PaymentConfirmed {
				return platform.Fail(400, "payment_confirmation_required", "付费版本须先核对实际收款并明确确认")
			}
			if err := applyPlan(r.Context(), tx, r.PathValue("id"), plan, platform.Current(r).UserID); err != nil {
				return err
			}
			if err := billingAudit(r.Context(), tx, r.PathValue("id"), platform.Current(r).UserID, "tenant_plan_assigned", r.PathValue("id"), map[string]any{"plan_snapshot": plan, "payment_reference": input.PaymentReference, "payment_confirmed": input.PaymentConfirmed}); err != nil {
				return err
			}
		}
		if input.Name != nil || input.Status != nil {
			if _, err := tx.Exec(r.Context(), "UPDATE tenants SET name=COALESCE($1,name),status=COALESCE($2,status) WHERE id=$3", input.Name, input.Status, r.PathValue("id")); err != nil {
				return err
			}
			if err := billingAudit(r.Context(), tx, r.PathValue("id"), platform.Current(r).UserID, "tenant_updated", r.PathValue("id"), map[string]any{"name": input.Name, "status": input.Status}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]bool{"updated": true})
	return nil
}
