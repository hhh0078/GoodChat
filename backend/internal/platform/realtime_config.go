// 文件作用说明：校验专用WebSocket来源和网页来源允许列表，配置错误时拒绝启动。
package platform

import (
	"fmt"
	"net/url"
	"strings"
)

// realtimeConfig 只接受规范来源，生产WSS不得降级为明文或携带路径、凭据和参数。
func realtimeConfig(endpoint, primary, aliases string, production bool) (string, []string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint != "" {
		u, e := url.Parse(endpoint)
		if e != nil || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "wss" && u.Scheme != "ws") || (production && u.Scheme != "wss") {
			return "", nil, fmt.Errorf("SUPPORT_REALTIME_URL必须是规范的WebSocket来源，生产环境只允许wss")
		}
		endpoint = u.Scheme + "://" + u.Host
	}
	origins := []string{}
	for _, value := range append([]string{primary}, strings.Split(aliases, ",")...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		u, e := url.Parse(value)
		if e != nil || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") || (production && u.Scheme != "https") {
			return "", nil, fmt.Errorf("SUPPORT_PUBLIC_ORIGINS必须是规范的网页来源")
		}
		origin := u.Scheme + "://" + u.Host
		found := false
		for _, old := range origins {
			if old == origin {
				found = true
			}
		}
		if !found {
			origins = append(origins, origin)
		}
	}
	return endpoint, origins, nil
}
