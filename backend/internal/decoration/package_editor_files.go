// 文件作用说明：为模板编辑器提供完整私有源码、受控图片修订及保留核心脚本的多文件合并。
package decoration

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/image/draw"
)

const packageRevisionRequestLimit = 28 << 20

// packageFilePatch 区分空文本与未提供字段，二进制仅接受真实图片的严格base64。
type packageFilePatch struct {
	Path       string  `json:"path"`
	Content    *string `json:"content,omitempty"`
	DataBase64 *string `json:"data_base64,omitempty"`
}

// protectedEditorPath 禁止普通编辑器改写固定脚本、脚本合约或导出元数据。
func protectedEditorPath(name string) bool {
	return isProtectedTemplatePath(name) || strings.EqualFold(name, "template.json")
}

// packageEditorFiles 导出源码原始字节，不将二进制转换成文本或运行其中脚本。
func packageEditorFiles(pkg *staticPackage) []map[string]any {
	files := make([]map[string]any, 0, len(pkg.Files))
	for _, file := range pkg.Files {
		value := map[string]any{"path": file.Path, "content_type": file.ContentType, "size": file.Size, "sha256": file.SHA256, "protected": protectedEditorPath(file.Path)}
		if strings.HasPrefix(file.ContentType, "text/") || file.ContentType == "application/json" {
			value["content"] = string(file.Content)
		} else {
			value["data_base64"] = base64.StdEncoding.EncodeToString(file.Content)
		}
		files = append(files, value)
	}
	return files
}

// getPackageSource 只向当前租户管理员返回完整源码及保护标记，模板窗口不持有该接口凭证。
func (s *Service) getPackageSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		pkg, err := loadArchivePackage(r.Context(), tx, CodeDesign{PackageID: id})
		if err != nil {
			return nil, err
		}
		metadata, err := record(r.Context(), tx, "SELECT to_jsonb(p)-'creation_xid' FROM decoration_code_packages p WHERE id=$1", id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"package": metadata, "entrypoint": pkg.Entrypoint, "files": packageEditorFiles(pkg)}, nil
	})
}

// mergePackageFiles 将作者文件变更合并成完整新快照，不修改原包，统一校验路径、大小与资源引用。
func mergePackageFiles(base *staticPackage, entry string, changes []packageFilePatch) (*staticPackage, error) {
	if len(changes) > packageFileCount {
		return nil, invalidDesign("一次最多修改200个资源")
	}
	if entry == "" {
		entry = base.Entrypoint
	}
	files := make(map[string]packageFile, len(base.Files)+len(changes))
	for _, file := range base.Files {
		files[file.Path] = file
	}
	changed := map[string]bool{}
	inputTotal := 0
	for _, change := range changes {
		if err := packagePath(change.Path); err != nil {
			return nil, err
		}
		if protectedEditorPath(change.Path) {
			return nil, invalidDesign("编辑器不能修改固定SDK、聊天脚本或合约：" + change.Path)
		}
		if changed[change.Path] || (change.Content == nil) == (change.DataBase64 == nil) {
			return nil, invalidDesign("文件路径不能重复，每个变更须且只能提供content或data_base64")
		}
		changed[change.Path] = true
		var file packageFile
		var err error
		if change.Content != nil {
			inputTotal += len(*change.Content)
			file, err = checkStaticFile(change.Path, []byte(*change.Content))
			if err == nil && !strings.HasPrefix(file.ContentType, "text/") && file.ContentType != "application/json" {
				err = invalidDesign("图片必须以data_base64提交，文本源码不能冒充二进制")
			}
		} else {
			if len(*change.DataBase64) > base64.StdEncoding.EncodedLen(packageFileLimit) || !strings.HasPrefix(change.Path, "static/") {
				return nil, invalidDesign("图片必须放在static目录且原始大小不能超过5MiB")
			}
			var raw []byte
			raw, err = base64.StdEncoding.Strict().DecodeString(*change.DataBase64)
			inputTotal += len(raw)
			if err == nil {
				file, err = editorImageFile(change.Path, raw)
			} else {
				err = invalidDesign("图片必须使用完整合法的base64编码")
			}
		}
		if err != nil {
			return nil, err
		}
		if inputTotal > packageTotalLimit {
			return nil, invalidDesign("本次修改的原始资源总计不能超过20MiB")
		}
		files[change.Path] = file
	}
	all := make([]packageFile, 0, len(files))
	for _, file := range files {
		all = append(all, file)
	}
	return finishPackage(entry, all)
}

// editorImageFile 验证真实图片并仅在必要时缩小PNG或JPEG，保留合适的小图及动画原始字节。
func editorImageFile(name string, raw []byte) (packageFile, error) {
	file, err := checkStaticFile(name, raw)
	if err != nil {
		return file, err
	}
	if !strings.HasPrefix(file.ContentType, "image/") {
		return packageFile{}, invalidDesign("二进制修订仅支持PNG、JPEG、GIF或WebP图片")
	}
	config, _, _ := image.DecodeConfig(bytes.NewReader(raw))
	if file.ContentType != "image/png" && file.ContentType != "image/jpeg" || max(config.Width, config.Height) <= 1920 && len(raw) <= 1<<20 {
		return file, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return packageFile{}, invalidDesign("图片实际内容损坏")
	}
	width, height := config.Width, config.Height
	if max(width, height) > 1920 {
		if width >= height {
			height, width = max(1, height*1920/width), 1920
		} else {
			width, height = max(1, width*1920/height), 1920
		}
	}
	clean := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(clean, clean.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var output bytes.Buffer
	if file.ContentType == "image/jpeg" {
		err = jpeg.Encode(&output, clean, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&output, clean)
	}
	if err != nil {
		return packageFile{}, err
	}
	if width == config.Width && height == config.Height && output.Len() >= len(raw) {
		return file, nil
	}
	return checkStaticFile(name, output.Bytes())
}
