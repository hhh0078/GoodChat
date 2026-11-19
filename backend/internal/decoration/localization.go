// 文件作用说明：为受控访客聊天脚本提供静态译文与个人时区，保留租户正文并维持原沙箱权限。
package decoration

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"golang.org/x/net/html"
	"net/http"
	"net/url"
	"strings"
	"supportdesk/backend/internal/localization"
)

// visitorLocalizationScript 仅为固定聊天节点提供译文和日期显示，不向沙箱增加网络能力。
//
//go:embed visitor_localization.js
var visitorLocalizationScript string

// validRenderPreferences 仅允许渲染入口继承的语言时区，资源凭证仍固定在路径中。
func validRenderPreferences(r *http.Request) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		if len(values) != 1 {
			return false
		}
		if key == "lang" && localization.Normalize(values[0]) != "" {
			continue
		}
		if key == "timezone" && localization.ValidTimezone(values[0]) {
			continue
		}
		return false
	}
	return true
}

// localizedRenderTarget 在静态入口重定向时保留有效的个人语言时区，不转发身份信息。
func localizedRenderTarget(r *http.Request, target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	query := parsed.Query()
	query.Set("lang", localization.RequestLocale(r))
	if zone := r.URL.Query().Get("timezone"); localization.ValidTimezone(zone) {
		query.Set("timezone", zone)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// visitorLocaleBoot 将平台脚本所需目录作为内联静态数据注入，不增加外部请求或 CSP 能力。
func visitorLocaleBoot(r *http.Request) string {
	locale := localization.RequestLocale(r)
	zone := r.URL.Query().Get("timezone")
	if !localization.ValidTimezone(zone) {
		zone = "UTC"
	}
	dictionary := localization.ScriptMessages(locale, sdkScript("", "", false)+schemaChatScript+templateActionScript+templatePreviewScript+templateChatMarkup())
	config, _ := json.Marshal(map[string]any{"locale": locale, "timezone": zone, "messages": dictionary, "patterns": localization.ScriptPatterns(locale, dictionary)})
	return `window.GoodChatI18n=` + string(config) + `;` + visitorLocalizationScript
}

// localizeChatControls 仅翻译固定聊天节点内的系统标签，不翻译品牌、问题、商品或客户聊天正文。
func localizeChatControls(raw []byte, locale string) []byte {
	document, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	ids := map[string]bool{"gc-status": true, "gc-typing": true, "gc-reply-bar": true, "gc-compose": true, "gc-error": true}
	var visit func(*html.Node, bool)
	visit = func(node *html.Node, owned bool) {
		for _, attribute := range node.Attr {
			if attribute.Key == "id" && ids[attribute.Val] {
				owned = true
			}
		}
		if owned {
			if node.Type == html.TextNode {
				node.Data = localization.Format(locale, node.Data)
			}
			for index, attribute := range node.Attr {
				if attribute.Key == "placeholder" || attribute.Key == "aria-label" || attribute.Key == "title" {
					node.Attr[index].Val = localization.Format(locale, attribute.Val)
				}
			}
		}
		if node.Type == html.ElementNode && node.Data == "html" {
			for index, attribute := range node.Attr {
				if attribute.Key == "lang" {
					node.Attr[index].Val = locale
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child, owned)
		}
	}
	visit(document, false)
	var output strings.Builder
	if !bytes.Contains(bytes.ToLower(raw), []byte("<html")) {
		var body *html.Node
		var find func(*html.Node)
		find = func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "body" {
				body = node
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				find(child)
			}
		}
		find(document)
		if body != nil {
			for child := body.FirstChild; child != nil; child = child.NextSibling {
				if html.Render(&output, child) != nil {
					return raw
				}
			}
			return []byte(output.String())
		}
	}
	if html.Render(&output, document) != nil {
		return raw
	}
	return []byte(output.String())
}
