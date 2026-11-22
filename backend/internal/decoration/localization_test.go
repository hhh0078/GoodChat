// 文件作用说明：验证沙箱本地化只影响系统控件、默认输出语言与静态启动数据，业务正文保持原文。
package decoration

import (
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"strings"
	"supportdesk/backend/internal/localization"
	"testing"
)

// TestLocalizedControlsKeepCustomerContent 验证客户正文和品牌名称不会被控件翻译改写。
func TestLocalizedControlsKeepCustomerContent(t *testing.T) {
	raw := []byte(`<html lang="zh-CN"><body><h1>系统</h1><div id="gc-messages"><span class="gc-message">系统</span></div><div id="gc-compose"><button id="gc-send">发送</button><textarea id="gc-text" placeholder="输入消息…"></textarea></div></body></html>`)
	result := string(localizeChatControls(raw, "de"))
	if !strings.Contains(result, `<h1>系统</h1>`) || !strings.Contains(result, `class="gc-message">系统</span>`) {
		t.Fatal("业务正文被改写")
	}
	if !strings.Contains(result, localization.Text("de", "发送")) || !strings.Contains(result, `lang="de"`) {
		t.Fatal("系统控件未本地化")
	}
}

// TestVisitorBootUsesSelectedLocale 验证独立文档启动数据包含所选语言时区且不能闭合脚本标签。
func TestVisitorBootUsesSelectedLocale(t *testing.T) {
	r := httptest.NewRequest("GET", "/?lang=fr&timezone=Europe%2FParis", nil)
	boot := visitorLocaleBoot(r)
	var config struct {
		Locale   string            `json:"locale"`
		Timezone string            `json:"timezone"`
		Messages map[string]string `json:"messages"`
	}
	if err := json.NewDecoder(strings.NewReader(strings.TrimPrefix(boot, "window.GoodChatI18n="))).Decode(&config); err != nil {
		t.Fatal(err)
	}
	if config.Locale != "fr" || config.Timezone != "Europe/Paris" || config.Messages[localization.Key("发送")] != localization.Text("fr", "发送") {
		t.Fatal("沙箱语言时区不一致")
	}
	if strings.Contains(strings.ToLower(boot), "</script") {
		t.Fatal("启动数据可闭合脚本")
	}
}

// TestVisitorTranslationKeepsExplicitParameters 验证中文同文翻译不会被兼容正则覆盖文件名、客服名等显式参数。
func TestVisitorTranslationKeepsExplicitParameters(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要Node运行真实翻译函数")
	}
	start := strings.Index(visitorLocalizationScript, "function translate(source")
	end := strings.Index(visitorLocalizationScript, "\n  window.gcTranslate")
	if start < 0 || end <= start {
		t.Fatal("找不到真实翻译函数")
	}
	source := `const assert=require('node:assert/strict');const messages={};const patterns=[{source:'下载 {p0}',text:'下载 {p0}',matcher:/^下载 (.+?)$/,names:['p0']}];` + visitorLocalizationScript[start:end] + `
assert.equal(translate('下载 {p0}',{p0:'合同.txt'}),'下载 合同.txt');
assert.equal(translate('客服 {p0}',{p0:'张三'}),'客服 张三');
assert.equal(translate('下载 历史.txt'),'下载 历史.txt');
`
	if output, err := exec.Command(node, "-e", source).CombinedOutput(); err != nil {
		t.Fatalf("翻译参数回归失败：%v %s", err, output)
	}
}
