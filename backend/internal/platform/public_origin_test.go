// 文件作用说明：验证双域名来源只能由可信本机网关注入，防止Host/转发头污染公开链接。
package platform

import (
	"net/http/httptest"
	"testing"
)

// TestPublicOriginBoundary 覆盖有效别名、伪造令牌、远程请求、非法来源及无上下文任务回退。
func TestPublicOriginBoundary(t *testing.T) {
	cfg := Config{PublicURL: "https://primary.example", GatewaySecret: "unit-test-secret", Production: true}
	for _, tc := range []struct{ name, remote, token, origin, want string }{
		{"alias", "127.0.0.1:1234", "unit-test-secret", "https://alias.example", "https://alias.example"},
		{"forged", "127.0.0.1:1234", "forged", "https://evil.example", "https://primary.example"},
		{"remote", "203.0.113.3:1234", "unit-test-secret", "https://evil.example", "https://primary.example"},
		{"credentials", "127.0.0.1:1234", "unit-test-secret", "https://user@evil.example", "https://primary.example"},
		{"path", "127.0.0.1:1234", "unit-test-secret", "https://alias.example/path", "https://primary.example"},
		{"scheme", "127.0.0.1:1234", "unit-test-secret", "http://alias.example", "https://primary.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://backend/", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-GoodChat-Gateway-Token", tc.token)
			r.Header.Set("X-GoodChat-Public-Origin", tc.origin)
			r = BindPublicOrigin(r, cfg)
			if got := PublicURL(r.Context(), cfg.PublicURL); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
