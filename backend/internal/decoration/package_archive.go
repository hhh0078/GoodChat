// 文件作用说明：在内存中校验ZIP目录、解压预算和实际静态文件类型，不写磁盘或执行构建命令。
package decoration

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font/sfnt"
	_ "golang.org/x/image/webp"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	packageZIPLimit   = 10 << 20
	packageTotalLimit = 20 << 20
	packageFileLimit  = 5 << 20
	packageFileCount  = 200
)

// packageFile 保存已验证文件，Content不进入资源清单JSON。
type packageFile struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	SHA256      string `json:"sha256"`
	Content     []byte `json:"-"`
}

// staticPackage 保存一次不可变导入的完整资源及排序摘要。
type staticPackage struct {
	Entrypoint string
	Files      []packageFile
	Total      int
	SHA256     string
}

// packagePath 拒绝所有需再次解码、规范化或可逃离资源目录的文件路径。
func packagePath(value string) error {
	if value == "" || len(value) > 240 || !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.ContainsAny(value, "\\%?#:") || strings.HasPrefix(value, "/") || path.Clean(value) != value || len(strings.Split(value, "/")) > 12 {
		return invalidDesign("资源路径必须是规范UTF-8相对路径，不能含穿越、编码、反斜线或查询参数")
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "." || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") || strings.HasPrefix(part, ".") {
			return invalidDesign("资源路径不能包含隐藏文件或特殊目录")
		}
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return invalidDesign("资源路径不能包含控制字符")
		}
	}
	return nil
}

// readStaticZIP 逐项限制声明与实际解压字节，拒绝符号链接、重复路径和异常压缩比。
func readStaticZIP(raw []byte, entrypoint string) (*staticPackage, error) {
	if len(raw) == 0 || len(raw) > packageZIPLimit {
		return nil, invalidDesign("ZIP不能为空且不能超过10MiB")
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, invalidDesign("无法读取ZIP，请上传无需构建的静态网站压缩包")
	}
	if len(reader.File) == 0 || len(reader.File) > packageFileCount {
		return nil, invalidDesign("ZIP总条目数必须为1到200，包含目录")
	}
	files := []packageFile{}
	seen := map[string]bool{}
	total := 0
	expanded := 0
	for _, item := range reader.File {
		name := strings.TrimSuffix(item.Name, "/")
		if err := packagePath(name); err != nil {
			return nil, err
		}
		key := cases.Fold().String(name)
		if seen[key] {
			return nil, invalidDesign("ZIP包含重名或大小写冲突路径")
		}
		seen[key] = true
		if item.Mode().IsDir() {
			continue
		}
		if !item.Mode().IsRegular() || item.Flags&1 != 0 || item.Method != zip.Store && item.Method != zip.Deflate {
			return nil, invalidDesign("ZIP只能包含普通静态文件，不能含链接、特殊文件、加密或未知压缩方式")
		}
		if item.UncompressedSize64 > packageFileLimit || item.UncompressedSize64 > uint64(packageTotalLimit-total) || item.UncompressedSize64 > 100*1024 && item.UncompressedSize64 > max(item.CompressedSize64, 1)*200 {
			return nil, invalidDesign("ZIP超过单文件5MiB、总计20MiB或200倍压缩比限制")
		}
		stream, err := item.Open()
		if err != nil {
			return nil, invalidDesign("ZIP文件无法解压")
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, packageFileLimit+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || len(content) > packageFileLimit || uint64(len(content)) != item.UncompressedSize64 {
			return nil, invalidDesign("ZIP文件校验和、声明大小或实际解压大小不正确")
		}
		expanded += len(content)
		if expanded > packageTotalLimit {
			return nil, invalidDesign("ZIP实际展开大小不能超过20MiB")
		}
		file, err := checkStaticFile(name, content)
		if err != nil {
			return nil, err
		}
		total += len(file.Content)
		if total > packageTotalLimit {
			return nil, invalidDesign("资源总大小不能超过20MiB")
		}
		files = append(files, file)
	}
	return finishPackage(entrypoint, files)
}

// finishPackage 重新检查所有引用并固定有序清单，修改源码同样经过完整校验。
func finishPackage(entrypoint string, files []packageFile) (*staticPackage, error) {
	if entrypoint == "" {
		entrypoint = "index.html"
	}
	if err := packagePath(entrypoint); err != nil {
		return nil, err
	}
	if len(files) == 0 || len(files) > packageFileCount {
		return nil, invalidDesign("资源文件数量必须为1到200")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	lookup := map[string]packageFile{}
	seen := map[string]bool{}
	total := 0
	for _, file := range files {
		key := cases.Fold().String(file.Path)
		if seen[key] {
			return nil, invalidDesign("资源路径存在大小写冲突")
		}
		seen[key] = true
		lookup[file.Path] = file
		total += file.Size
	}
	if total > packageTotalLimit {
		return nil, invalidDesign("资源总大小不能超过20MiB")
	}
	entry, ok := lookup[entrypoint]
	if !ok || entry.ContentType != "text/html; charset=utf-8" {
		return nil, invalidDesign("入口必须指向资源包内存在的UTF-8 HTML文件")
	}
	for _, file := range files {
		if err := validatePackageFile(file, lookup); err != nil {
			return nil, fmt.Errorf("%s：%w", file.Path, err)
		}
	}
	if _, err := validateTemplatePackage(entrypoint, lookup, false); err != nil {
		return nil, err
	}
	bridge := false
	for _, file := range files {
		if (strings.HasPrefix(file.ContentType, "text/html") || strings.HasPrefix(file.ContentType, "text/javascript")) && bytes.Contains(file.Content, []byte("GoodChat")) {
			bridge = true
		}
	}
	if !bridge {
		return nil, invalidDesign("代码包必须通过window.GoodChat接入当前授权聊天，请先下载示例包")
	}
	manifest, _ := json.Marshal(files)
	return &staticPackage{Entrypoint: entrypoint, Files: files, Total: total, SHA256: digest(append([]byte(entrypoint+"\n"), manifest...))}, nil
}

// checkStaticFile 依据文本编码、图片解码或二进制结构确定MIME，文件扩展名只用于检查类型一致。
func checkStaticFile(name string, content []byte) (packageFile, error) {
	if err := packagePath(name); err != nil {
		return packageFile{}, err
	}
	if len(content) > packageFileLimit {
		return packageFile{}, invalidDesign("单个资源不能超过5MiB")
	}
	ext := strings.ToLower(path.Ext(name))
	mime := ""
	switch ext {
	case ".html", ".htm", ".css", ".js", ".mjs", ".json", ".map", ".txt":
		content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(content) || bytes.ContainsRune(content, 0) {
			return packageFile{}, invalidDesign("源码只支持UTF-8编码，不能包含NUL或UTF-16")
		}
		mime = map[string]string{".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".json": "application/json", ".map": "application/json", ".txt": "text/plain; charset=utf-8"}[ext]
		if (ext == ".json" || ext == ".map") && !json.Valid(content) {
			return packageFile{}, invalidDesign("JSON资源内容无效")
		}
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		config, format, err := image.DecodeConfig(bytes.NewReader(content))
		if err != nil || !safeImageSize(config, 16_000_000, 8192) {
			return packageFile{}, invalidDesign("图片无法解码或超过1600万像素/8192边长限制")
		}
		if _, _, err = image.Decode(bytes.NewReader(content)); err != nil {
			return packageFile{}, invalidDesign("图片实际内容损坏")
		}
		mime = map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
		if mime != map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}[ext] {
			return packageFile{}, invalidDesign("图片扩展名与实际解码类型不一致")
		}
	case ".woff", ".woff2":
		magic := "wOFF"
		minimum := 44
		if ext == ".woff2" {
			magic = "wOF2"
			minimum = 48
		}
		if len(content) < minimum || string(content[:4]) != magic || binary.BigEndian.Uint32(content[8:12]) != uint32(len(content)) || binary.BigEndian.Uint16(content[12:14]) == 0 || binary.BigEndian.Uint16(content[14:16]) != 0 {
			return packageFile{}, invalidDesign("字体文件头或长度不正确")
		}
		mime = "font/" + strings.TrimPrefix(ext, ".")
	case ".ttf", ".otf":
		if _, err := sfnt.Parse(content); err != nil {
			return packageFile{}, invalidDesign("字体实际结构无法解析")
		}
		mime = "font/" + strings.TrimPrefix(ext, ".")
	case ".mp3", ".wav", ".ogg", ".webm", ".mp4":
		mime = http.DetectContentType(content)
		expected := map[string][]string{".mp3": {"audio/mpeg"}, ".wav": {"audio/wave", "audio/x-wav"}, ".ogg": {"application/ogg"}, ".webm": {"video/webm"}, ".mp4": {"video/mp4"}}[ext]
		ok := false
		for _, value := range expected {
			if mime == value {
				ok = true
			}
		}
		if !ok {
			return packageFile{}, invalidDesign("音视频扩展名与实际媒体类型不一致")
		}
	default:
		return packageFile{}, invalidDesign("不支持此文件类型；可导入HTML/CSS/JS/JSON、PNG/JPEG/GIF/WebP、字体和常见静态音视频，SVG、服务端源码、可执行文件和构建依赖请移除")
	}
	return packageFile{Path: name, ContentType: mime, Size: len(content), SHA256: digest(content), Content: content}, nil
}

// safeImageSize 在实际像素分配前检查乘法边界和最大边长。
func safeImageSize(config image.Config, pixels, edge int) bool {
	return config.Width > 0 && config.Height > 0 && config.Width <= edge && config.Height <= edge && int64(config.Width)*int64(config.Height) <= int64(pixels)
}
