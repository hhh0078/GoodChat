// 文件作用说明：专用WSS接受已配置网页来源，拒绝陌生页面且不消耗其携带的短期票据。
package chat

import (
	"net/http"
	"net/url"
	"strings"
	"supportdesk/backend/internal/platform"
)

// websocketOrigins 同域模式保持原来源，专用模式额外接受部署明确允许的网页来源。
func (s *Service) websocketOrigins(r *http.Request) []string {
	origins := []string{platform.PublicURL(r.Context(), s.cfg.PublicURL)}
	if s.cfg.RealtimeURL != "" {
		origins = append(origins, s.cfg.PublicOrigins...)
		origins = append(origins, s.cfg.PublicURL)
	}
	if !s.cfg.Production {
		origins = append(origins, "http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:13000", "http://127.0.0.1:13000")
	}
	return origins
}

// allowedWebsocketOrigin 精确校验浏览器的完整来源；无Origin的原生客户端仍须通过一次性票据认证。
func allowedWebsocketOrigin(value string, origins []string) bool {
	if value == "" {
		return true
	}
	u, e := url.Parse(value)
	if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	for _, candidate := range origins {
		v, e := url.Parse(candidate)
		if e == nil && u.Scheme == v.Scheme && strings.EqualFold(u.Host, v.Host) {
			return true
		}
	}
	return false
}
