// 文件作用说明：验证本机网关传入的公开来源，并通过请求上下文为多域名链接提供可信站点地址。
package platform

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type publicOriginKey struct{}

// PublicURL 优先返回可信请求来源；后台任务没有网页上下文时使用本站部署配置。
func PublicURL(ctx context.Context, fallback string) string {
	if value, ok := ctx.Value(publicOriginKey{}).(string); ok && value != "" {
		return value
	}
	return strings.TrimRight(fallback, "/")
}

// BindPublicOrigin 只信任携带内部令牌的本机网关，拒绝访客伪造来源头和非法URL。
func BindPublicOrigin(r *http.Request, cfg Config) *http.Request {
	secret := cfg.GatewaySecret
	if secret == "" {
		secret = cfg.AuthSecret
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || secret == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-GoodChat-Gateway-Token")), []byte(secret)) != 1 {
		return r
	}
	value := r.Header.Get("X-GoodChat-Public-Origin")
	origin, err := url.Parse(value)
	if err != nil || origin.Hostname() == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || (origin.Scheme != "https" && origin.Scheme != "http") || (cfg.Production && origin.Scheme != "https") {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), publicOriginKey{}, origin.Scheme+"://"+origin.Host))
}
