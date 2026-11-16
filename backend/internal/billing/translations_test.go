// 文件作用说明：验证平台内容译文不改写原始名称、价格、权益与快照。
package billing

import "testing"

// TestPlanTranslationsKeepBusinessValues 验证公开展示副本使用译文且原对象和计费字段保持不变。
func TestPlanTranslationsKeepBusinessValues(t *testing.T) {
	original := Plan{Code: "team", Name: "自定义团队版", PriceAmount: 12345, Currency: "CNY", SeatLimit: 5, Translations: map[string]PlanTranslation{"de": {Name: "Team", Description: "Für Teams"}}}
	if err := validatePlanTranslations(&original); err != nil {
		t.Fatal(err)
	}
	result := localizePlan(original, "de")
	if result.Name != "Team" || result.PriceAmount != original.PriceAmount || result.Currency != "CNY" || result.SeatLimit != 5 || original.Name != "自定义团队版" {
		t.Fatal("译文改变了业务值")
	}
	if localizePlan(original, "ja").Name != original.Name {
		t.Fatal("缺失译文改写了租户原文")
	}
	original.Translations["xx"] = PlanTranslation{Name: "Invalid"}
	if validatePlanTranslations(&original) == nil {
		t.Fatal("接受未知语言")
	}
}
