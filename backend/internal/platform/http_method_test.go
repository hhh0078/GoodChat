// 文件作用说明：验证CDN兼容请求仍进入原PATCH鉴权路径，非法方法覆盖被拒绝。
package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMethodOverrideKeepsPatchAuthorization 拒绝读请求伪装和越权路由，只转换明确的写方法。
func TestMethodOverrideKeepsPatchAuthorization(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Error("未调用原PATCH路由")
		}
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
	})
	for _, v := range []struct {
		method, path, override, auth string
		want                         int
	}{{"POST", "/api/v1/agent/status", "PATCH", "", 401}, {"POST", "/api/v1/agent/status", "PATCH", "authenticated", 200}, {"GET", "/api/v1/agent/status", "PATCH", "", 400}, {"POST", "/api/v1/agent/status", "GET", "", 400}, {"POST", "/render/abc", "PATCH", "", 400}} {
		r := httptest.NewRequest(v.method, v.path, strings.NewReader(`{"available":true,"busy":false}`))
		r.Header.Set("X-HTTP-Method-Override", v.override)
		r.Header.Set("Authorization", v.auth)
		w := httptest.NewRecorder()
		MethodOverride(next).ServeHTTP(w, r)
		if w.Code != v.want {
			t.Fatalf("%s %s: %d", v.method, v.override, w.Code)
		}
	}
}
