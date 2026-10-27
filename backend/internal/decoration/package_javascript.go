// 文件作用说明：静态识别ES模块依赖，支持包内相对import/export，明确拒绝依赖CDN、裸包名或运行时计算的模块路径。
package decoration

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// jsToken 保存与模块依赖识别有关的词法单元，不执行租户JavaScript。
type jsToken struct{ kind, value string }

// jsQuoted 读取JavaScript字符串并解码引用路径所需的标准字符转义。
func jsQuoted(text string, start int) (string, int, error) {
	quote := text[start]
	var out strings.Builder
	for i := start + 1; i < len(text); i++ {
		c := text[i]
		if c == quote {
			return out.String(), i + 1, nil
		}
		if c == '\n' || c == '\r' {
			return "", 0, invalidDesign("JavaScript字符串未结束")
		}
		if c != '\\' {
			out.WriteByte(c)
			continue
		}
		i++
		if i >= len(text) {
			break
		}
		c = text[i]
		switch c {
		case '\n':
			continue
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			continue
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'x', 'u':
			n := 2
			if c == 'u' {
				n = 4
			}
			if i+n >= len(text) {
				return "", 0, invalidDesign("JavaScript字符转义不完整")
			}
			value, err := strconv.ParseUint(text[i+1:i+1+n], 16, 32)
			if err != nil || value > utf8.MaxRune || value >= 0xd800 && value <= 0xdfff {
				return "", 0, invalidDesign("JavaScript模块路径字符转义无效")
			}
			out.WriteRune(rune(value))
			i += n
		default:
			out.WriteByte(c)
		}
	}
	return "", 0, invalidDesign("JavaScript字符串未结束")
}

// jsTokens 跳过注释和普通字符串，避免把文案中的import当成真实模块依赖。
func jsTokens(text string) ([]jsToken, error) {
	tokens := []jsToken{}
	for i := 0; i < len(text); {
		if len(tokens) > 200000 {
			return nil, invalidDesign("JavaScript词法单元过多，请拆分静态模块")
		}
		if strings.ContainsRune(" \t\n\r", rune(text[i])) {
			i++
			continue
		}
		if strings.HasPrefix(text[i:], "//") {
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				break
			}
			i += end
			continue
		}
		if strings.HasPrefix(text[i:], "/*") {
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return nil, invalidDesign("JavaScript注释未结束")
			}
			i += end + 4
			continue
		}
		c := text[i]
		if c == '\'' || c == '"' {
			value, end, err := jsQuoted(text, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, jsToken{"string", value})
			i = end
			continue
		}
		if c == '`' {
			// 模板表达式可能包含动态import，保守拒绝其中出现的模块装载，普通模板文案保持支持。
			start := i
			i++
			for i < len(text) {
				if text[i] == '\\' {
					i = min(i+2, len(text))
					continue
				}
				if text[i] == '`' {
					i++
					break
				}
				i++
			}
			if strings.Contains(text[start:i], "import(") || strings.Contains(text[start:i], "import (") {
				return nil, invalidDesign("模板表达式内的动态模块路径请预构建为明确相对import")
			}
			tokens = append(tokens, jsToken{"template", ""})
			continue
		}
		if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128 {
			start := i
			i++
			for i < len(text) {
				c = text[i]
				if !(c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 128) {
					break
				}
				i++
			}
			tokens = append(tokens, jsToken{"word", text[start:i]})
			continue
		}
		tokens = append(tokens, jsToken{"punct", string(c)})
		i++
	}
	return tokens, nil
}

// packageJS 验证每个明确模块依赖都存在本包中，预览下依赖继续继承短凭证路径。
func packageJS(text, current string, files map[string]packageFile) error {
	tokens, err := jsTokens(text)
	if err != nil {
		return err
	}
	check := func(value string) error {
		if !strings.HasPrefix(value, "./") && !strings.HasPrefix(value, "../") {
			return invalidDesign("ES模块仅支持包内./或../相对路径；CDN、裸包名、importmap和根路径请先构建为静态相对依赖")
		}
		_, err := packageReference(value, current, "", files, "script")
		return err
	}
	for i, t := range tokens {
		if t.kind != "word" || (t.value != "import" && t.value != "export") || i > 0 && tokens[i-1].value == "." || i+1 >= len(tokens) {
			continue
		}
		next := tokens[i+1]
		if t.value == "import" {
			if next.value == "." {
				continue
			}
			if next.kind == "string" {
				if err := check(next.value); err != nil {
					return err
				}
				continue
			}
			if next.value == "(" {
				if i+3 >= len(tokens) || tokens[i+2].kind != "string" || (tokens[i+3].value != ")" && tokens[i+3].value != ",") {
					return invalidDesign("动态import必须使用明确的相对字符串路径，请先构建动态模块依赖")
				}
				if err := check(tokens[i+2].value); err != nil {
					return err
				}
				continue
			}
		}
		for j := i + 1; j < len(tokens) && j < i+300; j++ {
			if tokens[j].value == ";" {
				break
			}
			if tokens[j].kind == "word" && tokens[j].value == "from" && j+1 < len(tokens) && tokens[j+1].kind == "string" {
				if err := check(tokens[j+1].value); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}
