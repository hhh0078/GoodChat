// 文件作用说明：验证平台头像清单及生产图片规范化的真实解码、缩放、损坏与主动内容边界。
package chat

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// avatarTestPNG 创建真实测试PNG，供上传和生产规范化共用，不以伪造扩展名替代图片字节。
func avatarTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.SetNRGBA(x, y, color.NRGBA{R: 80, G: 130, B: 190, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestAvatarLibraryCatalog 验证50张固定平台头像编号唯一且类别数量与素材合同一致。
func TestAvatarLibraryCatalog(t *testing.T) {
	items := builtinAvatars()
	if len(items) != 50 {
		t.Fatal("平台头像库必须为50张")
	}
	counts, seen := map[string]int{}, map[string]bool{}
	for _, item := range items {
		if seen[item.ID] || item.URL != "/images/avatars/"+item.ID+".png" || !item.Builtin {
			t.Fatal("头像库编号重复或地址不符合合同")
		}
		seen[item.ID] = true
		counts[item.Category]++
	}
	if counts["professional"] != 20 || counts["cartoon"] != 20 || counts["illustration"] != 10 {
		t.Fatal("平台头像类别数量错误")
	}
}

// TestNormalizeAvatarRealImage 验证保持宽高比缩到256、转换JPEG及清除源图附加主动内容。
func TestNormalizeAvatarRealImage(t *testing.T) {
	raw := append(avatarTestPNG(t, 1024, 512), []byte("<script>private-source-metadata</script>")...)
	clean, width, height, err := NormalizeAvatar(raw)
	if err != nil || width != 256 || height != 128 {
		t.Fatalf("实际规范化失败:%d %d %v", width, height, err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(clean))
	if err != nil || format != "png" || config.Width != width || config.Height != height || bytes.Contains(clean, []byte("private-source-metadata")) {
		t.Fatal("规范化输出未清除源图附加数据")
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 20, 20)), nil); err != nil {
		t.Fatal(err)
	}
	clean, _, _, err = NormalizeAvatar(jpg.Bytes())
	if err != nil || !bytes.HasPrefix(clean, []byte{137, 80, 78, 71}) {
		t.Fatal("真实JPEG应重编码为PNG")
	}
}

// TestNormalizeAvatarRejectsBrokenInputs 验证空、损坏、SVG、过大字节和非法像素边界不进入素材库。
func TestNormalizeAvatarRejectsBrokenInputs(t *testing.T) {
	valid := avatarTestPNG(t, 8, 8)
	for _, raw := range [][]byte{nil, []byte("<svg xmlns='http://www.w3.org/2000/svg'><script/></svg>"), []byte("not an image"), valid[:len(valid)/2], make([]byte, avatarUploadLimit+1), avatarTestPNG(t, 4097, 1)} {
		if _, _, _, err := NormalizeAvatar(raw); err == nil {
			t.Fatal("损坏或超限头像被接受")
		}
	}
}
