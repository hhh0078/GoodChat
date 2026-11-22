// 文件作用说明：在悬浮窗渲染时压缩重复品牌与状态区域，不修改已保存的模板或固定脚本。
package decoration

import (
	"bytes"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// compactWidgetDocument 仅对明确嵌入模式标记聊天祖先，保留聊天节点、弹窗与原始业务脚本。
func compactWidgetDocument(r *http.Request, raw []byte) []byte {
	if r.URL.Query().Get("embed") != "1" {
		return raw
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	var messages, input, head, body *html.Node
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if node.Data == "head" {
				head = node
			}
			if node.Data == "body" {
				body = node
			}
			for _, attr := range node.Attr {
				if attr.Key == "id" {
					if attr.Val == "gc-messages" {
						messages = node
					}
					if attr.Val == "gc-text" {
						input = node
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	if messages == nil || input == nil || head == nil || body == nil {
		return raw
	}
	chat := messages.Parent
	for chat != nil {
		contains := false
		for node := input; node != nil; node = node.Parent {
			if node == chat {
				contains = true
				break
			}
		}
		if contains {
			break
		}
		chat = chat.Parent
	}
	if chat == nil {
		return raw
	}
	addPresentationClass(chat, "gc-embed-chat")
	for node := chat.Parent; node != nil && node.Data != "html"; node = node.Parent {
		if node.Type == html.ElementNode {
			addPresentationClass(node, "gc-embed-path")
		}
	}
	addPresentationClass(body, "gc-embedded-document")
	style := &html.Node{Type: html.ElementNode, Data: "style"}
	style.AppendChild(&html.Node{Type: html.TextNode, Data: compactWidgetCSS})
	head.AppendChild(style)
	var output bytes.Buffer
	if html.Render(&output, doc) != nil {
		return raw
	}
	return output.Bytes()
}

// addPresentationClass 追加受控展示类而不覆盖租户自己的选择器。
func addPresentationClass(node *html.Node, name string) {
	for i, attr := range node.Attr {
		if attr.Key == "class" {
			node.Attr[i].Val = strings.TrimSpace(attr.Val + " " + name)
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: "class", Val: name})
}

const compactWidgetCSS = `
/* 悬浮模式只保留聊天主区；外层可信蓝栏提供品牌、连接与留言。 */
html,body.gc-embedded-document{height:100%;min-height:0;margin:0;padding:0}
.gc-embed-path{display:flex!important;flex-direction:column!important;position:relative!important;inset:auto!important;width:100%!important;max-width:none!important;height:100%!important;min-height:0!important;max-height:none!important;padding:0!important;margin:0!important;border:0!important;border-radius:0!important;overflow:hidden!important;grid-template:none!important}
.gc-embed-path > :not(.gc-embed-path):not(.gc-embed-chat):not(dialog):not(script):not(style){display:none!important}
.gc-embed-chat{display:flex!important;flex-direction:column!important;position:relative!important;flex:1 1 0!important;min-height:0!important;height:100%!important;max-height:none!important;width:100%!important;margin:0!important;padding:0!important;border:0!important;border-radius:0!important;overflow:hidden!important}
.gc-embedded-document .gc-chat-heading,.gc-embedded-document .gc-brandbar,.gc-embedded-document #gc-status,.gc-embed-chat>h2,.gc-embed-chat>p:not(#gc-typing):not(#gc-error){display:none!important}
.gc-embedded-document #gc-messages{flex:1 1 0!important;min-height:0!important;max-height:none!important;overflow:auto!important;padding:18px 14px!important}
.gc-embedded-document .gc-message{font-size:14px;line-height:1.6}.gc-embedded-document .gc-avatar{width:30px;height:30px}.gc-embedded-document .gc-message-row{padding-left:40px;margin-bottom:18px}.gc-embedded-document .gc-own{padding-left:0;padding-right:40px}.gc-embedded-document .gc-system{padding:0}
.gc-embedded-document .gc-composer-area{flex:none;padding:10px 12px!important}.gc-embedded-document #gc-compose{flex:none}.gc-embedded-document .gc-capsule{align-items:center}.gc-embedded-document #gc-text{font-size:14px;line-height:22px}.gc-embedded-document #gc-add img,.gc-embedded-document .gc-emoji-button img{width:20px;height:20px}
.gc-embedded-document #gc-file[hidden],.gc-embedded-document [hidden]{display:none!important}
`
