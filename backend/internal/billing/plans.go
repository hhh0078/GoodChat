// 文件作用说明：公开查询已发布套餐，并限制只有平台管理员能够配置价格、期限与功能。
package billing

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"supportdesk/backend/internal/platform"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,39}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validID 拒绝无效UUID进入数据库，避免将输入错误作为内部故障返回。
func validID(id string) bool { return uuidPattern.MatchString(id) }

// validatePlan 校验整数价格和布尔权益，保留免费两席位长期可用的商业约定。
func validatePlan(p *Plan) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
	if !codePattern.MatchString(p.Code) || utf8.RuneCountInString(p.Name) < 1 || utf8.RuneCountInString(p.Name) > 80 || utf8.RuneCountInString(p.Description) > 2000 {
		return platform.Fail(400, "invalid_plan", "请填写有效的版本代码、名称与说明")
	}
	if p.Currency == "" {
		p.Currency = "CNY"
	}
	if p.Currency != "CNY" || p.PriceAmount < 0 || p.PriceAmount > 1000000000 || p.ValidityDays < 0 || p.ValidityDays > 36500 || p.SeatLimit < 1 || p.SeatLimit > 100000 || p.SortOrder < -100000 || p.SortOrder > 100000 {
		return platform.Fail(400, "invalid_plan", "价格、有效天数、席位或排序超出有效范围")
	}
	if p.Features == nil {
		p.Features = map[string]bool{}
	}
	for key := range p.Features {
		if !featureCodes[key] {
			return platform.Fail(400, "unknown_feature", "套餐包含尚未支持的功能代码")
		}
	}
	if p.Code == "free" && (p.PriceAmount != 0 || p.ValidityDays != 0 || p.SeatLimit != 2 || !p.IsPublic || !p.IsActive) {
		return platform.Fail(400, "free_plan_protected", "免费版固定为零元、长期有效、两个席位且保持发布启用")
	}
	return nil
}

// publicPlans 只返回同时已发布和启用的真实套餐配置。
func (s *Service) publicPlans(w http.ResponseWriter, r *http.Request) error {
	items, err := loadPlans(r.Context(), s.pool, true)
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// platformPlans 向平台管理员返回包含草稿和下架版本的完整配置列表。
func (s *Service) platformPlans(w http.ResponseWriter, r *http.Request) error {
	var items []Plan
	err := s.platformTx(r, func(tx pgx.Tx) error { var err error; items, err = loadPlans(r.Context(), tx, false); return err })
	if err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// createPlan 创建真实版本但不创建虚构订阅或支付记录，公开接口没有此能力。
func (s *Service) createPlan(w http.ResponseWriter, r *http.Request) error {
	var input struct {
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
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	p := Plan{Code: input.Code, Name: input.Name, Description: input.Description, PriceAmount: input.PriceAmount, Currency: input.Currency, ValidityDays: input.ValidityDays, SeatLimit: input.SeatLimit, Features: input.Features, IsPublic: input.IsPublic, IsActive: input.IsActive, SortOrder: input.SortOrder}
	if err := validatePlan(&p); err != nil {
		return err
	}
	err := s.platformTx(r, func(tx pgx.Tx) error {
		err := tx.QueryRow(r.Context(), `INSERT INTO plans(code,name,description,price_amount,currency,validity_days,seat_limit,features,is_public,is_active,sort_order) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id::text`, p.Code, p.Name, p.Description, p.PriceAmount, p.Currency, p.ValidityDays, p.SeatLimit, p.Features, p.IsPublic, p.IsActive, p.SortOrder).Scan(&p.ID)
		if err != nil {
			var db *pgconn.PgError
			if errors.As(err, &db) && db.Code == "23505" {
				return platform.Fail(409, "plan_code_exists", "版本代码已存在")
			}
			return err
		}
		return billingAudit(r.Context(), tx, "", platform.Current(r).UserID, "plan_created", p.ID, p)
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 201, p)
	return nil
}

// updatePlan 仅修改显式提交的字段，免费版不可被停用或改成收费版。
func (s *Service) updatePlan(w http.ResponseWriter, r *http.Request) error {
	if !validID(r.PathValue("id")) {
		return platform.Fail(400, "invalid_plan", "套餐标识无效")
	}
	var input struct {
		Code         *string          `json:"code"`
		Name         *string          `json:"name"`
		Description  *string          `json:"description"`
		PriceAmount  *int64           `json:"price_amount"`
		Currency     *string          `json:"currency"`
		ValidityDays *int             `json:"validity_days"`
		SeatLimit    *int             `json:"seat_limit"`
		Features     *map[string]bool `json:"features"`
		IsPublic     *bool            `json:"is_public"`
		IsActive     *bool            `json:"is_active"`
		SortOrder    *int             `json:"sort_order"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	var p Plan
	err := s.platformTx(r, func(tx pgx.Tx) error {
		var err error
		p, err = scanPlan(tx.QueryRow(r.Context(), "SELECT "+planColumns+" FROM plans WHERE id=$1 FOR UPDATE", r.PathValue("id")))
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Fail(404, "plan_not_found", "套餐不存在")
		}
		if err != nil {
			return err
		}
		if input.Code != nil && *input.Code != p.Code {
			return platform.Fail(400, "plan_code_immutable", "版本代码创建后不可修改")
		}
		if input.Name != nil {
			p.Name = *input.Name
		}
		if input.Description != nil {
			p.Description = *input.Description
		}
		if input.PriceAmount != nil {
			p.PriceAmount = *input.PriceAmount
		}
		if input.Currency != nil {
			p.Currency = *input.Currency
		}
		if input.ValidityDays != nil {
			p.ValidityDays = *input.ValidityDays
		}
		if input.SeatLimit != nil {
			p.SeatLimit = *input.SeatLimit
		}
		if input.Features != nil {
			p.Features = *input.Features
		}
		if input.IsPublic != nil {
			p.IsPublic = *input.IsPublic
		}
		if input.IsActive != nil {
			p.IsActive = *input.IsActive
		}
		if input.SortOrder != nil {
			p.SortOrder = *input.SortOrder
		}
		if err := validatePlan(&p); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `UPDATE plans SET name=$1,description=$2,price_amount=$3,currency=$4,validity_days=$5,seat_limit=$6,features=$7,is_public=$8,is_active=$9,sort_order=$10,updated_at=now() WHERE id=$11`, p.Name, p.Description, p.PriceAmount, p.Currency, p.ValidityDays, p.SeatLimit, p.Features, p.IsPublic, p.IsActive, p.SortOrder, p.ID); err != nil {
			return err
		}
		return billingAudit(r.Context(), tx, "", platform.Current(r).UserID, "plan_updated", p.ID, p)
	})
	if err != nil {
		return err
	}
	platform.JSON(w, 200, p)
	return nil
}
