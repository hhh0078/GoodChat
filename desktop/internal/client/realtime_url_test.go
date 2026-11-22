// 文件作用说明：验证原生客户端能使用专用WSS，同时兼容旧服务端并拒绝不安全地址。
package client

import (
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRealtimeURL 验证相对/独立地址及混合内容、凭据和路径边界。
func TestRealtimeURL(t *testing.T) {
	for _, value := range []string{"/ws/v1?ticket=one", "wss://socket.example/ws/v1?ticket=one"} {
		if _, e := realtimeURL("https://main.example", value); e != nil {
			t.Fatal(e)
		}
	}
	for _, value := range []string{"ws://socket.example/ws/v1?ticket=one", "https://other.example/ws/v1?ticket=one", "wss://user@socket.example/ws/v1?ticket=one", "wss://socket.example/admin?ticket=one", "wss://socket.example/ws/v1?ticket=one&ticket=two"} {
		if _, e := realtimeURL("https://main.example", value); e == nil {
			t.Fatal("不安全地址被接受", value)
		}
	}
}

// TestDedicatedNativeTransport 验证原生客户端真实连接独立端点，不把账号Cookie或Authorization发送到消息服务。
func TestDedicatedNativeTransport(t *testing.T) {
	received := make(chan struct{}, 1)
	ws := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("登录凭据泄露到消息端点")
		}
		conn, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"ready"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"event","data":{"id":"dedicated-event","event_type":"message.created","conversation_id":"conversation"}}`))
		for {
			if _, _, e = conn.Read(r.Context()); e != nil {
				return
			}
		}
	}))
	defer ws.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "support_session", Value: "private-account-cookie", Path: "/"})
		fmt.Fprint(w, `{"data":{"user":{"id":"account"},"memberships":[{"id":"member","tenant_id":"tenant"}]}}`)
	})
	mux.HandleFunc("/api/v1/realtime/ticket", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"url": "/ws/v1?ticket=legacy", "websocket_url": "ws" + strings.TrimPrefix(ws.URL, "http") + "/ws/v1?ticket=one"}})
	})
	mux.HandleFunc("/api/v1/agent/conversations", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":{"items":[],"has_more":false}}`) })
	server := httptest.NewServer(mux)
	defer server.Close()
	c := newTestClient(t, server.URL, func(name string, _ any) {
		if name == "desktop:event" {
			select {
			case received <- struct{}{}:
			default:
			}
		}
	})
	if e := c.SetIdentity("tenant"); e != nil {
		t.Fatal(e)
	}
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("未收到专用端点事件")
	}
}
