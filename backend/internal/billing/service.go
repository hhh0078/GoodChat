// 文件作用说明：统一读取套餐权益并串行化席位预留，过期订阅实时回退免费版，不中断已有会话。
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// Service 管理套餐与真实人工开通流程，不执行任何在线支付。
type Service struct {
	pool *pgxpool.Pool
	auth platform.Authorizer
}

// Plan 是官网、后台和申请快照共用的套餐表示，价格单位为整数分。
type Plan struct {
	ID           string          `json:"id"`
	Code         string          `json:"code"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	PriceAmount  int64           `json:"price_amount"`
	Currency     string          `json:"currency"`
	ValidityDays int             `json:"validity_days"`
	SeatLimit    int             `json:"seat_limit"`
	Features     map[string]bool `json:"features"`
	IsPublic     bool            `json:"is_public"`
	IsActive     bool            `json:"is_active"`
	SortOrder    int             `json:"sort_order"`
}

// Subscription 返回最近一次开通及其实际有效状态；effective_plan在外层plan中体现。
type Subscription struct {
	TenantID     string     `json:"tenant_id"`
	PlanID       string     `json:"plan_id"`
	PlanSnapshot Plan       `json:"plan_snapshot"`
	StartedAt    time.Time  `json:"started_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	Status       string     `json:"status"`
}

// Usage 分开统计已启用接待成员和有效待激活邀请，管理角色与席位独立。
type Usage struct {
	SeatsUsed     int `json:"seats_used"`
	SeatsReserved int `json:"seats_reserved"`
	SeatLimit     int `json:"seat_limit"`
}

const planColumns = "id::text,code,name,description,price_amount,currency,validity_days,seat_limit,features,is_public,is_active,sort_order"

var featureCodes = map[string]bool{"chat": true, "multi_link": true, "labels": true, "statistics": true, "decoration": true, "advanced_decoration": true, "ai_design": true, "callbacks": true, "export": true}

// New 使用统一授权器构造套餐模块；配置保留为各业务模块一致的装配契约。
func New(pool *pgxpool.Pool, auth platform.Authorizer, cfg platform.Config) *Service {
	return &Service{pool: pool, auth: auth}
}

// Register 注册公开报价、租户订阅与平台开通接口，权限由服务端身份决定。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/public/plans", endpoint(s.publicPlans))
	mux.HandleFunc("GET /api/v1/tenant/subscription", s.auth.RequireAdmin(endpoint(s.subscription)))
	mux.HandleFunc("POST /api/v1/tenant/upgrade-requests", s.auth.RequireAdmin(endpoint(s.createRequest)))
	mux.HandleFunc("GET /api/v1/tenant/upgrade-requests", s.auth.RequireAdmin(endpoint(s.tenantRequests)))
	mux.HandleFunc("DELETE /api/v1/tenant/upgrade-requests/{id}", s.auth.RequireAdmin(endpoint(s.cancelRequest)))
	mux.HandleFunc("GET /api/v1/platform/plans", s.auth.RequirePlatform(endpoint(s.platformPlans)))
	mux.HandleFunc("POST /api/v1/platform/plans", s.auth.RequirePlatform(endpoint(s.createPlan)))
	mux.HandleFunc("PATCH /api/v1/platform/plans/{id}", s.auth.RequirePlatform(endpoint(s.updatePlan)))
	mux.HandleFunc("GET /api/v1/platform/upgrade-requests", s.auth.RequirePlatform(endpoint(s.platformRequests)))
	mux.HandleFunc("POST /api/v1/platform/upgrade-requests/{id}/activate", s.auth.RequirePlatform(endpoint(s.activateRequest)))
	mux.HandleFunc("GET /api/v1/platform/tenants", s.auth.RequirePlatform(endpoint(s.tenants)))
	mux.HandleFunc("PATCH /api/v1/platform/tenants/{id}", s.auth.RequirePlatform(endpoint(s.updateTenant)))
}

// endpoint 将模块错误交给统一安全响应，避免泄漏数据库连接信息。
func endpoint(action func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := action(w, r); err != nil {
			platform.ErrorJSON(w, r, err)
		}
	}
}

// scanPlan 从明确列顺序读取套餐，所有外部响应不包含内部管理员信息。
func scanPlan(row pgx.Row) (Plan, error) {
	var p Plan
	err := row.Scan(&p.ID, &p.Code, &p.Name, &p.Description, &p.PriceAmount, &p.Currency, &p.ValidityDays, &p.SeatLimit, &p.Features, &p.IsPublic, &p.IsActive, &p.SortOrder)
	return p, err
}

// loadPlans 按展示顺序读取平台配置或公开发布的可用套餐。
func loadPlans(ctx context.Context, tx interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, public bool) ([]Plan, error) {
	query := "SELECT " + planColumns + " FROM plans"
	if public {
		query += " WHERE is_public AND is_active"
	}
	rows, err := tx.Query(ctx, query+" ORDER BY sort_order,price_amount,code LIMIT 1000")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// tenantLock 串行化同一租户的订阅、成员启用与邀请预留，避免并发突破额度。
func tenantLock(ctx context.Context, tx pgx.Tx, tenantID string) error {
	var id string
	err := tx.QueryRow(ctx, "SELECT id::text FROM tenants WHERE id=$1 FOR UPDATE", tenantID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.Fail(404, "tenant_not_found", "租户不存在")
	}
	return err
}

// EnsureTenant 在注册事务内自动开通长期免费版；已有订阅不会被重置。
func (s *Service) EnsureTenant(ctx context.Context, tx pgx.Tx, tenantID string) error {
	if err := tenantLock(ctx, tx, tenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID); err != nil {
		return err
	}
	p, err := scanPlan(tx.QueryRow(ctx, "SELECT "+planColumns+" FROM plans WHERE code='free'"))
	if err != nil {
		return err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO tenant_subscriptions(tenant_id,plan_id,plan_snapshot) VALUES($1,$2,$3) ON CONFLICT(tenant_id) DO NOTHING", tenantID, p.ID, body)
	return err
}

// effectivePlan 读取真实开通快照，免费租户与到期租户使用当前免费配置而保留历史订阅。
func effectivePlan(ctx context.Context, tx pgx.Tx, tenantID string) (Plan, Subscription, error) {
	var sub Subscription
	var raw []byte
	err := tx.QueryRow(ctx, "SELECT tenant_id::text,plan_id::text,plan_snapshot,started_at,expires_at FROM tenant_subscriptions WHERE tenant_id=$1", tenantID).Scan(&sub.TenantID, &sub.PlanID, &raw, &sub.StartedAt, &sub.ExpiresAt)
	if err != nil {
		return Plan{}, sub, err
	}
	if err = json.Unmarshal(raw, &sub.PlanSnapshot); err != nil {
		return Plan{}, sub, err
	}
	p := sub.PlanSnapshot
	sub.Status = "active"
	var expired bool
	if err = tx.QueryRow(ctx, "SELECT $1::timestamptz IS NOT NULL AND $1::timestamptz<=statement_timestamp()", sub.ExpiresAt).Scan(&expired); err != nil {
		return Plan{}, sub, err
	}
	if expired {
		sub.Status = "expired"
	}
	if expired || p.Code == "free" {
		p, err = scanPlan(tx.QueryRow(ctx, "SELECT "+planColumns+" FROM plans WHERE code='free'"))
	}
	return p, sub, err
}

// seatUsage 计算实际占用，接受邀请时排除原预留，修改成员时排除原成员。
func seatUsage(ctx context.Context, tx pgx.Tx, tenantID, excludeMemberID, excludeInviteID string, limit int) (Usage, error) {
	result := Usage{SeatLimit: limit}
	err := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM tenant_memberships WHERE tenant_id=$1 AND can_serve AND NOT disabled AND removed_at IS NULL AND id<>COALESCE(NULLIF($2,'')::uuid,'00000000-0000-0000-0000-000000000000'::uuid)),
 (SELECT count(*) FROM member_invitations WHERE tenant_id=$1 AND can_serve AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>statement_timestamp() AND id<>COALESCE(NULLIF($3,'')::uuid,'00000000-0000-0000-0000-000000000000'::uuid))`, tenantID, excludeMemberID, excludeInviteID).Scan(&result.SeatsUsed, &result.SeatsReserved)
	return result, err
}

// CheckSeats 检查新增有效席位，必须与成员或邀请变更共用同一数据库事务。
func (s *Service) CheckSeats(ctx context.Context, tx pgx.Tx, tenantID, excludeMemberID, excludeInviteID string, additional int) error {
	if additional < 0 {
		return platform.Fail(400, "invalid_seat_change", "新增席位数量无效")
	}
	if err := s.EnsureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	p, _, err := effectivePlan(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	usage, err := seatUsage(ctx, tx, tenantID, excludeMemberID, excludeInviteID, p.SeatLimit)
	if err != nil {
		return err
	}
	if additional > 0 && usage.SeatsUsed+usage.SeatsReserved+additional > usage.SeatLimit {
		return platform.Fail(409, "seat_limit_reached", "客服坐席已达到套餐上限，请撤销待激活邀请、关闭接待资格或申请升级")
	}
	return nil
}

// CheckFeature 在租户事务内校验指定功能权益，未知功能不会默认开放。
func (s *Service) CheckFeature(ctx context.Context, tx pgx.Tx, tenantID, feature string) error {
	if !featureCodes[feature] {
		return platform.Fail(400, "unknown_feature", "未知的套餐功能")
	}
	if err := s.EnsureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	p, _, err := effectivePlan(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	if !p.Features[feature] {
		return platform.Fail(403, "feature_not_available", "当前套餐未开通此功能，请联系管理员或申请升级")
	}
	return nil
}

// platformTx 仅在真实平台身份校验后设置受控审计上下文，并再次核对数据库权限。
func (s *Service) platformTx(r *http.Request, action func(pgx.Tx) error) error {
	p := platform.Current(r)
	if !p.PlatformAdmin || p.UserID == "" {
		return platform.Fail(403, "platform_required", "需要平台管理员权限")
	}
	return pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var valid bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_platform_admin)", p.UserID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return platform.Fail(403, "platform_required", "平台管理员权限已变更")
		}
		if _, err := tx.Exec(r.Context(), "SELECT set_config('app.billing_actor',$1,true)", p.UserID); err != nil {
			return err
		}
		return action(tx)
	})
}

// billingAudit 保存安全的变更快照；全局套餐操作的租户字段为空。
func billingAudit(ctx context.Context, tx pgx.Tx, tenantID, actor, action, target string, details any) error {
	body, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO billing_audit_logs(tenant_id,actor_id,action,target_id,details) VALUES(NULLIF($1,'')::uuid,$2,$3,$4,$5)", tenantID, actor, action, target, body)
	return err
}
