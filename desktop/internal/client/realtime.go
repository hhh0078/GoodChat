// 文件作用说明：在原生Go协程中维持实时连接、心跳超时和退避重连，不依赖网页运行。
package client

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

var errDenied = errors.New("身份或权限已经失效")

// run 同时运行原生接收与持久队列任务，权限失效会停止本次身份的全部自动动作。
func (c *Client) run(parent context.Context, s *session, tenant, scope string) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); c.maintenance(ctx, s, tenant, scope) }()
	defer func() { cancel(); <-done }()
	attempt := 0
	for ctx.Err() == nil {
		c.setState(ctx, s, "connecting", "正在连接消息服务")
		started := time.Now()
		err := c.connect(ctx, s, tenant, scope)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errDenied) {
			c.setState(ctx, s, "denied", "登录或成员权限已失效，请重新登录")
			return
		}
		c.setState(ctx, s, "reconnecting", "连接已中断，正在重新连接并核对消息")
		if time.Since(started) >= 30*time.Second {
			attempt = 0
		}
		wait := min(30*time.Second, time.Second*time.Duration(1<<min(attempt, 5)))
		wait = time.Duration(float64(wait) * (0.75 + rand.Float64()*0.5))
		attempt++
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// connect 每次申请新票据，收到ready后才执行持久事件恢复。
func (c *Client) connect(ctx context.Context, s *session, tenant, scope string) error {
	result, err := c.requestContext(ctx, s, http.MethodPost, "/api/v1/realtime/ticket", tenant, []byte("{}"), "application/json")
	if err != nil {
		return err
	}
	if result.Status == 401 || result.Status == 403 {
		return errDenied
	}
	if result.Status != 200 {
		return errors.New("申请消息连接失败")
	}
	var ticket struct {
		Data struct {
			URL          string `json:"url"`
			WebsocketURL string `json:"websocket_url"`
		} `json:"data"`
	}
	if json.Unmarshal(result.Body, &ticket) != nil {
		return errors.New("消息票据无效")
	}
	value := ticket.Data.WebsocketURL
	if value == "" {
		value = ticket.Data.URL
	}
	u, err := realtimeURL(s.server, value)
	if err != nil {
		return err
	}
	dialCtx, stop := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPClient: &http.Client{Transport: s.http.Transport, CheckRedirect: s.http.CheckRedirect}})
	stop()
	if err != nil {
		return errors.New("消息连接建立失败")
	}
	conn.SetReadLimit(1024 * 1024)
	s.mu.Lock()
	if ctx.Err() != nil {
		s.mu.Unlock()
		conn.CloseNow()
		return ctx.Err()
	}
	s.conn = conn
	s.ready = false
	s.mu.Unlock()
	defer func() {
		conn.CloseNow()
		s.mu.Lock()
		if s.conn == conn {
			s.conn = nil
			s.ready = false
		}
		s.mu.Unlock()
	}()
	for {
		readCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		_, raw, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			code := websocket.CloseStatus(err)
			if code == 1008 || code == 4401 || code == 4403 {
				return errDenied
			}
			return errors.New("消息连接中断")
		}
		var frame struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		switch frame.Type {
		case "ready":
			s.mu.Lock()
			s.ready = true
			s.rescan = true
			s.mu.Unlock()
			c.setState(ctx, s, "recovering", "正在核对并恢复持久消息")
			signal(s.wake)
		case "event":
			var event struct {
				Conversation string `json:"conversation_id"`
			}
			if json.Unmarshal(frame.Data, &event) == nil {
				s.mu.Lock()
				s.revision++
				if event.Conversation != "" {
					s.dirty[event.Conversation] = s.revision
				} else {
					s.rescan = true
				}
				s.mu.Unlock()
			}
			c.publish(ctx, s, scope, "desktop:event", map[string]any{"scope": scope, "event": json.RawMessage(frame.Data)})
			signal(s.wake)
		case "typing":
			c.publish(ctx, s, scope, "desktop:frame", map[string]any{"scope": scope, "type": frame.Type, "data": json.RawMessage(frame.Data)})
		}
	}
}

// SendFrame 只允许输入状态帧，消息正文必须通过持久发送队列。
func (c *Client) SendFrame(conversation string, typing bool) bool {
	s, err := c.current()
	if err != nil {
		return false
	}
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	raw, _ := json.Marshal(map[string]any{"type": "typing", "conversation_id": conversation, "is_typing": typing})
	return conn.Write(ctx, websocket.MessageText, raw) == nil
}

// maintenance 在界面隐藏或重新加载时继续补拉与重试，失败最多每五秒再次检查。
func (c *Client) maintenance(ctx context.Context, s *session, tenant, scope string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	lastScan := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		rescan := s.rescan
		s.rescan = false
		s.mu.Unlock()
		var syncErr error
		if rescan || time.Since(lastScan) >= 30*time.Second {
			syncErr = c.synchronize(ctx, s, tenant, scope)
			if syncErr == nil {
				lastScan = time.Now()
			}
		}
		if syncErr == nil {
			syncErr = c.syncDirty(ctx, s, tenant, scope)
		}
		if err := syncErr; err != nil {
			s.mu.Lock()
			if ctx.Err() == nil {
				s.rescan = true
			}
			s.mu.Unlock()
			if errors.Is(err, errDenied) {
				c.setState(ctx, s, "denied", "身份失效，已暂停消息处理")
				s.mu.Lock()
				if ctx.Err() == nil && s.runCancel != nil {
					s.runCancel()
				}
				s.mu.Unlock()
				return
			}
			c.setState(ctx, s, "recovering", "持久消息尚未核对完成，正在重试")
		} else {
			s.mu.Lock()
			connected := s.conn != nil && s.ready
			s.mu.Unlock()
			if connected {
				c.setState(ctx, s, "connected", "消息连接和持久数据已同步")
			}
		}
		_ = c.flush(ctx, s, tenant, scope)
		c.remind(ctx, s, tenant)
		// 合并短时间大量事件；网络失败不能因为自身唤醒而形成请求风暴。
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		select {
		case <-s.wake:
		default:
		}
	}
}
