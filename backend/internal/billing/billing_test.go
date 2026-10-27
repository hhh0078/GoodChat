// 文件作用说明：验证套餐金额、免费基线、请求严格解码与功能代码输入边界，普通测试无需数据库。
package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestPlanValidationBoundaries 验证价格、长期有效和免费基线的业务边界。
func TestPlanValidationBoundaries(t *testing.T) {
	base := Plan{Code: "free", Name: "免费版", Currency: "CNY", SeatLimit: 2, IsPublic: true, IsActive: true, Features: map[string]bool{"chat": true}}
	if err := validatePlan(&base); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Plan){func(p *Plan) { p.PriceAmount = 1 }, func(p *Plan) { p.ValidityDays = 1 }, func(p *Plan) { p.SeatLimit = 3 }, func(p *Plan) { p.IsActive = false }, func(p *Plan) { p.IsPublic = false }} {
		p := base
		mutate(&p)
		if err := validatePlan(&p); err == nil {
			t.Fatal("免费基线变更应被拒绝")
		}
	}
	for _, p := range []Plan{{Code: "paid", Name: "套餐", PriceAmount: -1, SeatLimit: 2}, {Code: "paid", Name: "套餐", PriceAmount: 1000000001, SeatLimit: 2}, {Code: "paid", Name: "套餐", SeatLimit: 0}, {Code: "paid", Name: "套餐", SeatLimit: 2, ValidityDays: -1}, {Code: "paid", Name: "套餐", SeatLimit: 2, Currency: "USD"}, {Code: "paid", Name: "套餐", SeatLimit: 2, Features: map[string]bool{"unimplemented": true}}} {
		if err := validatePlan(&p); err == nil {
			t.Fatalf("非法套餐被允许: %+v", p)
		}
	}
}

// TestPriceAndTenantFieldsAreStrict 验证金额不能传浮点且客户端不能给平台套餐写入租户身份。
func TestPriceAndTenantFieldsAreStrict(t *testing.T) {
	s := New(nil, testAuthorizer{}, platform.Config{})
	for _, body := range []string{`{"code":"team","name":"团队","price_amount":12.5,"seat_limit":3}`, `{"code":"team","name":"团队","price_amount":100,"seat_limit":3,"tenant_id":"forged"}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/platform/plans", strings.NewReader(body))
		endpoint(s.createPlan)(w, r)
		if w.Code != 400 {
			t.Fatalf("非法输入得到%d: %s", w.Code, w.Body.String())
		}
	}
}

// TestUnknownFeatureFailsClosed 验证服务端拒绝未知权益，不能通过拼写变化绕过检查。
func TestUnknownFeatureFailsClosed(t *testing.T) {
	s := New(nil, testAuthorizer{}, platform.Config{})
	err := s.CheckFeature(context.Background(), nil, "tenant", "unimplemented")
	var api *platform.APIError
	if !errors.As(err, &api) || api.Code != "unknown_feature" {
		t.Fatalf("未知功能应先拒绝: %v", err)
	}
}
