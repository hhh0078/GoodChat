// 文件作用说明：读取当前会话授权的访客图片并压缩成多模态输入，图片费用仅采信供应商实际输入Token。
package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"supportdesk/backend/internal/platform"
)

// aiImageReference 只保存原消息及附件编号，不把图片字节重复写入任务快照。
type aiImageReference struct {
	Index        int    `json:"index"`
	MessageID    string `json:"message_id"`
	AttachmentID string `json:"attachment_id"`
}

// aiNormalizeImage 先检查像素上限，再去除元数据并缩放到最长1024像素，限制多模态传输容量。
func aiNormalizeImage(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 10<<20 {
		return "", errors.New("ai_image_unavailable")
	}
	config, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 20000000 {
		return "", errors.New("ai_image_unavailable")
	}
	original, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return "", errors.New("ai_image_unavailable")
	}
	width, height := config.Width, config.Height
	if width > 1024 || height > 1024 {
		if width >= height {
			height = height * 1024 / width
			width = 1024
		} else {
			width = width * 1024 / height
			height = 1024
		}
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(canvas, canvas.Bounds(), original, original.Bounds(), draw.Over, nil)
	var output bytes.Buffer
	if e = jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 85}); e != nil || output.Len() > 2<<20 {
		return "", errors.New("ai_image_unavailable")
	}
	return base64.StdEncoding.EncodeToString(output.Bytes()), nil
}

// aiReadInputImage 重新验证原始访客消息可见且未撤回，不使用模型或访客提供的外部URL。
func (s *Service) aiReadInputImage(ctx context.Context, j *aiJobData, ref aiImageReference) (string, error) {
	var key string
	e := platform.TenantTx(ctx, s.pool, j.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT a.storage_key FROM attachments a JOIN messages m ON m.attachment_id=a.id AND m.tenant_id=a.tenant_id WHERE m.id=$1 AND m.conversation_id=$2 AND m.attachment_id=$3 AND m.sender_kind='visitor' AND m.message_type='image' AND m.audience='public' AND m.withdrawn_at IS NULL AND a.content_type LIKE 'image/%'`, ref.MessageID, j.Conversation, ref.AttachmentID).Scan(&key)
	})
	if e != nil || !uuidPattern.MatchString(key) {
		return "", errors.New("ai_image_unavailable")
	}
	file, e := os.Open(filepath.Join(s.cfg.DataDir, "chat-attachments", key))
	if e != nil {
		return "", errors.New("ai_image_unavailable")
	}
	defer file.Close()
	raw, e := io.ReadAll(io.LimitReader(file, 10<<20+1))
	if e != nil {
		return "", errors.New("ai_image_unavailable")
	}
	return aiNormalizeImage(raw)
}

// aiHistoryMessages 仅在执行时把最多三张最近访客图片放入Eino多模态消息，任务快照不保存Base64。
func (s *Service) aiHistoryMessages(ctx context.Context, j *aiJobData) ([]*schema.Message, error) {
	messages := make([]*schema.Message, len(j.Snapshot.History))
	for i, item := range j.Snapshot.History {
		copy := *item
		messages[i] = &copy
	}
	if len(j.Snapshot.Images) > 0 && !j.Snapshot.Model.SupportsImages {
		return nil, errors.New("ai_images_not_supported")
	}
	for _, ref := range j.Snapshot.Images {
		if ref.Index < 0 || ref.Index >= len(messages) {
			return nil, errors.New("ai_image_unavailable")
		}
		data, e := s.aiReadInputImage(ctx, j, ref)
		if e != nil {
			return nil, e
		}
		text := messages[ref.Index].Content
		if text == "" {
			text = "Visitor sent an image. Inspect it and respond or request a human if unclear."
		}
		messages[ref.Index].Content = ""
		messages[ref.Index].UserInputMultiContent = []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: text}, {Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: &data, MIMEType: "image/jpeg"}, Detail: schema.ImageURLDetailAuto}}}
	}
	return messages, nil
}

// aiInputBound 对文字按字节保守估计、图片预留独立上限，不把Base64字符当文本Token收费。
func aiInputBound(messages []*schema.Message) (int, int) {
	size, count := 2048, 0
	for _, m := range messages {
		size += len(m.Content) + 128
		for _, part := range m.UserInputMultiContent {
			if part.Image != nil {
				size += 8192
				count++
			} else {
				size += len(part.Text) + 64
			}
		}
		for _, call := range m.ToolCalls {
			size += len(call.Function.Arguments) + len(call.Function.Name) + 128
		}
	}
	return size, count
}

// aiProbeImage 创建无客户数据的纯红图片，供平台连接测试验证图片输入协议。
func aiProbeImage() string {
	canvas := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	var output bytes.Buffer
	_ = jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 85})
	return base64.StdEncoding.EncodeToString(output.Bytes())
}
