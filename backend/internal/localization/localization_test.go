// 文件作用说明：验证逐请求语言优先级、目录完整性、时区合法性与跨端消息标识一致性。
package localization

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRequestLanguagePrecedence 验证有效 URL 优先且 Accept-Language 权重正常解析。
func TestRequestLanguagePrecedence(t *testing.T) {
	r := httptest.NewRequest("GET", "/?lang=de", nil)
	r.Header.Set("X-GoodChat-Locale", "ja")
	r.Header.Set("Accept-Language", "fr;q=0.2,en;q=0.9")
	if RequestLocale(r) != "de" {
		t.Fatal("URL未优先")
	}
	r = httptest.NewRequest("GET", "/?lang=bad&lang=ko", nil)
	r.Header.Set("Accept-Language", "xx,fr;q=0.2,en;q=0.9")
	if RequestLocale(r) != "en" {
		t.Fatal("语言权重或重复参数处理错误")
	}
	r.AddCookie(&http.Cookie{Name: "goodchat.locale", Value: "zh-TW"})
	if RequestLocale(r) != "zh-TW" {
		t.Fatal("已确认语言未优先")
	}
}

// TestCatalogCoverage 验证九语目录键完全一致，避免漏掉后端和独立访客脚本。
func TestCatalogCoverage(t *testing.T) {
	for _, locale := range Locales {
		if len(messages[locale]) != len(messages["zh-CN"]) {
			t.Fatalf("%s目录数量不一致", locale)
		}
		for key := range messages["zh-CN"] {
			if _, ok := messages[locale][key]; !ok {
				t.Fatalf("%s缺少%s", locale, key)
			}
		}
	}
	if Key("登录") != "mbad2af27" {
		t.Fatal("跨端消息标识不一致")
	}
	if !ValidTimezone("America/New_York") || ValidTimezone("UTC+8") || ValidTimezone("Bad/Zone") {
		t.Fatal("时区校验错误")
	}
}

// TestMiddlewareKeepsRoutePattern 验证语言上下文不丢失外层可观察的安全路由模式。
func TestMiddlewareKeepsRoutePattern(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		if ContextLocale(r.Context()) != "fr" {
			t.Error("上下文语言错误")
		}
	})
	r := httptest.NewRequest("GET", "/items/123?lang=fr", nil)
	w := httptest.NewRecorder()
	Middleware(mux).ServeHTTP(w, r)
	if r.Pattern != "GET /items/{id}" || w.Header().Get("Content-Language") != "fr" {
		t.Fatal("路由或响应语言丢失")
	}
}
