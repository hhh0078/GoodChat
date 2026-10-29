// 文件作用说明：真实解码并重编码品牌图片为受控PNG，素材直接进入草稿与不可变快照，不暴露聊天附件凭证。
package decoration

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/image/draw"
	"supportdesk/backend/internal/platform"
)

// controlledImage 只返回清除元数据并限制像素后的PNG，不复用租户提交的扩展名或媒体类型。
func controlledImage(raw []byte) ([]byte, int, int, error) {
	if len(raw) == 0 || len(raw) > packageFileLimit {
		return nil, 0, 0, invalidDesign("图片不能为空且不能超过5MiB")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || !safeImageSize(config, 4_000_000, 4096) {
		return nil, 0, 0, invalidDesign("图片无法识别或超过400万像素/4096边长限制")
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, invalidDesign("图片实际内容损坏")
	}
	width, height := config.Width, config.Height
	if max(width, height) > 512 {
		if width >= height {
			height = max(1, height*512/width)
			width = 512
		} else {
			width = max(1, width*512/height)
			height = 512
		}
	}
	clean := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(clean, clean.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var result bytes.Buffer
	if err := png.Encode(&result, clean); err != nil {
		return nil, 0, 0, err
	}
	if result.Len() > 256*1024 {
		return nil, 0, 0, invalidDesign("规范化PNG仍超过256KiB，请压缩或简化Logo后重试")
	}
	return result.Bytes(), width, height, nil
}

// validateLogoData 验证客户端提交的受控data图片，防止绕过上传接口混入SVG或巨幅图。
func validateLogoData(value string) error {
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(value, prefix) || len(value) > 350000 {
		return invalidDesign("受控Logo只支持最大256KiB的PNG data图片")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(raw) > 256*1024 {
		return invalidDesign("Logo图片编码或大小不正确")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || format != "png" || !safeImageSize(config, 512*512, 512) {
		return invalidDesign("Logo必须是受控512像素以内的真实PNG")
	}
	if _, format, err := image.Decode(bytes.NewReader(raw)); err != nil || format != "png" {
		return invalidDesign("Logo实际图片无法解码")
	}
	return nil
}

// importDecorationAsset 在租户权益事务内返回可直接固定到设计快照的规范化Logo。
func (s *Service) importDecorationAsset(w http.ResponseWriter, r *http.Request) {
	if !s.packageSlot(w, r) {
		return
	}
	defer func() { <-s.packageSlots }()
	raw, _, err := readPackageMultipart(w, r, packageFileLimit)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	clean, width, height, err := controlledImage(raw)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		if err := s.checkFeature(r.Context(), tx, platform.Current(r).TenantID, "decoration"); err != nil {
			return nil, err
		}
		return map[string]any{"data_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(clean), "content_type": "image/png", "width": width, "height": height, "size": len(clean), "sha256": digest(clean)}, nil
	})
}

// examplePackage 下载与新模板相同的固定SDK静态包，示例可直接通过严格导入接口验证。
func (s *Service) examplePackage(w http.ResponseWriter, r *http.Request) {
	pkg, err := industryTemplatePackage("business", defaultTemplateScripts())
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	archive, err := encodeTemplateArchive("GoodChat 多文件静态模板示例", pkg)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="goodchat-static-example.zip"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(archive)
}
