// 文件作用说明：兼容会拦截PATCH的CDN，通过受限POST方法覆盖继续调用原路由和鉴权，不放宽读写权限。
package platform

import (
	"net/http"
	"strings"
)

// MethodOverride 只允许API上的POST显式转为PATCH，禁止覆盖为GET或绕过其他请求方法。
func MethodOverride(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("X-HTTP-Method-Override")
		if len(values) > 0 {
			if len(values) != 1 || r.Method != "POST" || values[0] != "PATCH" || !strings.HasPrefix(r.URL.Path, "/api/v1/") {
				ErrorJSON(w, r, Fail(400, "invalid_method_override", "请求方法不被允许"))
				return
			}
			r = r.Clone(r.Context())
			r.Method = http.MethodPatch
			r.Header.Del("X-HTTP-Method-Override")
		}
		next.ServeHTTP(w, r)
	})
}
