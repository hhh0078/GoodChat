// 文件作用说明：仅本地化显式任务诊断字段，保留任务需求、模型正文、设计和任意业务资料。
package localization

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var printfArgument = regexp.MustCompile(`%(?:\[\d+\])?[-+#0 ]*\d*(?:\.\d+)?[sdvTf]`)

type diagnosticPattern struct {
	key     string
	pattern *regexp.Regexp
}

var diagnosticPatterns = buildDiagnosticPatterns()

// buildDiagnosticPatterns 预编译已知格式模板，只为持久化的安全诊断恢复消息参数。
func buildDiagnosticPatterns() []diagnosticPattern {
	var result []diagnosticPattern
	for key, source := range messages["zh-CN"] {
		matches := printfArgument.FindAllStringIndex(source, -1)
		if len(matches) == 0 || len(source) > 2000 {
			continue
		}
		var pattern strings.Builder
		pattern.WriteString("(?s)^")
		start := 0
		for _, match := range matches {
			pattern.WriteString(regexp.QuoteMeta(source[start:match[0]]))
			pattern.WriteString("(.+?)")
			start = match[1]
		}
		pattern.WriteString(regexp.QuoteMeta(source[start:]))
		pattern.WriteString("$")
		result = append(result, diagnosticPattern{key, regexp.MustCompile(pattern.String())})
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].pattern.String()) != len(result[j].pattern.String()) {
			return len(result[i].pattern.String()) > len(result[j].pattern.String())
		}
		return result[i].key < result[j].key
	})
	return result
}

// Diagnostic 翻译已知诊断，未知供应商说明保留原样；返回稳定标识便于前端再次切换语言。
func Diagnostic(locale, text string) (string, string, map[string]string) {
	params := map[string]string{}
	if text == "" {
		return text, "", params
	}
	key := Key(text)
	if _, ok := messages["zh-CN"][key]; ok {
		return Format(locale, text), key, params
	}
	for _, candidate := range diagnosticPatterns {
		if matched := candidate.pattern.FindStringSubmatch(text); matched != nil {
			for index, value := range matched[1:] {
				params[fmt.Sprintf("p%d", index)] = value
			}
			index := 0
			translated := printfArgument.ReplaceAllStringFunc(messages[locale][candidate.key], func(string) string { value := matched[index+1]; index++; return value })
			return translated, candidate.key, params
		}
	}
	return text, "", params
}

// Diagnostics 只访问任务记录及明确的任务列表，不遍历正文、设计、上下文和用户扩展字段。
func Diagnostics(data any, locale string) any {
	raw, err := json.Marshal(data)
	if err != nil {
		return data
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return data
	}
	var visit func(any)
	visit = func(value any) {
		switch record := value.(type) {
		case []any:
			for _, item := range record {
				visit(item)
			}
		case map[string]any:
			if text, ok := record["error_message"].(string); ok {
				translated, key, params := Diagnostic(locale, text)
				record["error_message"] = translated
				if key != "" {
					record["error_message_key"] = key
					record["error_message_params"] = params
				}
			}
			for _, field := range []string{"items", "image_tasks", "last_job", "job"} {
				if child, ok := record[field]; ok {
					visit(child)
				}
			}
		}
	}
	visit(value)
	return value
}
