// 文件作用说明：使用真实WebSocket验证协议探活、无Pong断线、权限撤销和跨重启游标。
package chat

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"supportdesk/backend/internal/platform"
	"testing"
	"time"
)

// realtimeProbeFixture 建立真实双端连接，让调用方控制客户端是否读取并自动回复Pong。
func realtimeProbeFixture(t *testing.T, renew func(context.Context) error) (*websocket.Conn, context.Context, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	failure := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		go keepRealtimeAlive(ctx, conn, 10*time.Millisecond, 250*time.Millisecond, renew, func(err error) { failure <- err; cancel() })
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}))
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		cancel()
		server.Close()
		t.Fatal("无法建立探活测试连接")
	}
	t.Cleanup(func() { cancel(); conn.CloseNow(); server.Close() })
	return conn, ctx, failure
}

// TestRealtimeProbeWithoutApplicationTimer 验证不发送应用心跳时协议Pong仍驱动续租。
func TestRealtimeProbeWithoutApplicationTimer(t *testing.T) {
	renewed := make(chan struct{}, 10)
	conn, ctx, failure := realtimeProbeFixture(t, func(context.Context) error { renewed <- struct{}{}; return nil })
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-renewed:
		case <-failure:
			t.Fatal("正常Pong被误判断线")
		case <-time.After(2 * time.Second):
			t.Fatal("没有应用定时器时未续租")
		}
	}
}

// TestRealtimeProbeMissingPong 验证半开客户端不会续租，也不会永久占据连接。
func TestRealtimeProbeMissingPong(t *testing.T) {
	renewed := make(chan struct{}, 1)
	_, _, failure := realtimeProbeFixture(t, func(context.Context) error { renewed <- struct{}{}; return nil })
	select {
	case <-failure:
	case <-time.After(2 * time.Second):
		t.Fatal("缺失Pong没有触发超时")
	}
	select {
	case <-renewed:
		t.Fatal("没有Pong不能续租")
	default:
	}
}

// TestRealtimeProbeRevokedAuthorization 验证网络正常但权限撤销时仍停止续租。
func TestRealtimeProbeRevokedAuthorization(t *testing.T) {
	denied := platform.Fail(403, "membership_disabled", "身份已停用")
	conn, ctx, failure := realtimeProbeFixture(t, func(context.Context) error { return denied })
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	select {
	case err := <-failure:
		if !errors.Is(err, denied) || realtimeErrorCode(err) != websocket.StatusPolicyViolation {
			t.Fatal("权限撤销没有转换成策略关闭")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("权限撤销未结束探活")
	}
}

// TestRealtimeCursorSurvivesRestart 验证同配置跨实例恢复游标，换密钥后旧游标失效。
func TestRealtimeCursorSurvivesRestart(t *testing.T) {
	cfg := platform.Config{AuthSecret: "test-only-realtime-signing-secret-12345"}
	first, restarted := New(nil, nil, cfg), New(nil, nil, cfg)
	identity := actor{TenantID: "tenant", ContactID: "visitor"}
	cursor := first.encodeCursor(identity, "conversation", 42)
	seq, err := restarted.decodeCursor(identity, "conversation", cursor)
	if err != nil || seq != 42 {
		t.Fatal("重启后游标未恢复")
	}
	cfg.AuthSecret = "test-only-rotated-signing-secret-67890"
	if _, err := New(nil, nil, cfg).decodeCursor(identity, "conversation", cursor); err == nil {
		t.Fatal("换密钥后接受旧游标")
	}
}
