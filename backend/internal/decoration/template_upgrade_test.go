// 文件作用说明：验证旧模板升级保留静态文案与合法平台入口，旧业务按钮和失去脚本的界面按钮不会冒充可执行动作。
package decoration

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// legacyAcceptanceControls 复现实际多文件验收包的不完整旧聊天节点，包含未绑定的取消与发送按钮。
const legacyAcceptanceControls = `<section class="gc-block gc-chat"><h2>旧在线咨询区</h2><div id="gc-status">旧连接状态</div><div id="gc-messages"></div><div id="gc-reply-bar" hidden><span id="gc-reply-text"></span><button id="gc-reply-cancel" type="button">取消引用</button></div><div id="gc-compose"><textarea id="gc-text" aria-label="旧消息内容"></textarea><input id="gc-file" type="file" aria-label="添加附件"><button id="gc-send" type="button">发送</button></div><div id="gc-typing"></div><p id="gc-error"></p></section>`

// TestLegacyUpgradeNormalizesUnboundButtons 验证升级共享入口不会推断旧按钮动作，仍严格检查新版静态包。
func TestLegacyUpgradeNormalizesUnboundButtons(t *testing.T) {
	body := `<h1>保留旧静态内容</h1><button id="old-period" class="old-option" data-period="6" onclick="oldBusiness()">分期6个月</button><button id="known-send" data-gc-send="咨询12个月">12个月</button><button id="old-dialog" data-dialog-open="old-notice">旧说明</button><dialog id="old-notice"><button id="old-close" type="button" data-dialog-close>关闭</button></dialog><button id="old-ui" type="button" data-gc-ui="show_details">旧界面</button>` + legacyAcceptanceControls
	code, _ := json.Marshal(CodeDesign{HTML: body, CSS: `.old-option{color:#765432}`, Javascript: `window.GoodChat;function oldBusiness(){window.oldBusinessExecuted=true}`})
	inline, err := exportTemplatePackage("code", code, defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	files := packageFixtureFiles(t)
	// 旧多文件包原本就禁止内联事件；它的旧业务绑定来自单独脚本。
	packageBody := strings.Replace(body, ` onclick="oldBusiness()"`, "", 1)
	files["site/index.html"] = []byte(`<!doctype html><html><head><link rel="stylesheet" href="./css/main.css"></head><body>` + packageBody + `<img src="./logo.png" alt="Logo"><script type="module" src="./js/main.mjs"></script></body></html>`)
	original := append([]byte(nil), files["site/index.html"]...)
	legacy, err := readStaticZIP(zipFixture(t, files, 0), "site/index.html")
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := upgradeLegacyTemplatePackage(legacy, defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(templateFileMap(legacy)[legacy.Entrypoint].Content, original) {
		t.Fatal("兼容升级不能改变旧资源包原字节")
	}
	for name, pkg := range map[string]*staticPackage{"inline": inline, "actual partial multi-file controls": upgraded} {
		t.Run(name, func(t *testing.T) {
			if err := verifyNewTemplatePackage(context.Background(), nil, pkg); err != nil {
				t.Fatal(err)
			}
			markup := templateFileMap(pkg)[pkg.Entrypoint].Content
			if !bytes.Contains(markup, []byte("保留旧静态内容")) || !bytes.Contains(markup, []byte("分期6个月")) || bytes.Contains(markup, []byte("onclick")) || bytes.Contains(markup, []byte("oldBusinessExecuted")) {
				t.Fatal("升级应保留静态文案并移除旧业务事件")
			}
			doc, err := xhtml.Parse(bytes.NewReader(markup))
			if err != nil {
				t.Fatal(err)
			}
			nodes := map[string]*xhtml.Node{}
			var collect func(*xhtml.Node)
			collect = func(node *xhtml.Node) {
				if id := templateNodeAttributes(node)["id"]; id != "" {
					nodes[id] = node
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					collect(child)
				}
			}
			collect(doc)
			for _, id := range []string{"old-period", "old-dialog", "old-close", "old-ui"} {
				node := nodes[id]
				if node == nil || node.Data != "span" {
					t.Fatalf("没有可验证实现的旧控件应变成静态展示：%s", id)
				}
				for attr := range templateNodeAttributes(node) {
					if attr == "data-gc-send" || attr == "data-dialog-open" || attr == "data-dialog-close" || attr == "data-gc-ui" || strings.HasPrefix(attr, "on") {
						t.Fatal("静态展示节点不能保留旧动作")
					}
				}
			}
			known := nodes["known-send"]
			if known == nil || known.Data != "button" || templateNodeAttributes(known)["data-gc-send"] != "咨询12个月" || templateNodeAttributes(known)["type"] != "button" {
				t.Fatal("已明确绑定的合法固定SDK咨询入口应保留")
			}
			for _, id := range []string{"gc-send", "gc-add", "gc-reply-cancel"} {
				if node := nodes[id]; node == nil || node.Data != "button" {
					t.Fatal("升级后必须补齐真正可用的固定聊天按钮")
				}
			}
			if bytes.Contains(markup, []byte("旧连接状态")) || bytes.Contains(markup, []byte("旧在线咨询区")) || bytes.Contains(markup, []byte("legacy-gc-")) {
				t.Fatal("不完整的原聊天骨架不能与新固定面板同时显示")
			}
			if bytes.Count(markup, []byte("<textarea")) != 1 || bytes.Count(markup, []byte(`type="file"`)) != 1 {
				t.Fatal("实际旧包升级后只能有一套输入和附件控件")
			}
		})
	}
}

// TestLegacyUpgradeUnknownContainerDisablesStaleForms 验证无明确旧容器时仅移除固定旧节点，其余旧表单展示且保留品牌与FAQ。
func TestLegacyUpgradeUnknownContainerDisablesStaleForms(t *testing.T) {
	markup := `<!doctype html><html><head></head><body><header><h1>品牌内容保留</h1></header><section class="faq"><h2>常见问题保留</h2></section><div class="custom-card"><h2>自定义内容保留</h2><p id="gc-status">旧连接状态</p><textarea id="gc-text"></textarea><input id="gc-file" type="file"><button id="gc-send">旧发送</button><label>原资料<input id="old-name"></label><textarea id="old-comment"></textarea><select id="old-choice"><option>原选项</option></select></div></body></html>`
	prepared, err := prepareLegacyTemplateHTML([]byte(markup))
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"品牌内容保留", "常见问题保留", "自定义内容保留", "原资料"} {
		if !strings.Contains(prepared, content) {
			t.Fatal("清理失效聊天时不能删除非聊天静态内容")
		}
	}
	if strings.Contains(prepared, "旧连接状态") || strings.Contains(prepared, "旧发送") {
		t.Fatal("无明确容器时也必须移除失效的固定旧节点")
	}
	doc, _ := xhtml.Parse(strings.NewReader(prepared))
	activeText, activeFile, disabledForms := 0, 0, 0
	var inspect func(*xhtml.Node)
	inspect = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			attrs := templateNodeAttributes(node)
			_, disabled := attrs["disabled"]
			if strings.HasPrefix(attrs["id"], "old-") && disabled {
				disabledForms++
			}
			if node.Data == "textarea" && !disabled {
				activeText++
			}
			if node.Data == "input" && attrs["type"] == "file" && !disabled {
				activeFile++
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			inspect(child)
		}
	}
	inspect(doc)
	if activeText != 1 || activeFile != 1 || disabledForms != 3 {
		t.Fatal("只能保留一套可用聊天输入和附件，其它旧表单必须禁用展示")
	}
}

// TestLegacyUpgradePreservesCompleteChat 验证完整聊天骨架不被重复替换，已受保护资源包保持全部原字节。
func TestLegacyUpgradePreservesCompleteChat(t *testing.T) {
	chat := strings.Replace(templateChatMarkup(), `<button type="button" data-dialog-open="gc-service-notice" class="gc-service-button">服务说明</button>`, "", 1)
	markup := `<!doctype html><html><head></head><body><h1>原完整模板品牌</h1>` + chat + `<input id="existing-extra" value="原独立控件"></body></html>`
	prepared, err := prepareLegacyTemplateHTML([]byte(markup))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(prepared, `id="gc-text"`) != 1 || strings.Count(prepared, `id="gc-file"`) != 1 || strings.Contains(prepared, "legacy-gc-") || !strings.Contains(prepared, `<input id="existing-extra" value="原独立控件"/>`) {
		t.Fatal("完整聊天不能被删掉再追加，也不应禁用原独立控件")
	}
	protected, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := upgradeLegacyTemplatePackage(protected, defaultTemplateScripts())
	if err != nil || upgraded != protected || upgraded.SHA256 != protected.SHA256 {
		t.Fatal("已受保护的固定SDK包必须原样返回")
	}
}
