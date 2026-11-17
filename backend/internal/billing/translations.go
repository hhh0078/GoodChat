// 文件作用说明：校验并投影平台发布的套餐译文，保留价格权益与原始历史快照。
package billing

import (
	"strings"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
	"unicode/utf8"
)

// PlanTranslation 保存平台管理员发布的单语言套餐文案。
type PlanTranslation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// validatePlanTranslations 限制语言清单与文案长度，空译文可回退原文。
func validatePlanTranslations(plan *Plan) error {
	if plan.Translations == nil {
		plan.Translations = map[string]PlanTranslation{}
	}
	for locale, value := range plan.Translations {
		if localization.Normalize(locale) != locale || utf8.RuneCountInString(value.Name) > 80 || utf8.RuneCountInString(value.Description) > 2000 {
			return platform.Fail(400, "invalid_translation", "请检查译文语言、名称和说明长度")
		}
		value.Name = strings.TrimSpace(value.Name)
		value.Description = strings.TrimSpace(value.Description)
		plan.Translations[locale] = value
	}
	return nil
}

// localizePlan 仅在输出副本应用已发布译文，内置免费版可使用随版本发布的系统译文。
func localizePlan(plan Plan, locale string) Plan {
	plan.ContentLocale = "zh-CN"
	if value, ok := plan.Translations[locale]; ok && value.Name != "" {
		plan.Name = value.Name
		if value.Description != "" {
			plan.Description = value.Description
		}
		plan.ContentLocale = locale
		return plan
	}
	if plan.Code == "free" && plan.Name == "免费版" {
		plan.Name = localization.Format(locale, "免费版")
		if plan.Description == "长期免费，包含2个客服坐席；更多接待成员可申请升级。" {
			plan.Description = localization.Format(locale, plan.Description)
		}
		plan.ContentLocale = locale
	}
	return plan
}
