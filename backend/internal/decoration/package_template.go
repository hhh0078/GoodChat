// 文件作用说明：将AI文件JSON转换为严格固定脚本静态包，并校验SDK摘要、控件和仅界面扩展脚本。
package decoration

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
)

var templateRequiredNodes = map[string]string{
	"gc-status": "", "gc-messages": "", "gc-text": "textarea", "gc-send": "button", "gc-file": "input", "gc-add": "button", "gc-attachment-menu": "", "gc-typing": "", "gc-error": "", "gc-reply-bar": "", "gc-reply-text": "", "gc-reply-cancel": "button",
}
var templateNetworkWords = regexp.MustCompile(`(?i)\b(fetch|XMLHttpRequest|WebSocket|EventSource|sendBeacon|importScripts|SharedWorker|serviceWorker|Worker)\b`)

// buildTemplatePackage 在没有额外二进制素材时转换一组完整AI文件。
func buildTemplatePackage(entry string, files []TemplateFile, scripts TemplateScripts) (*staticPackage, error) {
	return buildTemplatePackageAssets(entry, files, scripts, nil)
}

// buildTemplatePackageAssets 在全部素材就绪后一次转换和校验，不留下缺图或半成品资源包。
func buildTemplatePackageAssets(entry string, files []TemplateFile, scripts TemplateScripts, assets []packageFile) (*staticPackage, error) {
	if err := validateTemplateScripts(scripts); err != nil {
		return nil, err
	}
	if entry == "" {
		entry = "index.html"
	}
	if entry != "index.html" || len(files) == 0 || len(files)+len(assets) > packageFileCount-3 {
		return nil, invalidDesign("访客模板入口必须为index.html，最多包含197个生成文件和素材")
	}
	all := make([]packageFile, 0, len(files)+len(assets)+3)
	seen := map[string]bool{}
	for _, source := range files {
		if isProtectedTemplatePath(source.Path) {
			return nil, invalidDesign("AI或模板源码不能覆盖固定SDK、聊天脚本和版本合约")
		}
		if source.Path != entry && !strings.HasPrefix(source.Path, "static/") {
			return nil, invalidDesign("除index.html外，生成文件必须位于static静态目录")
		}
		file, err := checkStaticFile(source.Path, []byte(source.Content))
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(file.ContentType, "text/") && file.ContentType != "application/json" {
			return nil, invalidDesign("文件JSON只接收UTF-8文本，二进制图片必须使用受控素材")
		}
		if seen[file.Path] {
			return nil, invalidDesign("生成文件不能重复")
		}
		seen[file.Path] = true
		all = append(all, file)
	}
	for _, asset := range assets {
		if !strings.HasPrefix(asset.Path, "static/") || isProtectedTemplatePath(asset.Path) || seen[asset.Path] {
			return nil, invalidDesign("素材必须使用不重复的static目录路径，不能覆盖核心文件")
		}
		checked, err := checkStaticFile(asset.Path, asset.Content)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(checked.ContentType, "text/") || checked.ContentType == "application/json" {
			return nil, invalidDesign("二进制素材不能包含HTML、脚本或文本源码")
		}
		seen[asset.Path] = true
		all = append(all, checked)
	}
	contract := templateContract{SchemaVersion: 1, SDKVersion: scripts.Version, Files: map[string]string{templateSDKPath: digest([]byte(scripts.SDK)), templateUIPath: digest([]byte(scripts.ChatUI))}}
	for i := range all {
		if all[i].Path == entry {
			raw, err := freezeTemplateHTML(all[i].Content, contract)
			if err != nil {
				return nil, err
			}
			all[i], err = checkStaticFile(entry, raw)
			if err != nil {
				return nil, err
			}
		}
	}
	raw, _ := json.Marshal(contract)
	for name, content := range map[string][]byte{templateSDKPath: []byte(scripts.SDK), templateUIPath: []byte(scripts.ChatUI), templateContractPath: raw} {
		file, err := checkStaticFile(name, content)
		if err != nil {
			return nil, err
		}
		all = append(all, file)
	}
	pkg, err := finishPackage(entry, all)
	if err != nil {
		return nil, err
	}
	lookup := map[string]packageFile{}
	for _, file := range pkg.Files {
		lookup[file.Path] = file
	}
	if _, err := validateTemplatePackage(entry, lookup, true); err != nil {
		return nil, err
	}
	if err := validateNewTemplateActions(lookup[entry].Content, lookup); err != nil {
		return nil, err
	}
	return pkg, nil
}

// isProtectedTemplatePath 同时保护固定文件及其保留目录，避免大小写或旁路脚本冒充平台资源。
func isProtectedTemplatePath(name string) bool {
	name = strings.ToLower(name)
	return name == templateContractPath || strings.HasPrefix(name, "static/goodchat/")
}

// templateIntegrity 将十六进制SHA256转为浏览器SRI属性。
func templateIntegrity(hash string) string {
	raw, _ := hex.DecodeString(hash)
	return "sha256-" + base64.StdEncoding.EncodeToString(raw)
}

// freezeTemplateHTML 仅在构建时固化平台控件初始隐藏状态及脚本版本摘要，不修补缺失SDK或聊天节点。
func freezeTemplateHTML(raw []byte, contract templateContract) ([]byte, error) {
	doc, err := xhtml.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, invalidDesign("模板HTML无法解析")
	}
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			id, hidden := "", false
			for _, attr := range node.Attr {
				if attr.Key == "id" {
					id = attr.Val
				}
				if attr.Key == "hidden" {
					hidden = true
				}
			}
			if !hidden && (id == "gc-file" || id == "gc-reply-bar" || id == "gc-attachment-menu") {
				node.Attr = append(node.Attr, xhtml.Attribute{Key: "hidden", Val: ""})
			}
		}
		if node.Type == xhtml.ElementNode && node.Data == "script" {
			name := ""
			for _, attr := range node.Attr {
				if attr.Key == "src" {
					name = strings.TrimPrefix(attr.Val, "./")
				}
			}
			if hash, ok := contract.Files[name]; ok {
				attrs := node.Attr[:0]
				for _, attr := range node.Attr {
					if attr.Key != "integrity" && attr.Key != "data-goodchat-version" {
						attrs = append(attrs, attr)
					}
				}
				node.Attr = append(attrs, xhtml.Attribute{Key: "integrity", Val: templateIntegrity(hash)}, xhtml.Attribute{Key: "data-goodchat-version", Val: contract.SDKVersion})
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	var out bytes.Buffer
	if err := xhtml.Render(&out, doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// validateTemplatePackage 核验新版固定脚本的字节摘要、HTML必需节点及允许的扩展代码。
func validateTemplatePackage(entry string, files map[string]packageFile, required bool) (templateContract, error) {
	var contract templateContract
	file, exists := files[templateContractPath]
	if !exists {
		if required {
			return contract, invalidDesign("新访客模板必须包含平台固定SDK合约，请使用生成器或下载新版静态模板")
		}
		return contract, nil
	}
	if entry != "index.html" {
		return contract, invalidDesign("固定SDK模板入口必须为index.html")
	}
	contract, err := parseTemplateContract(file.Content)
	if err != nil {
		return contract, err
	}
	for name, expected := range contract.Files {
		core, ok := files[name]
		if !ok || digest(core.Content) != expected || core.SHA256 != expected {
			return contract, invalidDesign("固定SDK原始字节或摘要已被修改：" + name)
		}
	}
	for name, source := range files {
		if strings.HasPrefix(source.ContentType, "text/javascript") && name != templateSDKPath && name != templateUIPath {
			if name != "static/custom.js" || templateNetworkWords.Match(source.Content) {
				return contract, invalidDesign("额外脚本只能是static/custom.js中的界面交互，消息网络能力必须使用固定SDK")
			}
		}
		if strings.HasPrefix(source.ContentType, "text/html") && name != entry {
			return contract, invalidDesign("新访客模板只允许一个HTML入口，弹框请在当前页面用HTML/CSS/JS实现")
		}
	}
	if err := validateTemplateDOM(files[entry].Content, contract); err != nil {
		return contract, err
	}
	return contract, nil
}

// validateTemplateDOM 检查唯一控件、SDK加载顺序、版本及SRI，不把出现GoodChat字符串当作接入成功。
func validateTemplateDOM(raw []byte, contract templateContract) error {
	doc, err := xhtml.Parse(bytes.NewReader(raw))
	if err != nil {
		return invalidDesign("模板HTML无法解析")
	}
	ids := map[string]bool{}
	scripts := []string{}
	choices := map[string]bool{}
	position, lastControl, firstScript := 0, 0, 0
	var walk func(*xhtml.Node, int) error
	walk = func(node *xhtml.Node, depth int) error {
		position++
		if depth > 128 || position > 20000 {
			return invalidDesign("模板HTML结构过于复杂")
		}
		if node.Type == xhtml.ElementNode {
			attrs := map[string]string{}
			for _, attr := range node.Attr {
				attrs[attr.Key] = attr.Val
			}
			if id := attrs["id"]; id != "" {
				if ids[id] {
					return invalidDesign("模板节点ID不能重复：" + id)
				}
				ids[id] = true
				if tag, required := templateRequiredNodes[id]; required {
					lastControl = position
					if tag != "" && node.Data != tag {
						return invalidDesign("固定控件标签不正确：" + id)
					}
					if _, exists := attrs["disabled"]; exists {
						return invalidDesign("不能永久禁用固定聊天控件：" + id)
					}
					if _, exists := attrs["readonly"]; exists {
						return invalidDesign("不能把聊天输入设为只读")
					}
					if id == "gc-file" && attrs["type"] != "file" {
						return invalidDesign("gc-file必须是文件选择控件")
					}
					if id == "gc-messages" {
						for child := node.FirstChild; child != nil; child = child.NextSibling {
							if child.Type == xhtml.ElementNode || child.Type == xhtml.TextNode && strings.TrimSpace(child.Data) != "" {
								return invalidDesign("聊天消息区必须初始为空，由固定SDK注入预览或真实消息")
							}
						}
					}
					if id == "gc-file" || id == "gc-reply-bar" || id == "gc-attachment-menu" {
						if _, ok := attrs["hidden"]; !ok {
							return invalidDesign("文件、引用和附件菜单必须初始隐藏")
						}
					}
				}
			}
			if node.Data == "button" && attrs["data-kind"] != "" {
				for parent := node.Parent; parent != nil; parent = parent.Parent {
					for _, attr := range parent.Attr {
						if attr.Key == "id" && attr.Val == "gc-attachment-menu" {
							choices[attrs["data-kind"]] = true
						}
					}
				}
			}
			if node.Data == "script" {
				name := strings.TrimPrefix(attrs["src"], "./")
				if name == "" || path.Clean(name) != name || name != templateSDKPath && name != templateUIPath && name != "static/custom.js" {
					return invalidDesign("模板只允许固定sdk.js、chat-ui.js及可选static/custom.js，不能内联脚本")
				}
				for _, key := range []string{"async", "defer", "type"} {
					if _, ok := attrs[key]; ok {
						return invalidDesign("固定模板脚本必须按body末尾顺序直接加载")
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					if child.Type == xhtml.TextNode && strings.TrimSpace(child.Data) != "" {
						return invalidDesign("外部脚本标签不能混入内联代码")
					}
				}
				if hash, core := contract.Files[name]; core && (attrs["integrity"] != templateIntegrity(hash) || attrs["data-goodchat-version"] != contract.SDKVersion) {
					return invalidDesign("HTML固定脚本版本或SRI摘要不正确")
				}
				if firstScript == 0 {
					firstScript = position
				}
				scripts = append(scripts, name)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(doc, 0); err != nil {
		return err
	}
	for id := range templateRequiredNodes {
		if !ids[id] {
			return invalidDesign("模板缺少固定聊天节点：" + id)
		}
	}
	if len(scripts) < 2 || len(scripts) > 3 || scripts[0] != templateSDKPath || scripts[1] != templateUIPath || len(scripts) == 3 && scripts[2] != "static/custom.js" || firstScript <= lastControl {
		return invalidDesign("必须在聊天控件之后依次加载原版SDK、聊天UI和可选界面扩展脚本")
	}
	for _, kind := range []string{"image", "file", "video"} {
		if !choices[kind] {
			return invalidDesign("附件菜单必须保留图片、文件和视频三个固定入口")
		}
	}
	return nil
}
