// 文件作用说明：解析服务端指定的专用WebSocket地址，保留旧相对路径且不向WSS转发登录Cookie。
package client

import (
	"errors"
	"net/url"
)

// realtimeURL 接受可信票据API指定的消息端点，拒绝明文降级、凭据及消息以外的路径。
func realtimeURL(server, value string) (*url.URL, error) {
	base, e := url.Parse(server)
	if e != nil {
		return nil, errors.New("服务器地址无效")
	}
	u, e := url.Parse(value)
	if e != nil {
		return nil, errors.New("消息连接地址无效")
	}
	if !u.IsAbs() && u.Host == "" {
		u = base.ResolveReference(u)
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		if u.Scheme != base.Scheme || u.Host != base.Host {
			return nil, errors.New("消息连接来源无效")
		}
		if u.Scheme == "https" {
			u.Scheme = "wss"
		} else {
			u.Scheme = "ws"
		}
	}
	if (u.Scheme != "ws" && u.Scheme != "wss") || (base.Scheme == "https" && u.Scheme != "wss") || u.User != nil || u.Fragment != "" || u.Hostname() == "" || u.Path != "/ws/v1" || len(u.Query()) != 1 || len(u.Query()["ticket"]) != 1 || u.Query().Get("ticket") == "" {
		return nil, errors.New("服务器返回的消息连接地址无效")
	}
	return u, nil
}
