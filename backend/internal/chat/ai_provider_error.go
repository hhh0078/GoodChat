// 文件作用说明：识别已复现的供应商纯文本错误封套，避免作为业务回答、知识描述或工具协议修复继续处理。
package chat

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

// aiProviderUnavailableReply 仅接受完整已知上游错误，拒绝普通正文提及、多模态内容、工具调用及超长文本。
func aiProviderUnavailableReply(out *schema.Message) bool {
	if out == nil || out.Role != schema.Assistant || len(out.ToolCalls) != 0 || len(out.Content) > 256 || len(out.MultiContent) != 0 || len(out.UserInputMultiContent) != 0 || len(out.AssistantGenMultiContent) != 0 {
		return false
	}
	return strings.TrimSpace(out.Content) == "[上游错误] 权益不足"
}
