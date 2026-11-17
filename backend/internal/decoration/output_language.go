// 文件作用说明：指定 AI 新生成说明和页面的默认语言，保留用户原始需求与明确的内容语言选择。
package decoration

import (
	"fmt"
	"supportdesk/backend/internal/localization"
)

// outputLanguageInstruction 只控制生成语言，不改变 JSON 协议、平台控件或已有业务内容。
func outputLanguageInstruction(locale string) string {
	locale = localization.Normalize(locale)
	if locale == "" {
		locale = "zh-CN"
	}
	return fmt.Sprintf("Default output locale: %s. Write user-visible explanations and newly authored page copy in this language. If the user explicitly requests a different content language, follow that request. Preserve existing brand names, customer content, identifiers, code keys and the required JSON structure. Set the generated page language appropriately.", locale)
}
