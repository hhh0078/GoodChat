// 文件作用说明：验证专用WSS网页来源边界，真实数据库下检查客服和访客票据及拒绝后不消耗票据。
package chat

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestWebsocketOriginBoundary 精确隔离网页域名、协议、端口和伪造来源。
func TestWebsocketOriginBoundary(t *testing.T) {
	allowed := []string{"https://main.example", "https://alias.example"}
	for _, value := range []string{"", "https://main.example", "https://alias.example"} {
		if !allowedWebsocketOrigin(value, allowed) {
			t.Fatal("合法来源被拒绝", value)
		}
	}
	for _, value := range []string{"null", "https://evil.example", "http://main.example", "https://main.example:8443", "https://main.example/path", "https://user@main.example"} {
		if allowedWebsocketOrigin(value, allowed) {
			t.Fatal("非法来源被接受", value)
		}
	}
}

// TestDedicatedWebsocketEndpoint 验证客服和访客均使用独立端点，陌生来源被拒绝且票据仍可由合法来源使用。
func TestDedicatedWebsocketEndpoint(t *testing.T) {
	f := newFixture(t)
	transport := httptest.NewServer(http.HandlerFunc(f.s.websocket))
	defer transport.Close()
	f.s.cfg.RealtimeURL = "ws" + strings.TrimPrefix(transport.URL, "http")
	f.s.cfg.PublicURL = "https://main.example"
	f.s.cfg.PublicOrigins = []string{"https://main.example", "https://alias.example"}
	f.s.cfg.Production = true
	token, _, _ := f.visitor("")
	for _, actor := range []struct{ member, token, origin string }{{f.agent, "", "https://main.example"}, {"", token, "https://alias.example"}} {
		status, body := f.request("POST", "/api/v1/realtime/ticket", actor.member, actor.token, map[string]any{})
		ticket := mustData(t, status, body)
		endpoint := ticket["websocket_url"].(string)
		if !strings.HasPrefix(endpoint, f.s.cfg.RealtimeURL+"/ws/v1?") || !strings.HasPrefix(ticket["url"].(string), "/ws/v1?") {
			t.Fatal("票据兼容字段错误")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		_, res, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}}})
		cancel()
		if err == nil || res == nil || res.StatusCode != 403 {
			t.Fatal("陌生来源未被拒绝")
		}
		ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
		conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{actor.origin}}})
		if err != nil {
			cancel()
			t.Fatal("合法来源连接失败")
		}
		_, raw, err := conn.Read(ctx)
		conn.CloseNow()
		cancel()
		var frame struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &frame)
		if err != nil || frame.Type != "ready" {
			t.Fatal("未收到真实ready帧")
		}
		ctx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
		_, res, err = websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{actor.origin}}})
		cancel()
		if err == nil || res == nil || res.StatusCode != 401 {
			t.Fatal("票据可被重复使用")
		}
	}
	// 未配置专用端点时保留旧版同源校验，不能影响已有本机代理和服务端。
	f.s.cfg.RealtimeURL = ""
	f.s.cfg.Production = false
	status, body := f.request("POST", "/api/v1/realtime/ticket", f.agent, "", map[string]any{})
	legacy := mustData(t, status, body)
	if legacy["websocket_url"] != nil {
		t.Fatal("旧部署出现跨域端点")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	conn, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+legacy["url"].(string), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{f.server.URL}}})
	cancel()
	if e != nil {
		t.Fatal("旧版同源连接回归失败")
	}
	conn.CloseNow()

}
