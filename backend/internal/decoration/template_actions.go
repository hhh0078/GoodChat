// 文件作用说明：仅对新构建和新导入模板检查按钮动作，防止可见业务入口未绑定固定SDK。
package decoration

import (
	"bytes"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
)

var templateDialogIDPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,79}$`)
var templateUIActionPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var templateInlineCSSComments = regexp.MustCompile(`(?s)/\*.*?\*/`)
var templatePermanentHiddenStyle = regexp.MustCompile(`(?i)(?:^|;)\s*(?:display\s*:\s*none|visibility\s*:\s*hidden)\s*(?:!\s*important\s*)?(?:;|$)`)

// templateNodeAttributes 读取解析器规范化的HTML属性，仅供静态按钮动作检查。
func templateNodeAttributes(node *xhtml.Node) map[string]string {
	attrs := map[string]string{}
	for _, attr := range node.Attr {
		attrs[attr.Key] = attr.Val
	}
	return attrs
}

// templateIsDialog 判断本地节点是否具有原生或无障碍对话框语义。
func templateIsDialog(node *xhtml.Node) bool {
	return node != nil && node.Type == xhtml.ElementNode && (node.Data == "dialog" || templateNodeAttributes(node)["role"] == "dialog")
}

// templateButtonWithin 确认按钮位于指定固定控件内部，不认可无关位置的同名动作属性。
func templateButtonWithin(node *xhtml.Node, id string) bool {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if templateNodeAttributes(parent)["id"] == id {
			return true
		}
	}
	return false
}

// validateNewTemplateVisibility 拒绝固定SDK无法切换的永久隐藏状态，初始隐藏仅由原生hidden属性控制。
func validateNewTemplateVisibility(ids map[string]*xhtml.Node) error {
	for _, id := range []string{"gc-reply-bar", "gc-attachment-menu", "gc-messages", "gc-text", "gc-send", "gc-compose"} {
		node := ids[id]
		if node == nil {
			continue
		}
		attrs := templateNodeAttributes(node)
		if id != "gc-reply-bar" && id != "gc-attachment-menu" {
			if _, hidden := attrs["hidden"]; hidden {
				return invalidDesign("固定消息和输入节点不能初始隐藏：" + id)
			}
		}
		for _, name := range strings.Fields(attrs["class"]) {
			if strings.EqualFold(name, "hidden") || strings.EqualFold(name, "is-hidden") {
				return invalidDesign("固定聊天节点不能使用hidden/is-hidden类永久隐藏：" + id + "；初始隐藏请仅用hidden属性，由固定SDK切换")
			}
		}
		style, err := cssWord(attrs["style"])
		if err != nil {
			return err
		}
		style = templateInlineCSSComments.ReplaceAllString(style, "")
		if templatePermanentHiddenStyle.MatchString(style) {
			return invalidDesign("固定聊天节点不能以内联display:none或visibility:hidden永久隐藏：" + id + "；初始隐藏请仅用hidden属性，由固定SDK切换")
		}
	}
	return nil
}

// validateNewTemplateActions 要求新按钮使用平台动作或本地UI声明，不从data-period或外观推断业务行为。
func validateNewTemplateActions(raw []byte, files map[string]packageFile) error {
	doc, err := xhtml.Parse(bytes.NewReader(raw))
	if err != nil {
		return invalidDesign("模板HTML无法解析")
	}
	ids := map[string]*xhtml.Node{}
	buttons := []*xhtml.Node{}
	customScriptLoaded := false
	var collect func(*xhtml.Node)
	collect = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			attrs := templateNodeAttributes(node)
			if attrs["id"] != "" {
				ids[attrs["id"]] = node
			}
			if node.Data == "button" {
				buttons = append(buttons, node)
			}
			if node.Data == "script" && strings.TrimPrefix(attrs["src"], "./") == "static/custom.js" {
				customScriptLoaded = true
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(doc)
	if err := validateNewTemplateVisibility(ids); err != nil {
		return err
	}
	for _, button := range buttons {
		attrs := templateNodeAttributes(button)
		if attrs["type"] != "button" {
			return invalidDesign("所有模板按钮必须显式设置type=button")
		}
		actions := 0
		for _, name := range []string{"data-gc-send", "data-gc-action", "data-dialog-open", "data-dialog-close", "data-gc-ui"} {
			if _, present := attrs[name]; present {
				actions++
			}
		}
		id := attrs["id"]
		if id == "gc-send" || id == "gc-add" || id == "gc-reply-cancel" {
			if actions != 0 || attrs["data-kind"] != "" {
				return invalidDesign("固定聊天控件不能重复声明其他动作：" + id)
			}
			continue
		}
		if kind, present := attrs["data-kind"]; present {
			if actions != 0 || !templateButtonWithin(button, "gc-attachment-menu") || kind != "image" && kind != "file" && kind != "video" {
				return invalidDesign("附件按钮仅允许在固定附件菜单内声明image、file或video")
			}
			continue
		}
		if actions != 1 {
			return invalidDesign("模板按钮必须且只能声明一个动作：业务咨询使用data-gc-send，留言使用data-gc-action=leave-message，本地界面使用data-dialog-open/close或data-gc-ui")
		}
		if action, present := attrs["data-gc-action"]; present {
			if action != "leave-message" && action != "evaluation" {
				return invalidDesign("data-gc-action仅允许固定留言leave-message或评价evaluation动作")
			}
			method := "openLeaveMessage"
			if action == "evaluation" {
				method = "openEvaluation"
			}
			if !strings.Contains(string(files[templateSDKPath].Content), method) || !strings.Contains(string(files[templateUIPath].Content), `data-gc-action`) {
				return invalidDesign("该固定动作需要支持留言和评价的新版平台核心脚本，请先升级SDK")
			}
			continue
		}
		if text, present := attrs["data-gc-send"]; present {
			if strings.TrimSpace(text) == "" || len(text) > 10000 || strings.ContainsRune(text, 0) {
				return invalidDesign("data-gc-send必须包含1至10000字节的有效咨询文本")
			}
			continue
		}
		custom, exists := files["static/custom.js"]
		if !customScriptLoaded || !exists || strings.TrimSpace(string(custom.Content)) == "" {
			return invalidDesign("本地弹框和界面按钮必须加载static/custom.js实现对应交互")
		}
		if target, present := attrs["data-dialog-open"]; present {
			if !templateDialogIDPattern.MatchString(target) || !templateIsDialog(ids[target]) {
				return invalidDesign("data-dialog-open必须指向本页存在的dialog或role=dialog节点ID")
			}
			continue
		}
		if target, present := attrs["data-dialog-close"]; present {
			if target != "" {
				if !templateDialogIDPattern.MatchString(target) || !templateIsDialog(ids[target]) {
					return invalidDesign("data-dialog-close指定的对话框ID不存在或无效")
				}
			} else {
				withinDialog := false
				for parent := button.Parent; parent != nil; parent = parent.Parent {
					withinDialog = withinDialog || templateIsDialog(parent)
				}
				if !withinDialog {
					return invalidDesign("未指定目标的data-dialog-close按钮必须位于本地对话框内部")
				}
			}
			continue
		}
		if !templateUIActionPattern.MatchString(attrs["data-gc-ui"]) {
			return invalidDesign("data-gc-ui必须为1至64位小写字母开头的本地动作名，只能包含字母、数字、连字符或下划线")
		}
	}
	return nil
}
