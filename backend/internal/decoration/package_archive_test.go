// 文件作用说明：验证真实ZIP解压、HTML实体与编码、相对依赖、素材解码和静态包安全边界，不启动浏览器。
package decoration

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"io/fs"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// zipFixture 构造受控内存ZIP，可选择特殊文件模式用于安全负例。
func zipFixture(t *testing.T, files map[string][]byte, mode fs.FileMode) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for name, body := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		if mode != 0 {
			header.SetMode(mode)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestPackageRevisionRequestBudget 验证文本修订请求超过普通1MiB仍合法，同时拒绝未知字段及第二个JSON对象。
func TestPackageRevisionRequestBudget(t *testing.T) {
	input := map[string]any{"files": []any{map[string]any{"path": "large.txt", "content": strings.Repeat("合法文本", 100000)}}}
	raw, _ := json.Marshal(input)
	var parsed packageRevisionInput
	if err := decodePackageRevision(httptest.NewRecorder(), httptest.NewRequest("POST", "http://trusted.invalid/revisions", bytes.NewReader(raw)), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Files) != 1 || len(parsed.Files[0].Content) <= 1<<20 {
		t.Fatal("大文本测试没有覆盖公共1MiB上限")
	}
	for _, raw := range []string{`{"tenant_id":"untrusted","files":[]}`, `{"files":[]} {"files":[]}`} {
		if err := decodePackageRevision(httptest.NewRecorder(), httptest.NewRequest("POST", "http://trusted.invalid/revisions", strings.NewReader(raw)), &parsed); err == nil {
			t.Fatal("修订请求必须严格匹配单个JSON结构")
		}
	}
}

// packageFixtureFiles 返回带相对模块、嵌套CSS引用和实际PNG的可运行静态网站。
func packageFixtureFiles(t *testing.T) map[string][]byte {
	t.Helper()
	var imageData bytes.Buffer
	_ = png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 2, 3)))
	return map[string][]byte{
		"site/index.html":            []byte(`<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="./css/main.css"></head><body><h1>资源包中文</h1><img src="./logo.png" alt="Logo"><script type="module" src="./js/main.mjs"></script></body></html>`),
		"site/css/main.css":          []byte(`@import "./nested/colors.css"; body{background-image:url('../logo.png')}`),
		"site/css/nested/colors.css": []byte(`h1{color:#1f6f5b}`),
		"site/js/main.mjs":           []byte(`import { text } from './helpers/words.mjs'; window.GoodChat.ready().then(()=>{document.querySelector('h1').textContent=text});`),
		"site/js/helpers/words.mjs":  []byte(`export const text='中文资源包';`),
		"site/logo.png":              imageData.Bytes(),
	}
}

// TestZIPImportAndRelativeReferences 验证BOM规范化、HTML解析与多层预览资源引用保持短凭证路径。
func TestZIPImportAndRelativeReferences(t *testing.T) {
	files := packageFixtureFiles(t)
	files["site/index.html"] = append([]byte{0xef, 0xbb, 0xbf}, files["site/index.html"]...)
	pkg, err := readStaticZIP(zipFixture(t, files, 0), "site/index.html")
	if err != nil {
		t.Fatal(err)
	}
	lookup := map[string]packageFile{}
	for _, file := range pkg.Files {
		lookup[file.Path] = file
		if bytes.HasPrefix(file.Content, []byte{0xef, 0xbb, 0xbf}) {
			t.Fatal("UTF8 BOM应被规范化")
		}
	}
	prefix := "/preview-assets/10000000-0000-4000-8000-000000000001/short-preview-token-123456/"
	body, err := packageHTML(lookup[pkg.Entrypoint].Content, pkg.Entrypoint, prefix, "nonce-123", "window.GoodChat={};", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(prefix+"site/js/main.mjs")) || !bytes.Contains(body, []byte(prefix+"site/css/main.css")) || bytes.Index(body, []byte("window.GoodChat={}")) > bytes.Index(body, []byte("site/js/main.mjs")) {
		t.Fatal("SDK必须先于租户脚本注入，资源必须固定到预览目录")
	}
	css, err := packageCSS(string(files["site/css/main.css"]), "site/css/main.css", prefix, lookup)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{prefix + "site/css/nested/colors.css", prefix + "site/logo.png"} {
		if !strings.Contains(css, path) {
			t.Fatalf("CSS引用没有继承短凭证：%s", path)
		}
	}
	if err := packageJS(string(files["site/js/main.mjs"]), "site/js/main.mjs", lookup); err != nil {
		t.Fatal(err)
	}
	for _, js := range []string{`import './helpers/words.mjs'`, `export {text} from './helpers/words.mjs'`, `import('./helpers/words.mjs')`, `import('\x2e/helpers/words.mjs')`} {
		if err := packageJS(js, "site/js/main.mjs", lookup); err != nil {
			t.Fatalf("应支持明确相对模块：%s %v", js, err)
		}
	}
}

// TestZIPRejectsUnsafePathsAndModes 覆盖目录穿越、编码别名、大小写冲突、链接、特殊文件和压缩炸弹。
func TestZIPRejectsUnsafePathsAndModes(t *testing.T) {
	for _, name := range []string{"../index.html", "/index.html", "a/../../index.html", "a\\index.html", "%2e%2e/index.html", "%252e/index.html", "a/./index.html", "a//index.html", "a\x00.html", "a/.hidden", "a/e\u0301.html", "a/file."} {
		t.Run(name, func(t *testing.T) {
			if _, err := readStaticZIP(zipFixture(t, map[string][]byte{name: []byte("GoodChat")}, 0), name); err == nil {
				t.Fatal("危险路径被接受")
			}
		})
	}
	for _, mode := range []fs.FileMode{fs.ModeSymlink | 0777, fs.ModeNamedPipe | 0600, fs.ModeDevice | 0600} {
		if _, err := readStaticZIP(zipFixture(t, map[string][]byte{"index.html": []byte("GoodChat")}, mode), "index.html"); err == nil {
			t.Fatal("链接或特殊文件不能导入")
		}
	}
	duplicate := packageFixtureFiles(t)
	duplicate["site/LOGO.png"] = duplicate["site/logo.png"]
	if _, err := readStaticZIP(zipFixture(t, duplicate, 0), "site/index.html"); err == nil {
		t.Fatal("大小写路径冲突应拒绝")
	}
	large := map[string][]byte{"large.txt": bytes.Repeat([]byte("x"), packageFileLimit+1)}
	if _, err := readStaticZIP(zipFixture(t, large, 0), "index.html"); err == nil {
		t.Fatal("超大文件应拒绝")
	}
	count := map[string][]byte{}
	for i := 0; i <= packageFileCount; i++ {
		count[strings.Repeat("x", i+1)+".txt"] = []byte("x")
	}
	if _, err := readStaticZIP(zipFixture(t, count, 0), ""); err == nil {
		t.Fatal("条目数超限应拒绝")
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	file, _ := writer.Create("bomb.txt")
	_, _ = file.Write(bytes.Repeat([]byte("0"), 1<<20))
	_ = writer.Close()
	if _, err := readStaticZIP(out.Bytes(), ""); err == nil {
		t.Fatal("高压缩比ZIP应拒绝")
	}
	corrupt := zipFixture(t, map[string][]byte{"index.html": []byte("unique-checksum-GoodChat")}, 0)
	at := bytes.Index(corrupt, []byte("unique-checksum"))
	corrupt[at] = 'X'
	if _, err := readStaticZIP(corrupt, ""); err == nil {
		t.Fatal("CRC校验失败不能导入")
	}
}

// TestPackageRejectsExternalAndEncodedReferences 验证实体、CSS转义、多轮编码、缺失依赖和错误MIME不会绕过包内白名单。
func TestPackageRejectsExternalAndEncodedReferences(t *testing.T) {
	base := packageFixtureFiles(t)
	pkg, err := readStaticZIP(zipFixture(t, base, 0), "site/index.html")
	if err != nil {
		t.Fatal(err)
	}
	lookup := map[string]packageFile{}
	for _, file := range pkg.Files {
		lookup[file.Path] = file
	}
	for _, markup := range []string{
		`<img src="https://evil.invalid/a.png">`, `<img src="&#x68;ttps://evil.invalid/a.png">`, `<img src="&#47;&#47;evil.invalid/a.png">`,
		`<img src="%252e%252e/secret.png">`, `<img src="%2e%2e/%2e%2e/secret.png">`, `<img src="./missing.png">`, `<img src="./js/main.mjs">`,
		`<meta http-equiv="refresh" content="0;url=https://evil.invalid">`, `<meta charset="utf-16">`, `<meta charset="utf16" charset="utf8">`,
		`<base href="https://evil.invalid">`, `<iframe src="./index.html"></iframe>`, `<form></form>`, `<svg><script>GoodChat</script></svg>`,
		`<script type="importmap">{"imports":{}}</script>`, `<a href="./index.html" target="_top">escape</a>`, `<img src="./logo.png" onerror="alert(1)">`,
		`<style>body{background:u\72 l(https://evil.invalid/a)}</style>`, `<div style="background:url(&#x68;ttps://evil.invalid/a)"></div>`,
	} {
		if _, err := packageHTML([]byte(markup), "site/index.html", "", "", "", lookup); err == nil {
			t.Errorf("危险HTML被接受：%s", markup)
		}
	}
	for _, css := range []string{`@import 'https://evil.invalid/style.css';`, `body{background:u\72l(https://evil.invalid/a)}`, `body{background:u\72 l(https://evil.invalid/a)}`, `body{background:url(\68 ttps://evil.invalid/a)}`, `@\69mport '//evil.invalid/a.css';`, `body{background:url(%252e%252e/a.png)}`} {
		if _, err := packageCSS(css, "site/css/main.css", "", lookup); err == nil {
			t.Errorf("危险CSS被接受：%s", css)
		}
	}
	for _, js := range []string{`import 'react'`, `import 'https://evil.invalid/x.js'`, `import('\x68ttps://evil.invalid/a.js')`, `import('/site/js/main.mjs')`, `import(name)`, `import('./missing.mjs')`, `import('../../../../a.js')`} {
		if err := packageJS(js, "site/js/main.mjs", lookup); err == nil {
			t.Errorf("不支持的模块依赖被接受：%s", js)
		}
	}
}

// TestActualMIMEAndControlledLogo 拒绝文件名伪装、巨幅图片与非法编码，真实PNG可规范化并显示在Schema。
func TestActualMIMEAndControlledLogo(t *testing.T) {
	for name, raw := range map[string][]byte{"fake.png": []byte("<html>not png</html>"), "fake.woff2": []byte("not a font"), "bad.js": {0xff, 0xfe, 0x00, 0x41}, "bad.css": []byte("a\x00{}"), "bad.svg": []byte(`<svg/>`), "bad.json": []byte("not json")} {
		if _, err := checkStaticFile(name, raw); err == nil {
			t.Errorf("实际类型或编码校验未生效：%s", name)
		}
	}
	files := packageFixtureFiles(t)
	raw := files["site/logo.png"]
	clean, w, h, err := controlledImage(raw)
	if err != nil || w != 2 || h != 3 {
		t.Fatalf("真实Logo规范化失败：%v", err)
	}
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(clean)
	if err := validateLogoData(data); err != nil {
		t.Fatal(err)
	}
	var schema Schema
	_ = strictJSON(defaultDesign("schema", "品牌"), &schema)
	schema.Brand.LogoURL = data
	if !strings.Contains(schemaCode(schema, true).HTML, data) {
		t.Fatal("受控Logo应在隔离Schema中可见")
	}
	oversized := append([]byte{}, raw...)
	binary.BigEndian.PutUint32(oversized[16:20], 100000)
	binary.BigEndian.PutUint32(oversized[20:24], 100000)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	if _, _, _, err := controlledImage(oversized); err == nil {
		t.Fatal("必须在实际Decode分配前拒绝巨幅图片")
	}
	if err := validateLogoData("data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg/>`))); err == nil {
		t.Fatal("Logo不能按data MIME信任未解码字节")
	}
}

// TestDownloadableExampleIsImportable 确认提供给租户的示例是真实可导入多文件包，并包含现有SDK聊天脚本。
func TestDownloadableExampleIsImportable(t *testing.T) {
	s := &Service{}
	w := httptest.NewRecorder()
	s.examplePackage(w, httptest.NewRequest("GET", "http://trusted.invalid/example", nil))
	if w.Code != 200 {
		t.Fatalf("示例ZIP失败：%d", w.Code)
	}
	pkg, err := readStaticZIP(w.Body.Bytes(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Files) < 5 {
		t.Fatal("示例必须包含真实多文件资源")
	}
	if target := os.Getenv("GOODCHAT_PACKAGE_FIXTURE"); target != "" {
		if err := os.WriteFile(target, w.Body.Bytes(), 0600); err != nil {
			t.Fatal("无法保存无敏感验收示例ZIP")
		}
	}
}
