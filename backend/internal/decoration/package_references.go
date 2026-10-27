// 文件作用说明：校验静态包的HTML、CSS与ES模块引用，并将资源定位到固定发布或短期预览路径。
package decoration

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
)

// packageReference 只解析本包相对文件，拒绝外部地址、多轮编码、API路径与不存在依赖。
func packageReference(value, current, prefix string, files map[string]packageFile, kind string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "#") {
		if kind == "any" || kind == "html" {
			return value, nil
		}
		return "", invalidDesign("脚本、样式和媒体引用必须指向实际包内文件")
	}
	if strings.HasPrefix(value, "data:") {
		parts := strings.SplitN(value, ",", 2)
		if len(parts) != 2 || len(value) > 350000 {
			return "", invalidDesign("内联图片或字体数据过大或格式不正确")
		}
		mime := strings.TrimSuffix(strings.TrimPrefix(parts[0], "data:"), ";base64")
		ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "font/woff": ".woff", "font/woff2": ".woff2", "font/ttf": ".ttf", "font/otf": ".otf"}[mime]
		if ext == "" || parts[0] != "data:"+mime+";base64" || kind != "any" && kind != "image" || kind == "image" && !strings.HasPrefix(mime, "image/") {
			return "", invalidDesign("内联资源仅支持真实图片或字体的base64数据，SVG与脚本不允许")
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(parts[1])
		if err != nil {
			return "", invalidDesign("内联资源base64编码错误")
		}
		if _, err := checkStaticFile("inline"+ext, raw); err != nil {
			return "", err
		}
		return value, nil
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\\' {
			return "", invalidDesign("资源引用包含控制字符或反斜线")
		}
	}
	u, err := url.Parse(value)
	if err != nil || value == "" || u.Scheme != "" || u.Host != "" || u.User != nil || strings.HasPrefix(value, "//") || strings.Contains(u.Path, "%") || u.RawQuery != "" {
		return "", invalidDesign("资源只能引用包内文件，不能使用外链、查询参数或多轮编码路径")
	}
	target := path.Clean(path.Join(path.Dir(current), u.Path))
	if strings.HasPrefix(u.Path, "/") {
		target = strings.TrimPrefix(path.Clean(u.Path), "/")
	}
	if err := packagePath(target); err != nil {
		return "", err
	}
	file, ok := files[target]
	if !ok {
		return "", invalidDesign("引用资源不存在：" + target)
	}
	valid := kind == "any" || kind == "script" && strings.HasPrefix(file.ContentType, "text/javascript") || kind == "style" && strings.HasPrefix(file.ContentType, "text/css") || kind == "html" && strings.HasPrefix(file.ContentType, "text/html") || kind == "image" && strings.HasPrefix(file.ContentType, "image/") || kind == "media" && (strings.HasPrefix(file.ContentType, "audio/") || strings.HasPrefix(file.ContentType, "video/") || file.ContentType == "application/ogg")
	if !valid {
		return "", invalidDesign("引用文件类型与用途不一致：" + target)
	}
	if prefix == "" {
		return value, nil
	}
	result := prefix + escapePackagePath(target)
	if u.Fragment != "" {
		result += "#" + url.PathEscape(u.Fragment)
	}
	return result, nil
}

// escapePackagePath 按每个路径片段编码，避免空格或中文路径被浏览器再次解释。
func escapePackagePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// validatePackageFile 对所有可执行或引用静态资源的源码执行一致校验。
func validatePackageFile(file packageFile, files map[string]packageFile) error {
	switch {
	case strings.HasPrefix(file.ContentType, "text/html"):
		_, err := packageHTML(file.Content, file.Path, "", "", "", files)
		return err
	case strings.HasPrefix(file.ContentType, "text/css"):
		_, err := packageCSS(string(file.Content), file.Path, "", files)
		return err
	case strings.HasPrefix(file.ContentType, "text/javascript"):
		return packageJS(string(file.Content), file.Path, files)
	}
	return nil
}

// packageHTML 使用真实HTML解析器校验实体、属性和编码，在租户脚本前插入唯一可信桥接SDK。
func packageHTML(raw []byte, current, prefix, nonce, sdk string, files map[string]packageFile, overrides ...CodeDesign) ([]byte, error) {
	doc, err := xhtml.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, invalidDesign("HTML无法解析")
	}
	var head, body *xhtml.Node
	var walk func(*xhtml.Node, int) error
	count := 0
	walk = func(n *xhtml.Node, depth int) error {
		count++
		if depth > 128 || count > 20000 {
			return invalidDesign("HTML嵌套或节点数量超限，请简化已构建页面")
		}
		if n.Type == xhtml.ElementNode {
			tag := strings.ToLower(n.Data)
			switch tag {
			case "iframe", "frame", "frameset", "object", "embed", "base", "form", "svg", "math":
				return invalidDesign("HTML不允许嵌套页面、表单、SVG、MathML或base标签")
			}
			if tag == "head" {
				head = n
			}
			if tag == "body" {
				body = n
			}
			attrs := map[string]string{}
			for _, a := range n.Attr {
				if _, exists := attrs[strings.ToLower(a.Key)]; exists {
					return invalidDesign("HTML不能包含同名重复属性")
				}
				attrs[strings.ToLower(a.Key)] = a.Val
			}
			if tag == "meta" {
				if attrs["http-equiv"] != "" {
					return invalidDesign("HTML不能设置http-equiv跳转、编码或策略")
				}
				if charset := strings.ToLower(attrs["charset"]); charset != "" && charset != "utf-8" && charset != "utf8" {
					return invalidDesign("HTML只支持UTF-8字符编码")
				}
			}
			if tag == "script" {
				t := strings.ToLower(attrs["type"])
				if t != "" && t != "module" && t != "text/javascript" && t != "application/javascript" {
					return invalidDesign("只支持普通或module脚本，不支持importmap；请导出已构建的相对模块")
				}
				if attrs["src"] == "" {
					for child := n.FirstChild; child != nil; child = child.NextSibling {
						if child.Type == xhtml.TextNode {
							if err := packageJS(child.Data, current, files); err != nil {
								return err
							}
						}
					}
				}
			}
			if tag == "style" {
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					if child.Type == xhtml.TextNode {
						css, err := packageCSS(child.Data, current, prefix, files)
						if err != nil {
							return err
						}
						child.Data = css
					}
				}
			}
			if tag == "link" {
				rel := strings.ToLower(attrs["rel"])
				if rel != "stylesheet" && rel != "icon" && rel != "preload" && rel != "modulepreload" {
					return invalidDesign("link仅支持包内stylesheet、icon、preload或modulepreload资源")
				}
			}
			for i := range n.Attr {
				a := &n.Attr[i]
				key := strings.ToLower(a.Key)
				if a.Namespace != "" || strings.HasPrefix(key, "on") || key == "srcdoc" || key == "action" || key == "formaction" || key == "ping" || key == "srcset" || key == "imagesrcset" {
					return invalidDesign("HTML包含事件属性或不支持的资源属性；请使用脚本addEventListener和单一src")
				}
				if key == "target" && a.Val != "" && a.Val != "_self" {
					return invalidDesign("装修页面只能在当前隔离窗口内导航")
				}
				if key == "style" {
					css, err := packageCSS(a.Val, current, prefix, files)
					if err != nil {
						return err
					}
					a.Val = css
				}
				if key == "src" || key == "href" || key == "poster" {
					kind := "any"
					switch {
					case tag == "script":
						kind = "script"
					case tag == "img" || key == "poster" || tag == "link" && attrs["rel"] == "icon":
						kind = "image"
					case tag == "a":
						kind = "html"
					case tag == "link" && attrs["rel"] == "stylesheet":
						kind = "style"
					case tag == "audio" || tag == "video" || tag == "source":
						kind = "media"
					}
					value, err := packageReference(a.Val, current, prefix, files, kind)
					if err != nil {
						return err
					}
					a.Val = value
				}
			}
			if tag == "script" && nonce != "" {
				filtered := n.Attr[:0]
				for _, a := range n.Attr {
					if a.Key != "nonce" {
						filtered = append(filtered, a)
					}
				}
				n.Attr = append(filtered, xhtml.Attribute{Key: "nonce", Val: nonce})
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(doc, 0); err != nil {
		return nil, err
	}
	if nonce != "" && head != nil {
		script := &xhtml.Node{Type: xhtml.ElementNode, Data: "script", Attr: []xhtml.Attribute{{Key: "nonce", Val: nonce}}}
		script.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: sdk})
		head.InsertBefore(script, head.FirstChild)
		charset := &xhtml.Node{Type: xhtml.ElementNode, Data: "meta", Attr: []xhtml.Attribute{{Key: "charset", Val: "utf-8"}}}
		head.InsertBefore(charset, head.FirstChild)
		if len(overrides) > 0 {
			code := overrides[0]
			if code.CSS != "" {
				css, err := packageCSS(code.CSS, current, prefix, files)
				if err != nil {
					return nil, err
				}
				style := &xhtml.Node{Type: xhtml.ElementNode, Data: "style"}
				style.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: css})
				head.AppendChild(style)
			}
			if code.Javascript != "" && body != nil {
				script := &xhtml.Node{Type: xhtml.ElementNode, Data: "script", Attr: []xhtml.Attribute{{Key: "nonce", Val: nonce}}}
				script.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: code.Javascript})
				body.AppendChild(script)
			}
		}
	}
	var out bytes.Buffer
	if err := xhtml.Render(&out, doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// cssWord 解码CSS标识或URL中的转义，防止转义后的url和import逃过校验。
func cssWord(value string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '\\' {
			out.WriteByte(value[i])
			i++
			continue
		}
		i++
		if i == len(value) {
			return "", invalidDesign("CSS转义不完整")
		}
		start := i
		for i < len(value) && i-start < 6 && strings.ContainsRune("0123456789abcdefABCDEF", rune(value[i])) {
			i++
		}
		if i > start {
			number, _ := strconv.ParseUint(value[start:i], 16, 32)
			if number == 0 || number > utf8.MaxRune {
				return "", invalidDesign("CSS包含非法转义")
			}
			out.WriteRune(rune(number))
			if i < len(value) && strings.ContainsRune(" \r\n\t\f", rune(value[i])) {
				i++
			}
		} else {
			if value[i] == '\n' || value[i] == '\r' {
				return "", invalidDesign("CSS引用不能跨行转义")
			}
			out.WriteByte(value[i])
			i++
		}
	}
	return out.String(), nil
}

// cssTokenEnd 定位字符串、注释或标识结尾，避免把注释与字符串内容误认为网络引用。
func cssTokenEnd(text string, start int, quote byte) int {
	i := start
	for i < len(text) {
		if text[i] == '\\' {
			i++
			start := i
			for i < len(text) && i-start < 6 && strings.ContainsRune("0123456789abcdefABCDEF", rune(text[i])) {
				i++
			}
			if i == start {
				if i < len(text) {
					i++
				}
			} else if i < len(text) && strings.ContainsRune(" \r\n\t\f", rune(text[i])) {
				i++
			}
			continue
		}
		if quote != 0 {
			if text[i] == quote {
				return i + 1
			}
		} else if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-", rune(text[i])) && text[i] < 128 {
			return i
		}
		i++
	}
	return len(text)
}

// packageCSS 校验并重写所有url和import引用，保留不涉及资源的CSS源码。
func packageCSS(text, current, prefix string, files map[string]packageFile) (string, error) {
	var out strings.Builder
	last := 0
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], "/*") {
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return "", invalidDesign("CSS注释未结束")
			}
			i += end + 4
			continue
		}
		if text[i] == '\'' || text[i] == '"' {
			i = cssTokenEnd(text, i+1, text[i])
			continue
		}
		at := text[i] == '@'
		start := i
		if at {
			i++
		}
		if i >= len(text) {
			break
		}
		if !(text[i] == '\\' || text[i] == '_' || text[i] == '-' || text[i] >= 'a' && text[i] <= 'z' || text[i] >= 'A' && text[i] <= 'Z') {
			i++
			continue
		}
		end := cssTokenEnd(text, i, 0)
		word, err := cssWord(text[i:end])
		if err != nil {
			return "", err
		}
		word = strings.ToLower(word)
		i = end
		if word == "expression" {
			return "", invalidDesign("CSS不能使用expression")
		}
		for i < len(text) && strings.ContainsRune(" \t\r\n", rune(text[i])) {
			i++
		}
		if at && word == "import" && i < len(text) && (text[i] == '\'' || text[i] == '"') {
			quote := text[i]
			stop := cssTokenEnd(text, i+1, quote)
			if stop <= i+1 || text[stop-1] != quote {
				return "", invalidDesign("CSS import字符串未结束")
			}
			value, err := cssWord(text[i+1 : stop-1])
			if err != nil {
				return "", err
			}
			value, err = packageReference(value, current, prefix, files, "style")
			if err != nil {
				return "", err
			}
			out.WriteString(text[last:i])
			out.WriteString(strconv.Quote(value))
			last = stop
			i = stop
			continue
		}
		if word != "url" || i >= len(text) || text[i] != '(' {
			if at && word == "import" && !(i+3 <= len(text) && strings.EqualFold(text[i:i+3], "url")) {
				return "", invalidDesign("CSS import只支持明确的包内CSS路径")
			}
			continue
		}
		i++
		for i < len(text) && strings.ContainsRune(" \t\r\n", rune(text[i])) {
			i++
		}
		valueStart := i
		stop := i
		if i < len(text) && (text[i] == '\'' || text[i] == '"') {
			q := text[i]
			stop = cssTokenEnd(text, i+1, q)
			if stop <= i+1 || text[stop-1] != q {
				return "", invalidDesign("CSS URL字符串未结束")
			}
			valueStart = i + 1
			i = stop
			stop--
		} else {
			for i < len(text) && text[i] != ')' {
				if text[i] == '\\' {
					i++
				}
				i++
			}
			stop = i
		}
		for i < len(text) && strings.ContainsRune(" \t\r\n", rune(text[i])) {
			i++
		}
		if i >= len(text) || text[i] != ')' {
			return "", invalidDesign("CSS URL未结束")
		}
		value, err := cssWord(strings.TrimSpace(text[valueStart:stop]))
		if err != nil {
			return "", err
		}
		value, err = packageReference(value, current, prefix, files, "any")
		if err != nil {
			return "", err
		}
		out.WriteString(text[last:start])
		out.WriteString("url(" + strconv.Quote(value) + ")")
		i++
		last = i
	}
	out.WriteString(text[last:])
	return out.String(), nil
}
