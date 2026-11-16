// 文件作用说明：校验平台公共模板的可选发布译文，租户私有内容与原始设计保持原样。
package decoration

import (
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
	"unicode/utf8"
)

// templateTranslation 保存平台管理员明确发布的单语言模板信息。
type templateTranslation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// validateTemplateTranslations 限制支持语言和字段长度，不调用运行时翻译服务。
func validateTemplateTranslations(values map[string]templateTranslation) error {
	for locale, value := range values {
		if localization.Normalize(locale) != locale || utf8.RuneCountInString(value.Name) > 100 || utf8.RuneCountInString(value.Description) > 2000 {
			return platform.Fail(400, "invalid_translation", "请检查译文语言、名称和说明长度")
		}
	}
	return nil
}
