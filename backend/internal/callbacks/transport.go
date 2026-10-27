// 文件作用说明：限制回调目标到公网HTTPS，校验重定向和解析地址并对请求签名。
package callbacks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"supportdesk/backend/internal/platform"
)

// validateURL 验证回调地址及全部解析结果；测试仅在明确开发开关下允许本机。
func (s *Service) validateURL(ctx context.Context, address string) error {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(address) > 2048 {
		return platform.Fail(400, "invalid_callback", "请填写有效的HTTPS回调地址，不包含账号密码或片段")
	}
	allowLocal := s.config.CallbackAllowLocal && !s.config.Production
	if u.Scheme != "https" && !(allowLocal && u.Scheme == "http") {
		return platform.Fail(400, "https_required", "回调地址需要使用HTTPS")
	}
	lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookup, u.Hostname())
	if err != nil || len(addresses) == 0 {
		return platform.Fail(400, "callback_unreachable", "暂时无法解析回调地址，请检查域名")
	}
	for _, address := range addresses {
		if !allowLocal && !publicIP(address.IP) {
			return platform.Fail(400, "private_callback", "回调地址不能指向本机或内部网络")
		}
	}
	return nil
}

// publicIP 排除本机私网和常见保留地址，防止回调被用于访问内部服务。
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		prefix := netip.MustParsePrefix(block)
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

// client 在拨号时再次校验并固定解析IP，不跟随跳转，不使用环境代理绕过限制。
func (s *Service) client() *http.Client {
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		allowLocal := s.config.CallbackAllowLocal && !s.config.Production
		for _, candidate := range addresses {
			if !allowLocal && !publicIP(candidate.IP) {
				return nil, fmt.Errorf("目标地址不在允许范围")
			}
		}
		var last error
		for _, candidate := range addresses {
			connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if err == nil {
				return connection, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
}

// signature 对时间戳和原始JSON正文生成HMAC摘要，接收方据事件ID去重。
func signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// fmtUUID 输出稳定UUID文本，不依赖外部格式化库。
func fmtUUID(value []byte) string {
	if len(value) != 16 {
		return ""
	}
	text := hex.EncodeToString(value)
	return strings.Join([]string{text[:8], text[8:12], text[12:16], text[16:20], text[20:]}, "-")
}
