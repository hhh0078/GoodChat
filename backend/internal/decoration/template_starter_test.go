// 文件作用说明：验证外部AI可移植基础包、版本往返与悬浮模式的非破坏性渲染。
package decoration

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStarterContractAndVersion 保证基础包文档可导入、导出保留作品版本，固定核心仍受严格保护。
func TestStarterContractAndVersion(t *testing.T) {
	pkg, err := starterPackage()
	if err != nil {
		t.Fatal(err)
	}
	files := templateFileMap(pkg)
	for _, name := range []string{"SKILL.md", "README.md", "PROMPT.md"} {
		if len(files[name].Content) < 100 || files[name].ContentType != "text/plain; charset=utf-8" {
			t.Fatal("基础包缺少安全的说明文档", name)
		}
	}
	archive, err := encodeTemplateArchive("验收副本", pkg)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := templateZIPMetadata(archive)
	if err != nil || metadata.Version != starterVersion || metadata.SDKVersion != defaultTemplateScripts().Version {
		t.Fatal("导出丢失作品版本或核心版本", err)
	}
	imported, err := readStaticZIP(archive, metadata.Entrypoint)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyNewTemplatePackage(context.Background(), nil, imported); err != nil {
		t.Fatal(err)
	}
	for _, file := range imported.Files {
		if file.Path == templateSDKPath {
			file.Content = append(file.Content, []byte("/*篡改*/")...)
			files[file.Path] = file
		}
	}
	if _, err := validateTemplatePackage("index.html", files, true); err == nil {
		t.Fatal("修改固定SDK不能通过合约校验")
	}
}

// TestWidgetPresentationIsIsolated 覆盖七行业嵌入渲染，原包和普通页面字节不变，偏好不能携带任意查询参数。
func TestWidgetPresentationIsIsolated(t *testing.T) {
	for _, item := range industryTemplateCatalog() {
		t.Run(item.Key, func(t *testing.T) {
			pkg, err := industryTemplatePackage(item.Key, defaultTemplateScripts())
			if err != nil {
				t.Fatal(err)
			}
			original := templateFileMap(pkg)["index.html"].Content
			before := bytes.Clone(original)
			normal := compactWidgetDocument(httptest.NewRequest("GET", "http://render.test/", nil), original)
			if !bytes.Equal(normal, before) {
				t.Fatal("普通页面被改变")
			}
			rendered := compactWidgetDocument(httptest.NewRequest("GET", "http://render.test/?embed=1", nil), original)
			if !bytes.Contains(rendered, []byte("gc-embed-chat")) || !bytes.Contains(rendered, []byte("gc-embedded-document")) {
				t.Fatal("没有找到聊天主区")
			}
			if !bytes.Equal(original, before) {
				t.Fatal("不可变包被修改")
			}
			for id := range templateRequiredNodes {
				if !bytes.Contains(rendered, []byte(`id="`+id+`"`)) {
					t.Fatal("丢失官方节点", id)
				}
			}
		})
	}
	for _, query := range []string{"embed=1", "lang=en&embed=1&timezone=UTC"} {
		if !validRenderPreferences(httptest.NewRequest("GET", "http://render.test/?"+query, nil)) {
			t.Fatal("合法嵌入被拒绝")
		}
	}
	for _, query := range []string{"embed=0", "embed=1&embed=1", "token=secret"} {
		if validRenderPreferences(httptest.NewRequest("GET", "http://render.test/?"+query, nil)) {
			t.Fatal("非法偏好被接受")
		}
	}
	target := localizedRenderTarget(httptest.NewRequest("GET", "http://render.test/?embed=1", nil), "/assets/index.html")
	if !strings.Contains(target, "embed=1") {
		t.Fatal("重定向丢失嵌入模式")
	}
}

// TestTemplateContentVersionValidation 保留无作品版本的历史ZIP，拒绝无界或路径形式的版本号。
func TestTemplateContentVersionValidation(t *testing.T) {
	for _, version := range []string{"", "1.0.0", "999999.0.2"} {
		raw := zipFixture(t, map[string][]byte{"template.json": []byte(`{"schema_version":1,"entrypoint":"index.html","version":"` + version + `"}`)}, 0)
		if _, err := templateZIPMetadata(raw); err != nil {
			t.Fatal("合法或历史作品版本被拒绝", err)
		}
	}
	for _, version := range []string{"../1.0.0", "v1.0", "1000000.0.0", "1.2.3.4"} {
		raw := zipFixture(t, map[string][]byte{"template.json": []byte(`{"schema_version":1,"entrypoint":"index.html","version":"` + version + `"}`)}, 0)
		if _, err := templateZIPMetadata(raw); err == nil {
			t.Fatal("非法作品版本被接受", version)
		}
	}
}
