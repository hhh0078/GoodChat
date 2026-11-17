// 文件作用说明：验证图片压缩不把Base64字符计为Token，并限制损坏图片及不支持多模态的模型。
package chat

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// TestAIImageInputAccounting 验证图片按请求次数计量而非按链接或Base64长度额外扣费。
func TestAIImageInputAccounting(t *testing.T) {
	raw := aiProbeImage()
	long := raw + raw + raw
	message := &schema.Message{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: "看图"}, {Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: &raw, MIMEType: "image/jpeg"}}}}}
	bound, count := aiInputBound([]*schema.Message{message})
	message.UserInputMultiContent[1].Image.Base64Data = &long
	next, _ := aiInputBound([]*schema.Message{message})
	if count != 1 || bound != next {
		t.Fatal("Base64字节不应当作文本Token计费")
	}
	if _, e := aiNormalizeImage([]byte("not an image")); e == nil {
		t.Fatal("损坏图片未拒绝")
	}
	j := aiJobData{Snapshot: aiSnapshot{Images: []aiImageReference{{Index: 0}}, History: []*schema.Message{schema.UserMessage("")}}}
	s := &Service{}
	if _, e := s.aiHistoryMessages(context.Background(), &j); e == nil {
		t.Fatal("不支持图片的模型不应继续调用")
	}
}
