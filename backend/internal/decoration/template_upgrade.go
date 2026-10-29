// 文件作用说明：把旧结构化和内联模板导出为带固定SDK的可重导入静态包，旧业务脚本不会迁入新模板。
package decoration

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// exportTemplatePackage 将非资源包模板转为固定动作静态包，保留原有内容与可验证样式。
func exportTemplatePackage(mode string, design json.RawMessage, scripts TemplateScripts) (*staticPackage, error) {
	checked, err := validateDesign(mode, design)
	if err != nil {
		return nil, err
	}
	var code CodeDesign
	if mode == "schema" {
		var schema Schema
		if err := json.Unmarshal(checked, &schema); err != nil {
			return nil, err
		}
		code = schemaCode(schema, false)
	} else if err := json.Unmarshal(checked, &code); err != nil {
		return nil, err
	}
	if code.PackageID != "" {
		return nil, invalidDesign("已有资源包请使用其完整文件导出")
	}
	markup := `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><title>在线咨询</title></head><body>` + code.HTML + `</body></html>`
	markup, err = prepareLegacyTemplateHTML([]byte(markup))
	if err != nil {
		return nil, err
	}
	css := "/* 文件作用说明：保留旧模板外观并补齐固定SDK聊天状态样式。 */\n:root{--gc-primary:#386249;--gc-background:#f2f4ee}\n" + industryBaseCSS + "\n" + code.CSS
	return buildTemplatePackage("index.html", []TemplateFile{{Path: "index.html", Content: markup}, {Path: "static/upgrade.css", Content: css}}, scripts)
}

// upgradeLegacyTemplatePackage 保留旧资源的相对目录与静态内容，移除旧脚本后冻结新版平台SDK。
func upgradeLegacyTemplatePackage(pkg *staticPackage, scripts TemplateScripts) (*staticPackage, error) {
	if pkg == nil {
		return nil, invalidDesign("旧资源包不存在")
	}
	lookup := map[string]packageFile{}
	for _, file := range pkg.Files {
		lookup[file.Path] = file
	}
	if templateHasContract(lookup) {
		return pkg, nil
	}
	entry, ok := lookup[pkg.Entrypoint]
	if !ok {
		return nil, invalidDesign("旧资源包入口不存在")
	}
	// 先用原有解析器将资源定位到静态保留目录，再移除原脚本和跨页面导航。
	raw, err := packageHTML(entry.Content, pkg.Entrypoint, "/static/legacy/", "", "", lookup)
	if err != nil {
		return nil, err
	}
	markup, err := prepareLegacyTemplateHTML(raw)
	if err != nil {
		return nil, err
	}
	files := []TemplateFile{{Path: "index.html", Content: markup}, {Path: "static/upgrade.css", Content: "/* 文件作用说明：为旧静态包补齐固定聊天UI样式。 */\n:root{--gc-primary:#386249;--gc-background:#f2f4ee}\n" + industryBaseCSS}}
	assets := []packageFile{}
	for _, file := range pkg.Files {
		if strings.HasPrefix(file.ContentType, "text/javascript") || strings.HasPrefix(file.ContentType, "text/html") || file.Path == "template.json" {
			continue
		}
		name := "static/legacy/" + file.Path
		if strings.HasPrefix(file.ContentType, "text/css") {
			css, err := packageCSS(string(file.Content), file.Path, "/static/legacy/", lookup)
			if err != nil {
				return nil, err
			}
			files = append(files, TemplateFile{Path: name, Content: css})
		} else if strings.HasPrefix(file.ContentType, "text/") || file.ContentType == "application/json" {
			files = append(files, TemplateFile{Path: name, Content: string(file.Content)})
		} else {
			file.Path = name
			assets = append(assets, file)
		}
	}
	return buildTemplatePackageAssets("index.html", files, scripts, assets)
}

// prepareLegacyTemplateHTML 保留静态内容并确保聊天控件完整，旧脚本与非入口导航不会继续执行。
func prepareLegacyTemplateHTML(raw []byte) (string, error) {
	doc, err := xhtml.Parse(bytes.NewReader(raw))
	if err != nil {
		return "", invalidDesign("旧模板HTML无法解析")
	}
	var head, body *xhtml.Node
	ids := map[string]bool{}
	var scan func(*xhtml.Node)
	scan = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			if node.Data == "head" {
				head = node
			}
			if node.Data == "body" {
				body = node
			}
			for _, attr := range node.Attr {
				if attr.Key == "id" {
					ids[attr.Val] = true
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			scan(child)
		}
	}
	scan(doc)
	complete := true
	for id := range templateRequiredNodes {
		complete = complete && ids[id]
	}
	var clean func(*xhtml.Node)
	clean = func(node *xhtml.Node) {
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			remove := child.Type == xhtml.ElementNode && child.Data == "script"
			if child.Type == xhtml.ElementNode && child.Data == "link" {
				for _, attr := range child.Attr {
					if attr.Key == "rel" && (attr.Val == "modulepreload" || attr.Val == "preload") {
						remove = true
					}
				}
			}
			if remove {
				node.RemoveChild(child)
			} else {
				if child.Type == xhtml.ElementNode {
					attrs := child.Attr[:0]
					for _, attr := range child.Attr {
						if strings.HasPrefix(strings.ToLower(attr.Key), "on") {
							continue
						}
						if attr.Key == "id" && strings.HasPrefix(attr.Val, "gc-") && !complete {
							attr.Val = "legacy-" + attr.Val
						}
						if child.Data == "a" && attr.Key == "href" && !strings.HasPrefix(attr.Val, "#") {
							attr.Val = "#"
						}
						attrs = append(attrs, attr)
					}
					child.Attr = attrs
					if child.Data == "button" {
						normalizeLegacyButton(child, complete)
					}
					if complete {
						for _, attr := range attrs {
							if attr.Key == "id" && attr.Val == "gc-messages" {
								for child.FirstChild != nil {
									child.RemoveChild(child.FirstChild)
								}
							}
						}
					}
				}
				clean(child)
			}
			child = next
		}
	}
	clean(doc)
	if head == nil || body == nil {
		return "", invalidDesign("旧模板缺少完整HTML文档结构")
	}
	link := &xhtml.Node{Type: xhtml.ElementNode, Data: "link", Attr: []xhtml.Attribute{{Key: "rel", Val: "stylesheet"}, {Key: "href", Val: "static/upgrade.css"}}}
	head.InsertBefore(link, head.FirstChild)
	if !complete {
		// 旧模板仅补固定聊天区，没有行业说明弹框，不能加入一个找不到目标的说明按钮。
		markup := strings.Replace(templateChatMarkup(), `<button type="button" data-dialog-open="gc-service-notice" class="gc-service-button">服务说明</button>`, "", 1)
		fragment, err := xhtml.Parse(strings.NewReader(markup))
		if err != nil {
			return "", err
		}
		var widget *xhtml.Node
		var find func(*xhtml.Node)
		find = func(node *xhtml.Node) {
			if node.Type == xhtml.ElementNode && node.Data == "section" && widget == nil {
				widget = node
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				find(child)
			}
		}
		find(fragment)
		if widget == nil {
			return "", invalidDesign("固定聊天控件无法生成")
		}
		widget.Parent.RemoveChild(widget)
		body.AppendChild(widget)
	}
	for _, name := range []string{templateSDKPath, templateUIPath} {
		body.AppendChild(&xhtml.Node{Type: xhtml.ElementNode, Data: "script", Attr: []xhtml.Attribute{{Key: "src", Val: path.Clean(name)}}})
	}
	var out bytes.Buffer
	if err := xhtml.Render(&out, doc); err != nil {
		return "", err
	}
	return out.String(), nil
}

// normalizeLegacyButton 仅保留固定SDK可执行的旧按钮，失去旧脚本的控件转为保留文案样式的静态节点。
func normalizeLegacyButton(node *xhtml.Node, complete bool) {
	attrs := templateNodeAttributes(node)
	actions := 0
	for _, name := range []string{"data-gc-send", "data-dialog-open", "data-dialog-close", "data-gc-ui"} {
		if _, exists := attrs[name]; exists {
			actions++
		}
	}
	id := attrs["id"]
	core := complete && (id == "gc-send" || id == "gc-add" || id == "gc-reply-cancel") && actions == 0 && attrs["data-kind"] == ""
	kind, hasKind := attrs["data-kind"]
	attachment := complete && actions == 0 && hasKind && (kind == "image" || kind == "file" || kind == "video") && templateButtonWithin(node, "gc-attachment-menu")
	text, hasSend := attrs["data-gc-send"]
	send := actions == 1 && hasSend && !hasKind && strings.TrimSpace(text) != "" && len(text) <= 10000 && !strings.ContainsRune(text, 0)
	if core || attachment || send {
		for i := range node.Attr {
			if node.Attr[i].Key == "type" {
				node.Attr[i].Val = "button"
				return
			}
		}
		node.Attr = append(node.Attr, xhtml.Attribute{Key: "type", Val: "button"})
		return
	}
	// 升级不会迁移旧custom.js，因此旧UI声明也没有可验证的实现；不能据按钮文案猜测发送动作。
	node.Data, node.DataAtom = "span", atom.Span
	clean := node.Attr[:0]
	for _, attr := range node.Attr {
		switch attr.Key {
		case "type", "disabled", "name", "value", "form", "formaction", "formmethod", "formenctype", "formtarget", "formnovalidate", "tabindex", "role", "aria-expanded", "aria-controls", "aria-haspopup", "aria-pressed", "data-kind", "data-gc-send", "data-dialog-open", "data-dialog-close", "data-gc-ui":
			continue
		}
		clean = append(clean, attr)
	}
	node.Attr = clean
}
