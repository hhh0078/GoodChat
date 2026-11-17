// Package localization 提供九种系统语言、稳定消息标识和逐请求语言协商，不翻译用户业务内容。
package localization

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"

	"golang.org/x/text/language"
)

//go:embed catalogs/*.json
var catalogs embed.FS
var messages = loadCatalogs()
var Locales = []string{"zh-CN", "en", "zh-TW", "ja", "ko", "es", "pt-BR", "fr", "de"}

type localeKey struct{}

// loadCatalogs 在启动时读取静态语言目录，目录损坏立即失败，不在运行时请求翻译服务。
func loadCatalogs() map[string]map[string]string {
	result := map[string]map[string]string{}
	entries, err := catalogs.ReadDir("catalogs")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		body, err := catalogs.ReadFile("catalogs/" + entry.Name())
		if err != nil {
			panic(err)
		}
		var values map[string]string
		if err := json.Unmarshal(body, &values); err != nil {
			panic(err)
		}
		result[strings.TrimSuffix(entry.Name(), ".json")] = values
	}
	return result
}

// Normalize 将浏览器区域语言映射到支持语言；未知值保持为空以便继续协商。
func Normalize(value string) string {
	value = strings.ToLower(strings.ReplaceAll(value, "_", "-"))
	if value == "zh" || value == "zh-cn" || value == "zh-sg" || strings.HasPrefix(value, "zh-hans") {
		return "zh-CN"
	}
	if value == "zh-tw" || value == "zh-hk" || value == "zh-mo" || strings.HasPrefix(value, "zh-hant") {
		return "zh-TW"
	}
	base := strings.Split(value, "-")[0]
	if base == "pt" {
		return "pt-BR"
	}
	for _, locale := range []string{"en", "ja", "ko", "es", "fr", "de"} {
		if base == locale {
			return locale
		}
	}
	return ""
}

// ValidTimezone 验证明确的 IANA 时区名称，拒绝固定偏移与不存在的名称。
func ValidTimezone(value string) bool {
	if len(value) > 100 || (value != "UTC" && !strings.Contains(value, "/")) {
		return false
	}
	_, err := time.LoadLocation(value)
	return err == nil
}

// RequestLocale 优先使用有效 URL 语言，再读取明确请求头、偏好 Cookie 和浏览器语言。
func RequestLocale(r *http.Request) string {
	if values := r.URL.Query()["lang"]; len(values) == 1 {
		if value := Normalize(values[0]); value != "" {
			return value
		}
	}
	if value := Normalize(r.Header.Get("X-GoodChat-Locale")); value != "" {
		return value
	}
	if cookie, err := r.Cookie("goodchat.locale"); err == nil {
		if value := Normalize(cookie.Value); value != "" {
			return value
		}
	}
	tags, _, _ := language.ParseAcceptLanguage(r.Header.Get("Accept-Language"))
	for _, tag := range tags {
		if value := Normalize(tag.String()); value != "" {
			return value
		}
	}
	if r.Header.Get("Accept-Language") != "" || r.URL.Query().Has("lang") {
		return "en"
	}
	return "zh-CN"
}

// WithLocale 将当前请求语言放入上下文，供事务内投影和后台任务快照读取。
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeKey{}, locale)
}

// ContextLocale 读取明确的请求语言；旧调用回退中文保持兼容。
func ContextLocale(ctx context.Context) string {
	if locale, ok := ctx.Value(localeKey{}).(string); ok {
		return locale
	}
	return "zh-CN"
}

// Key 对 UTF-8 原文生成与前端一致的稳定消息标识。
func Key(source string) string {
	digest := fnv.New32a()
	_, _ = digest.Write([]byte(source))
	return fmt.Sprintf("m%08x", digest.Sum32())
}

// ScriptMessages 仅提供平台脚本实际引用的文案，避免把整套后台目录送入每个访客页面。
func ScriptMessages(locale, script string) map[string]string {
	result := map[string]string{}
	for key, source := range messages["zh-CN"] {
		if len(source) < 2000 && strings.Contains(script, source) {
			result[key] = messages[locale][key]
		}
	}
	return result
}

// ScriptPatterns 为已冻结的旧控件恢复命名参数，不处理任意聊天正文。
func ScriptPatterns(locale string, selected map[string]string) []map[string]string {
	result := []map[string]string{}
	for key, text := range selected {
		source := messages["zh-CN"][key]
		if strings.Contains(source, "{p") {
			result = append(result, map[string]string{"source": source, "text": text})
		}
	}
	return result
}

// Text 只翻译明确标记的系统文案，缺失时保留原文，不修改任意用户数据。
func Text(locale, source string) string {
	if translated, ok := messages[locale][Key(source)]; ok {
		return translated
	}
	return source
}

// Format 先翻译系统格式模板再插入原始业务参数，避免改写姓名、链接和验证码。
func Format(locale, source string, args ...any) string {
	value := Text(locale, source)
	if len(args) == 0 {
		return value
	}
	return fmt.Sprintf(value, args...)
}

// RequestText 按请求语言生成系统邮件、导出和公开提示。
func RequestText(r *http.Request, source string, args ...any) string {
	return Format(RequestLocale(r), source, args...)
}

// Middleware 为响应标记语言并隔离上下文，不改变身份、租户或业务时区。
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		locale := RequestLocale(r)
		w.Header().Set("Content-Language", locale)
		w.Header().Add("Vary", "Accept-Language, Cookie, X-GoodChat-Locale")
		localized := r.WithContext(WithLocale(r.Context(), locale))
		next.ServeHTTP(w, localized)
		r.Pattern = localized.Pattern
	})
}
