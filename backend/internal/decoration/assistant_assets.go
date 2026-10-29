// 文件作用说明：按实际格式与预算检测压缩设计参考图，保留清晰截图和透明通道，仅允许管理员访问本租户素材。
package decoration

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"

	"github.com/jackc/pgx/v5"
	"golang.org/x/image/draw"
	"supportdesk/backend/internal/platform"
)

// assistantNormalizeImage 先检测格式、像素和预算，保留无需压缩的清晰图片及透明通道；target仅用于强制生成素材的实际格式。
func assistantNormalizeImage(raw []byte, target string) ([]byte, string, int, int, error) {
	if len(raw) == 0 || len(raw) > 10<<20 {
		return nil, "", 0, 0, invalidDesign("图片不能为空且不能超过10MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || !safeImageSize(cfg, 16_000_000, 8192) {
		return nil, "", 0, 0, invalidDesign("图片无法识别或超过1600万像素及8192边长限制")
	}
	if format == "gif" {
		if target != "" {
			return nil, "", 0, 0, invalidDesign("生成素材不能将GIF动画静默转换为静态图片")
		}
		if len(raw) > 1<<20 || cfg.Width > 1600 || cfg.Height > 6000 {
			return nil, "", 0, 0, invalidDesign("GIF动画须在1MiB、宽1600像素和高6000像素以内；超限请先压缩动画")
		}
		if err := assistantGIFBudget(raw, cfg.Width, cfg.Height); err != nil {
			return nil, "", 0, 0, err
		}
		if _, err := gif.DecodeAll(bytes.NewReader(raw)); err != nil {
			return nil, "", 0, 0, invalidDesign("GIF动画实际帧数据损坏")
		}
		return raw, "image/gif", cfg.Width, cfg.Height, nil
	}
	if format != "png" && format != "jpeg" && format != "webp" {
		return nil, "", 0, 0, invalidDesign("参考图片仅支持PNG、JPEG和WebP")
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", 0, 0, invalidDesign("图片实际内容损坏")
	}
	width, height := cfg.Width, cfg.Height
	compatible := target == "" || target == ".png" && format == "png" || (target == ".jpg" || target == ".jpeg") && format == "jpeg"
	if compatible && len(raw) <= 1<<20 && width <= 1600 && height <= 6000 {
		return raw, map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format], width, height, nil
	}
	preservePNG := target == ".png" || target == "" && format != "jpeg"
	if width > 1600 {
		height = max(1, height*1600/width)
		width = 1600
	}
	if height > 6000 {
		width = max(1, width*6000/height)
		height = 6000
	}
	for attempt := 0; attempt < 5; attempt++ {
		clean := image.NewNRGBA(image.Rect(0, 0, width, height))
		op := draw.Src
		if !preservePNG {
			for i := range clean.Pix {
				clean.Pix[i] = 255
			}
			op = draw.Over
		}
		draw.CatmullRom.Scale(clean, clean.Bounds(), decoded, decoded.Bounds(), op, nil)
		var buffer bytes.Buffer
		mime := "image/jpeg"
		if preservePNG {
			err = png.Encode(&buffer, clean)
			mime = "image/png"
		} else {
			err = jpeg.Encode(&buffer, clean, &jpeg.Options{Quality: 82})
		}
		if err != nil {
			return nil, "", 0, 0, err
		}
		if buffer.Len() <= 1<<20 {
			return buffer.Bytes(), mime, width, height, nil
		}
		width = max(1, width*3/4)
		height = max(1, height*3/4)
	}
	return nil, "", 0, 0, invalidDesign("图片压缩后仍过大，请缩小图片后重试")
}

// assistantGIFBudget 在解码所有帧前扫描GIF块，限制帧数和帧总像素，避免小文件触发巨量解压分配。
func assistantGIFBudget(raw []byte, width, height int) error {
	invalid := func() error { return invalidDesign("GIF动画损坏或超过100帧及3200万帧总像素预算") }
	if len(raw) < 13 {
		return invalid()
	}
	offset := 13
	if raw[10]&0x80 != 0 {
		offset += 3 * (1 << ((raw[10] & 7) + 1))
	}
	frames, total := 0, int64(0)
	// 跳过长度前缀子块，不解释或执行扩展内容。
	skip := func() bool {
		for offset < len(raw) {
			size := int(raw[offset])
			offset++
			if size == 0 {
				return true
			}
			if size > len(raw)-offset {
				return false
			}
			offset += size
		}
		return false
	}
	for offset < len(raw) {
		marker := raw[offset]
		offset++
		switch marker {
		case 0x3b:
			if frames < 1 || offset != len(raw) {
				return invalid()
			}
			return nil
		case 0x21:
			if offset >= len(raw) {
				return invalid()
			}
			offset++
			if !skip() {
				return invalid()
			}
		case 0x2c:
			if len(raw)-offset < 9 {
				return invalid()
			}
			left := int(binary.LittleEndian.Uint16(raw[offset:]))
			top := int(binary.LittleEndian.Uint16(raw[offset+2:]))
			w := int(binary.LittleEndian.Uint16(raw[offset+4:]))
			h := int(binary.LittleEndian.Uint16(raw[offset+6:]))
			packed := raw[offset+8]
			offset += 9
			frames++
			total += int64(w) * int64(h)
			if w < 1 || h < 1 || left+w > width || top+h > height || frames > 100 || total > 32_000_000 {
				return invalid()
			}
			if packed&0x80 != 0 {
				offset += 3 * (1 << ((packed & 7) + 1))
			}
			if offset >= len(raw) {
				return invalid()
			}
			offset++
			if !skip() {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	return invalid()
}

// assistantAssetInfo 返回可直接预览的规范化图片，不暴露原始上传文件或访客凭证。
func assistantAssetInfo(id, mime string, content []byte, width, height, original int) map[string]any {
	return map[string]any{"id": id, "content_type": mime, "width": width, "height": height, "size": len(content), "original_size": original, "compressed": len(content) < original, "data_url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(content)}
}

// assistantUploadAsset 在当前租户范围保存已压缩参考图片，限制并发和租户总存储预算。
func (s *Service) assistantUploadAsset(w http.ResponseWriter, r *http.Request) {
	if !s.packageSlot(w, r) {
		return
	}
	defer func() { <-s.packageSlots }()
	raw, _, err := readPackageMultipart(w, r, 10<<20)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	clean, mime, width, height, err := assistantNormalizeImage(raw, "")
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		p := platform.Current(r)
		if err := s.checkFeature(r.Context(), tx, p.TenantID, "ai_design"); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", p.TenantID); err != nil {
			return nil, err
		}
		var total int64
		if err := tx.QueryRow(r.Context(), "SELECT coalesce(sum(octet_length(content)),0) FROM assistant_assets").Scan(&total); err != nil {
			return nil, err
		}
		if total+int64(len(clean)) > 100<<20 {
			return nil, platform.Fail(429, "assistant_asset_limit", "当前租户参考图片总量已达100MiB")
		}
		var id string
		if err := tx.QueryRow(r.Context(), "INSERT INTO assistant_assets(tenant_id,created_by,content_type,content,width,height,original_size,sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text", p.TenantID, p.MembershipID, mime, clean, width, height, len(raw), digest(clean)).Scan(&id); err != nil {
			return nil, err
		}
		return assistantAssetInfo(id, mime, clean, width, height, len(raw)), nil
	})
}

// assistantGetAsset 读取本租户参考图片，跨租户随机编号不可枚举。
func (s *Service) assistantGetAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var mime string
		var content []byte
		var width, height, original int
		err := tx.QueryRow(r.Context(), "SELECT content_type,content,width,height,original_size FROM assistant_assets WHERE id=$1", id).Scan(&mime, &content, &width, &height, &original)
		if err != nil {
			return nil, err
		}
		return assistantAssetInfo(id, mime, content, width, height, original), nil
	})
}
