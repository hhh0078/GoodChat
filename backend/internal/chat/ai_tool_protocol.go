// 文件作用说明：兼容供应商将单个工具调用放进已知文本标签的格式，动作仍经过原有严格参数和租户授权校验。
package chat

import (
	"encoding/json"
	"github.com/cloudwego/eino/schema"
	"strings"
)

// aiNormalizeToolResponse 只接受完整已知封套中的单个允许工具JSON，不从自然语言、代码块或多个调用中提取指令。
// 保留原供应商用量，不增加调用次数；返回true表示已转换为标准Eino工具消息。
func aiNormalizeToolResponse(out *schema.Message) bool {
	if out == nil || len(out.ToolCalls) > 0 {
		return false
	}
	raw := strings.TrimSpace(out.Content)
	if len(raw) > 32000 {
		return false
	}
	enclosed := false
	for _, pair := range [][2]string{{"<function_call>", "</function>\n</tool_call>"}, {"<function_call>", "</function_call>"}, {"<tool_call>", "</tool_call>"}} {
		if strings.HasPrefix(raw, pair[0]) && strings.HasSuffix(raw, pair[1]) {
			raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, pair[0]), pair[1]))
			enclosed = true
			break
		}
	}
	if !enclosed {
		return false
	}
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if aiDecode(raw, &call) != nil || !inStrings(call.Name, "read_knowledge_bases", "submit_reply", "request_handoff") || !strings.HasPrefix(strings.TrimSpace(string(call.Arguments)), "{") {
		return false
	}
	out.ToolCalls = []schema.ToolCall{{ID: "compat_" + newID(), Type: "function", Function: schema.FunctionCall{Name: call.Name, Arguments: string(call.Arguments)}}}
	out.Content = ""
	if out.Extra == nil {
		out.Extra = map[string]any{}
	}
	out.Extra["goodchat_tool_wrapper"] = true
	return true
}
