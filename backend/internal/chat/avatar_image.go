// 文件作用说明：验证头像真实图片内容、限制像素分配并缩放重编码PNG，不信任扩展名或上传声明。
package chat

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"supportdesk/backend/internal/platform"
)

const avatarUploadLimit = 5 << 20

// NormalizeAvatar 解码受限PNG、JPEG、GIF或WebP头像，删除元数据并把输出限制在256像素及256KiB。
// raw 为原始图片字节，返回干净PNG和实际宽高，损坏图片不保存或转发。
func NormalizeAvatar(raw []byte) ([]byte, int, int, error) {
	invalid := platform.Fail(400, "invalid_avatar_image", "头像需为有效PNG、JPEG、GIF或WebP，最大5MiB、400万像素且边长不超过4096")
	if len(raw) == 0 || len(raw) > avatarUploadLimit {
		return nil, 0, 0, invalid
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || config.Width > 4_000_000/config.Height {
		return nil, 0, 0, invalid
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, invalid
	}
	width, height := config.Width, config.Height
	if max(width, height) > 256 {
		if width >= height {
			height, width = max(1, height*256/width), 256
		} else {
			width, height = max(1, width*256/height), 256
		}
	}
	clean := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(clean, clean.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, clean); err != nil {
		return nil, 0, 0, err
	}
	if buffer.Len() > 256*1024 {
		return nil, 0, 0, platform.Fail(400, "avatar_image_too_large", "规范化头像仍超过256KiB，请压缩或简化图片后重试")
	}
	return buffer.Bytes(), width, height, nil
}
