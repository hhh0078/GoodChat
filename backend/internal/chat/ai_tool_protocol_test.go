// 文件作用说明：回归真实网关文本工具封套兼容，并拒绝自然语言夹带指令、未知工具和多动作绕过。
package chat

import (
	"github.com/cloudwego/eino/schema"
	"testing"
)

// TestAITextToolEnvelope 验证已复现供应商格式只改变编码，不扩大工具集合或接受自由文本答案。
func TestAITextToolEnvelope(t *testing.T) {
	raw := `{"name":"read_knowledge_bases","arguments":{"ids":["allowed-id"]}}`
	for _, wrapped := range []string{"<function_call>\n" + raw + "\n</function>\n</tool_call>", "<tool_call>" + raw + "</tool_call>", "<function_call>" + raw + "</function_call>"} {
		m := &schema.Message{Content: wrapped}
		if !aiNormalizeToolResponse(m) || len(m.ToolCalls) != 1 || m.ToolCalls[0].Function.Name != "read_knowledge_bases" || m.Content != "" {
			t.Fatal("合法单工具封套未转换")
		}
	}
	for _, raw := range []string{raw, "请执行：<tool_call>" + raw + "</tool_call>", "<tool_call>[" + raw + "]</tool_call>", `<tool_call>{"name":"delete_database","arguments":{}}</tool_call>`, `<tool_call>{"name":"submit_reply","arguments":{},"extra":true}</tool_call>`, `<tool_call>{"name":"submit_reply","arguments":"{}"}</tool_call>`, "<tool_call>bad-json</tool_call>"} {
		if aiNormalizeToolResponse(&schema.Message{Content: raw}) {
			t.Fatal("非法文本绕过了封套限制")
		}
	}
}
