// 文件作用说明：通过真实HTTP票据与WebSocket验证立即续租、重复票据拒绝和客服上下班意图保留。
package chat

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"strings"
	"testing"
	"time"
)

// TestRealtimePresenceAndSingleUseTicket 验证重连无需等待15秒即可恢复在线，但不会把下班或示忙改为可接待。
func TestRealtimePresenceAndSingleUseTicket(t *testing.T) {
	f := newFixture(t)
	status, reply := f.request("PATCH", "/api/v1/agent/status", f.agent, "", map[string]any{"available": false, "busy": true})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(context.Background(), "UPDATE agent_runtime SET online_until=now()-interval '1 minute' WHERE membership_id=$1", f.agent); err != nil {
		t.Fatal("无法准备离线租约")
	}
	status, reply = f.request("POST", "/api/v1/realtime/ticket", f.agent, "", map[string]any{})
	ticket := mustData(t, status, reply)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(f.server.URL, "http") + ticket["url"].(string)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal("一次性票据首次连接失败")
	}
	defer conn.CloseNow()
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal("未收到连接就绪帧")
	}
	var frame map[string]any
	if json.Unmarshal(raw, &frame) != nil || frame["type"] != "ready" {
		t.Fatal("首帧不是ready")
	}
	var online, available, busy bool
	if err := f.admin.QueryRow(ctx, "SELECT online_until>now(),available,busy FROM agent_runtime WHERE membership_id=$1", f.agent).Scan(&online, &available, &busy); err != nil {
		t.Fatal("无法核验即时续租")
	}
	if !online || available || !busy {
		t.Fatal("连接恢复未即时续租，或错误修改上下班/忙碌意图")
	}
	duplicate, response, err := websocket.Dial(ctx, url, nil)
	if duplicate != nil {
		duplicate.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode < 400 {
		t.Fatal("同一票据被重复消费")
	}
}

// TestRealtimeVisitorPresence 验证访客连接建立后立即产生在线租约。
func TestRealtimeVisitorPresence(t *testing.T) {
	f := newFixture(t)
	token, contact, _ := f.visitor("13800000000")
	status, reply := f.request("POST", "/api/v1/realtime/ticket", "", token, map[string]any{})
	ticket := mustData(t, status, reply)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+ticket["url"].(string), nil)
	if err != nil {
		t.Fatal("访客连接失败")
	}
	defer conn.CloseNow()
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal("访客没有收到ready")
	}
	var online bool
	if err := f.admin.QueryRow(ctx, "SELECT online_until>now() FROM contacts WHERE id=$1", contact).Scan(&online); err != nil || !online {
		t.Fatal("访客连接未更新在线租约")
	}
}
