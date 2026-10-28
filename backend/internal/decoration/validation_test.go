// 文件作用说明：验证不可信装修输入、独立来源及发布请求边界，不将静态测试视为真实浏览器验收。
package decoration

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestDesignRejectsUnsafeInputs 覆盖结构化脚本注入、外部资源、缺少聊天及超大输入。
func TestDesignRejectsUnsafeInputs(t *testing.T) {
	valid := defaultDesign("schema", "测试品牌")
	var schema Schema
	if err := json.Unmarshal(valid, &schema); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, mode string
		body       json.RawMessage
	}{
		{"unknown schema field", "schema", json.RawMessage(strings.TrimSuffix(string(valid), "}") + `,"javascript":"alert(1)"}`)},
		{"second json", "schema", append(append(json.RawMessage{}, valid...), []byte(` {}`)...)},
		{"unsafe logo", "schema", json.RawMessage(strings.Replace(string(valid), `"logo_url":""`, `"logo_url":"javascript:alert(1)"`, 1))},
		{"css import", "schema", json.RawMessage(strings.Replace(string(valid), `"custom_css":""`, `"custom_css":"@import 'https://example.com/style'"`, 1))},
		{"missing chat", "schema", json.RawMessage(strings.Replace(string(valid), `"type":"chat"`, `"type":"contact"`, 1))},
		{"form", "code", json.RawMessage(`{"html":"<form><button>发送</button></form>","css":"","javascript":"GoodChat.getState()"}`)},
		{"external script", "code", json.RawMessage(`{"html":"<script src='https://example.com/a.js'></script>","css":"","javascript":"GoodChat.getState()"}`)},
		{"external css", "code", json.RawMessage(`{"html":"<div>test</div>","css":"body{background:url(https://example.com)}","javascript":"GoodChat.getState()"}`)},
		{"script close", "code", json.RawMessage(`{"html":"<div>test</div>","css":"","javascript":"GoodChat.getState();</SCRIPT>"}`)},
		{"no bridge", "code", json.RawMessage(`{"html":"<div>test</div>","css":"","javascript":"alert(1)"}`)},
		{"oversize", "schema", json.RawMessage(strings.Repeat(" ", 512*1024+1))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateDesign(test.mode, test.body); err == nil {
				t.Fatal("危险或不完整设计应被拒绝")
			}
		})
	}
	for _, mode := range []string{"schema", "code"} {
		if _, err := validateDesign(mode, defaultDesign(mode, "测试")); err != nil {
			t.Fatalf("合法%s默认草稿应通过：%v", mode, err)
		}
	}
}

// TestOriginsAndRenderHost 拒绝同源、生产同站、IP代码源以及跨版本Host伪造。
func TestOriginsAndRenderHost(t *testing.T) {
	tests := []struct {
		public, code     string
		production, want bool
	}{
		{"http://127.0.0.1:13000", "http://localhost:18081", false, true},
		{"http://localhost:18081", "http://localhost:18081", false, false},
		{"http://localhost:13000", "http://127.0.0.1:18081", false, false},
		{"https://admin.example.com", "https://code.example.com", true, false},
		{"https://admin.example.com.", "https://code.example.com", true, false},
		{"https://support.example.com", "https://goodchat-content.net", true, true},
		{"https://support.example.com", "http://goodchat-content.net", true, false},
		{"https://support.example.com", "https://goodchat-content.net/path", true, false},
	}
	for _, test := range tests {
		if err := validateOrigins(test.public, test.code, test.production); (err == nil) != test.want {
			t.Errorf("来源校验结果不符：%s %s: %v", test.public, test.code, err)
		}
	}
	service, err := New(nil, nil, platform.Config{PublicURL: "http://127.0.0.1:13000", CodeOrigin: "http://localhost:18081"})
	if err != nil {
		t.Fatal(err)
	}
	id := "20000000-0000-4000-8000-000000000001"
	raw := service.renderURL(id, "short-token")
	if !strings.HasPrefix(raw, "http://"+id+".localhost:18081/render/"+id+"?preview=") {
		t.Fatal("预览必须使用版本独立来源")
	}
	request := httptest.NewRequest("GET", "http://localhost:18081/render/"+id, nil)
	response := httptest.NewRecorder()
	service.CodeHandler().ServeHTTP(response, request)
	if response.Code != 421 {
		t.Fatalf("非版本Host必须拒绝，收到%d", response.Code)
	}
	if response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("预览令牌不能通过Referer泄露")
	}
}

// TestPublicationValidation 验证批量绑定的重复、越界和预期版本参数。
func TestPublicationValidation(t *testing.T) {
	id := "20000000-0000-4000-8000-000000000001"
	if releaseRequest(releaseInput{Version: 1, LinkIDs: []string{id, id}}, false) == nil {
		t.Fatal("重复绑定应拒绝")
	}
	if releaseRequest(releaseInput{Version: 1, ExpectedReleaseID: "wrong"}, false) == nil {
		t.Fatal("畸形线上版本应拒绝")
	}
	if releaseRequest(releaseInput{Version: 1, LinkIDs: []string{id}}, false) != nil {
		t.Fatal("首次发布应允许空旧版本")
	}
	if releaseRequest(releaseInput{ReleaseID: id, ExpectedReleaseID: id}, true) != nil {
		t.Fatal("同页面回滚请求格式应通过")
	}
}

// TestSchemaEscapesTenantText 确认租户文案无法突破静态渲染文档并执行脚本。
func TestSchemaEscapesTenantText(t *testing.T) {
	var schema Schema
	_ = json.Unmarshal(defaultDesign("schema", "<script>alert('x')</script>"), &schema)
	code := schemaCode(schema, true)
	if strings.Contains(code.HTML, "<script>") || !strings.Contains(code.HTML, "&lt;script&gt;") {
		t.Fatal("品牌文案必须按文本转义")
	}
	if !strings.Contains(schemaChatScript, "BigInt") || !strings.Contains(schemaChatScript, "withdrawn_at") {
		t.Fatal("已读游标必须保留64位精度并显示撤回消息")
	}
}

// TestSchemaCapsuleAttachmentChooser 验证新胶囊用单一选择器承接三种明确用户点击，下载仍由可信父层确认。
func TestSchemaCapsuleAttachmentChooser(t *testing.T) {
	var schema Schema
	_ = json.Unmarshal(defaultDesign("schema", "附件控件测试"), &schema)
	code := schemaCode(schema, false)
	input := regexp.MustCompile(`<input[^>]*id="gc-file"[^>]*>`).FindString(code.HTML)
	if input == "" || !strings.Contains(input, "hidden") || !strings.Contains(input, `type="file"`) || strings.Count(code.HTML, `type="file"`) != 1 {
		t.Fatal("胶囊附件菜单应复用单一系统文件选择器")
	}
	for _, marker := range []string{`id="gc-add"`, `data-kind="image"`, `data-kind="file"`, `data-kind="video"`, `aria-label="发送消息"`} {
		if !strings.Contains(code.HTML, marker) {
			t.Fatalf("缺少胶囊或附件菜单标记：%s", marker)
		}
	}
	if !strings.Contains(code.Javascript, "saveAttachment") || strings.Contains(code.Javascript, "element.download") {
		t.Fatal("沙箱文件保存必须通过可信父层，不创建子页下载链接")
	}
}
