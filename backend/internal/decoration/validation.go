// 文件作用说明：统一校验人工和AI装修设计，阻止结构化页面执行任意脚本及外部代码依赖。
package decoration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"

	"supportdesk/backend/internal/platform"
)

// Theme 定义结构化装修的自然色与圆角变量。
type Theme struct {
	PrimaryColor string `json:"primary_color"`
	Background   string `json:"background"`
	TextColor    string `json:"text_color"`
	Radius       int    `json:"radius"`
}

// Brand 定义访客页品牌内容。
type Brand struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	LogoURL  string `json:"logo_url"`
}

// Block 定义受控的可拖拽页面区块。
type Block struct {
	ID      string              `json:"id"`
	Type    string              `json:"type"`
	Title   string              `json:"title"`
	Content string              `json:"content"`
	Items   []map[string]string `json:"items"`
}

// Schema 定义与前端共享的首版可视化设计格式。
type Schema struct {
	SchemaVersion int     `json:"schema_version"`
	Theme         Theme   `json:"theme"`
	Brand         Brand   `json:"brand"`
	Blocks        []Block `json:"blocks"`
	CustomCSS     string  `json:"custom_css"`
}

// CodeDesign 保存浏览器静态代码，服务器从不执行其中代码。
type CodeDesign struct {
	HTML       string `json:"html"`
	CSS        string `json:"css"`
	Javascript string `json:"javascript"`
	PackageID  string `json:"package_id,omitempty"`
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var unsafeCSS = regexp.MustCompile(`(?i)@import|url\s*\(|expression\s*\(|</style|javascript:`)
var externalMarkup = regexp.MustCompile(`(?i)<\s*(iframe|frame|object|embed|base|form|meta)\b|<\s*(script|link)\b[^>]*(src|href)\s*=|(?:src|href)\s*=\s*["']\s*(?:https?:|//|javascript:|data:text/html)`)

// strictJSON 拒绝未知结构字段和附带第二个JSON对象的输入。
func strictJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidDesign("设计结构与当前版本不匹配")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return invalidDesign("设计必须是单个JSON对象")
	}
	return nil
}

// invalidDesign 创建可安全展示的装修校验错误。
func invalidDesign(message string) error { return platform.Fail(400, "invalid_design", message) }

// validateDesign 规范化两类设计并校验体积、必需聊天区块与静态代码能力。
func validateDesign(mode string, body json.RawMessage) (json.RawMessage, error) {
	if len(body) == 0 || len(body) > 512*1024 {
		return nil, invalidDesign("装修设计不能为空且总大小不能超过512KiB")
	}
	if mode == "schema" {
		var schema Schema
		if err := strictJSON(body, &schema); err != nil {
			return nil, err
		}
		if schema.SchemaVersion != 1 || !colorPattern.MatchString(schema.Theme.PrimaryColor) || !colorPattern.MatchString(schema.Theme.Background) || !colorPattern.MatchString(schema.Theme.TextColor) || schema.Theme.Radius < 0 || schema.Theme.Radius > 48 {
			return nil, invalidDesign("主题颜色、圆角或设计版本不正确")
		}
		if len(schema.Brand.Title) > 300 || len(schema.Brand.Subtitle) > 2000 || len(schema.Blocks) == 0 || len(schema.Blocks) > 80 || len(schema.CustomCSS) > 32768 {
			return nil, invalidDesign("品牌文案、区块数量或自定义样式超出限制")
		}
		if schema.Brand.LogoURL != "" {
			if strings.HasPrefix(schema.Brand.LogoURL, "data:") {
				if err := validateLogoData(schema.Brand.LogoURL); err != nil {
					return nil, err
				}
			} else {
				u, err := url.Parse(schema.Brand.LogoURL)
				if err != nil || u.User != nil || u.Scheme != "" && u.Scheme != "https" || u.Scheme == "" && !strings.HasPrefix(schema.Brand.LogoURL, "/") || strings.HasPrefix(schema.Brand.LogoURL, "//") {
					return nil, invalidDesign("Logo请使用受控相对路径或HTTPS图片")
				}
			}
		}
		if unsafeCSS.MatchString(schema.CustomCSS) {
			return nil, invalidDesign("自定义样式不允许外部资源、脚本或样式标签")
		}
		ids := map[string]bool{}
		chat := 0
		for _, b := range schema.Blocks {
			if b.ID == "" || len(b.ID) > 80 || ids[b.ID] || len(b.Title) > 600 || len(b.Content) > 16000 || len(b.Items) > 100 {
				return nil, invalidDesign("区块编号、文案或条目数量不正确")
			}
			ids[b.ID] = true
			switch b.Type {
			case "chat":
				chat++
			case "welcome", "announcement", "faq", "contact":
			default:
				return nil, invalidDesign("包含未支持的页面区块")
			}
			for _, item := range b.Items {
				if len(item) > 8 {
					return nil, invalidDesign("区块条目字段过多")
				}
				for key, value := range item {
					if len(key) > 50 || len(value) > 4000 {
						return nil, invalidDesign("区块条目过长")
					}
				}
			}
		}
		if chat != 1 {
			return nil, invalidDesign("页面必须保留且仅包含一个聊天区块")
		}
		return json.Marshal(schema)
	}
	if mode == "code" {
		var design CodeDesign
		if err := strictJSON(body, &design); err != nil {
			return nil, err
		}
		if (strings.TrimSpace(design.HTML) == "" && design.PackageID == "") || len(design.HTML) > 300000 || len(design.CSS) > 100000 || len(design.Javascript) > 150000 {
			return nil, invalidDesign("静态代码内容为空或超过大小限制")
		}
		if design.PackageID != "" {
			if idValue(design.PackageID) != nil {
				return nil, invalidDesign("代码资源包编号不正确")
			}
			if strings.Contains(strings.ToLower(design.CSS), "</style") || strings.Contains(strings.ToLower(design.Javascript), "</script") {
				return nil, invalidDesign("代码不能包含结束宿主样式或脚本标签")
			}
			return json.Marshal(design)
		}
		if externalMarkup.MatchString(design.HTML) || unsafeCSS.MatchString(design.CSS) || strings.Contains(strings.ToLower(design.HTML), "</body") || strings.Contains(strings.ToLower(design.Javascript), "</script") {
			return nil, invalidDesign("代码不允许外部依赖、表单、嵌套页面或结束宿主脚本标签")
		}
		if !strings.Contains(design.Javascript, "GoodChat") {
			return nil, invalidDesign("代码页面必须通过window.GoodChat接入当前授权聊天")
		}
		return json.Marshal(design)
	}
	return nil, invalidDesign("请选择schema或code装修模式")
}

// defaultDesign 返回未发布的真实编辑初稿，不作为AI生成结果。
func defaultDesign(mode, name string) json.RawMessage {
	if mode == "code" {
		var schema Schema
		_ = json.Unmarshal(defaultDesign("schema", name), &schema)
		body, _ := json.Marshal(schemaCode(schema, false))
		return body
	}
	body, _ := json.Marshal(Schema{SchemaVersion: 1, Theme: Theme{"#1f6f5b", "#f6f7f4", "#17231f", 16}, Brand: Brand{Title: name, Subtitle: "欢迎在线咨询"}, Blocks: []Block{{ID: "welcome", Type: "welcome", Title: "您好，有什么可以帮您？", Items: []map[string]string{}}, {ID: "chat", Type: "chat", Title: "在线咨询", Items: []map[string]string{}}}})
	return body
}
