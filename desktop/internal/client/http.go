// 文件作用说明：把嵌入界面的同源API交给原生HTTP客户端，Cookie不暴露给网页或其他服务器。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type reply struct {
	Status int
	Body   []byte
	Header http.Header
}

// request 只请求已选服务器的固定相对路径，禁止重定向转移认证信息。
func (c *Client) request(s *session, method, path, tenant string, body []byte, contentType string) (reply, error) {
	return c.requestContext(s.ctx, s, method, path, tenant, body, contentType)
}

// requestContext 为界面与后台请求使用相同的安全目标和Cookie存储。
func (c *Client) requestContext(ctx context.Context, s *session, method, path, tenant string, body []byte, contentType string, extra ...http.Header) (reply, error) {
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || (!strings.HasPrefix(u.Path, "/api/v1/") && u.Path != "/healthz") {
		return reply{}, errors.New("请求地址无效")
	}
	r, err := http.NewRequestWithContext(ctx, method, s.server+path, bytes.NewReader(body))
	if err != nil {
		return reply{}, err
	}
	if tenant != "" {
		r.Header.Set("X-Tenant-ID", tenant)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "GoodChat-Desktop/"+c.version)
	for _, headers := range extra {
		for _, key := range []string{"Idempotency-Key", "If-Match", "Range"} {
			if value := headers.Get(key); value != "" {
				r.Header.Set(key, value)
			}
		}
	}
	res, err := s.http.Do(r)
	if err != nil {
		return reply{}, errors.New("暂时无法连接服务器，请检查网络")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32*1024*1024+1))
	if err != nil || len(raw) > 32*1024*1024 {
		return reply{}, errors.New("服务器响应中断或超过大小限制")
	}
	for _, cookie := range res.Cookies() {
		if cookie.Name != "support_session" {
			continue
		}
		s.credentialMu.Lock()
		if cookie.MaxAge < 0 {
			_ = c.vault.Delete("GoodChat", scopeHash(s.server))
		} else {
			// 只保存host-only主会话，过期时间沿用服务端，不自行延长。
			cookie.Domain = ""
			cookie.Path = "/"
			if cookie.Expires.IsZero() && cookie.MaxAge > 0 {
				cookie.Expires = time.Now().Add(time.Duration(cookie.MaxAge) * time.Second)
			}
			encoded, _ := json.Marshal([]*http.Cookie{cookie})
			if err = c.vault.Set("GoodChat", scopeHash(s.server), string(encoded)); err != nil {
				s.credentialMu.Unlock()
				return reply{}, errors.New("系统凭据库不可用，无法安全保存登录，请检查钥匙串或凭据管理器")
			}
		}
		s.credentialMu.Unlock()
	}
	return reply{Status: res.StatusCode, Body: raw, Header: res.Header}, nil
}

// Middleware 将本地API请求转交原生层，其余请求继续读取随包携带的界面。
func (c *Client) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; media-src 'self' blob:; connect-src 'self'; font-src 'self' data:; object-src 'none'; base-uri 'self'; frame-src 'none'; frame-ancestors 'none'; form-action 'self'")
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				localError(w, 403, "invalid_origin", "请求来源无效")
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			localError(w, 403, "invalid_origin", "请求来源无效")
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/login-code") {
			if err := c.beginLogin(); err != nil {
				localError(w, 400, "server_required", err.Error())
				return
			}
		}
		s, err := c.current()
		if err != nil {
			localError(w, 400, "server_required", err.Error())
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stop := context.AfterFunc(s.ctx, cancel)
		defer stop()
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 24*1024*1024))
		if err != nil {
			localError(w, 413, "request_too_large", "文件或请求超过大小限制")
			return
		}
		if r.Method == http.MethodPost && messagePath(r.URL.Path) {
			c.sendMessage(ctx, s, w, r, body)
			return
		}
		result, err := c.requestContext(ctx, s, r.Method, r.URL.RequestURI(), r.Header.Get("X-Tenant-ID"), body, r.Header.Get("Content-Type"), r.Header)
		if err != nil {
			localError(w, 502, "desktop_network", err.Error())
			return
		}
		if r.URL.Path == "/api/v1/auth/logout" && result.Status == 200 {
			c.clearIdentity(s)
		}
		for _, key := range []string{"Content-Type", "Content-Disposition", "X-Request-ID"} {
			if value := result.Header.Get(key); value != "" {
				w.Header().Set(key, value)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(result.Status)
		_, _ = w.Write(result.Body)
	})
}

// clearIdentity 退出后停止后台发送，保留原账号分区等待本人重新认证。
func (c *Client) clearIdentity(s *session) {
	s.mu.Lock()
	if s.runCancel != nil {
		s.runCancel()
	}
	s.scope = ""
	s.user = ""
	s.tenant = ""
	s.member = ""
	s.state = State{Status: "idle"}
	s.mu.Unlock()
}

// localError 按项目错误结构返回本机错误，不伪装成服务端已确认。
func localError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "request_id": "desktop-local"})
}

// messagePath 精确识别普通消息发送入口，撤回和其他业务请求保持原接口行为。
func messagePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "conversations" && parts[3] != "" && parts[4] == "messages"
}
