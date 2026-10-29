// 文件作用说明：通过实际HTTP鉴权和一次票据建立真实WS，记录延迟与事件，不保存凭证明文到统计输出。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type apiClient struct {
	base, tenant string
	http         *http.Client
	metrics      *measurements
}
type identity struct{ cookie, token string }
type apiFailure struct {
	Status int
	Code   string
}

// Error 只显示HTTP状态与业务错误码，不包含请求URL、正文或票据。
func (e apiFailure) Error() string { return fmt.Sprintf("http_%d_%s", e.Status, e.Code) }

type peer struct {
	auth         identity
	conversation string
	mu           sync.Mutex
	conn         *websocket.Conn
	events       map[string]int
	arrivals     map[string]time.Time
	unexpected   int
	connected    bool
}

// request 只向指定本机接口发送真实身份，读取统一响应并记录成功、失败与延迟。
func (a *apiClient) request(ctx context.Context, method, path string, who identity, body any, result any) (requestErr error) {
	start, status := time.Now(), 0
	defer func() { a.metrics.record("http", time.Since(start), requestErr == nil, status) }()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, e := http.NewRequestWithContext(ctx, method, a.base+"/api/v1"+path, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("Origin", a.base)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", a.tenant)
	if who.cookie != "" {
		req.Header.Set("Cookie", "support_session="+who.cookie)
	}
	if who.token != "" {
		req.Header.Set("Authorization", "Bearer "+who.token)
	}
	resp, e := a.http.Do(req)
	if e != nil {
		return e
	}
	status = resp.StatusCode
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if e != nil {
		return e
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
		Code string          `json:"code"`
	}
	if e = json.Unmarshal(data, &envelope); e != nil {
		return e
	}
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !ok {
		return apiFailure{resp.StatusCode, envelope.Code}
	}
	if result != nil {
		return json.Unmarshal(envelope.Data, result)
	}
	return nil
}

// connect 每次重新申请票据并等待服务端ready，读循环持续处理真实控制帧和可靠事件。
func (a *apiClient) connect(ctx context.Context, p *peer) (string, error) {
	var ticket struct {
		URL string `json:"url"`
	}
	start := time.Now()
	if e := a.request(ctx, "POST", "/realtime/ticket", p.auth, map[string]any{}, &ticket); e != nil {
		return "", e
	}
	u, e := url.Parse(a.base + ticket.URL)
	if e != nil {
		return "", e
	}
	u.Scheme = "ws"
	conn, _, e := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{a.base}}})
	if e != nil {
		a.metrics.record("connect", time.Since(start), false, 0)
		return "", e
	}
	p.mu.Lock()
	p.conn = conn
	p.connected = true
	p.mu.Unlock()
	ready := make(chan struct{}, 1)
	go func() {
		defer func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.conn == conn {
				p.connected = false
				if ctx.Err() == nil {
					p.unexpected++
				}
			}
		}()
		for {
			_, b, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var frame struct {
				Type string `json:"type"`
				Data struct {
					Type    string `json:"event_type"`
					Payload struct {
						ClientID string `json:"client_message_id"`
					} `json:"payload"`
				} `json:"data"`
			}
			if json.Unmarshal(b, &frame) != nil {
				continue
			}
			if frame.Type == "ready" {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
			if frame.Type == "event" && frame.Data.Type == "message.created" && frame.Data.Payload.ClientID != "" {
				p.mu.Lock()
				key := frame.Data.Payload.ClientID
				p.events[key]++
				if _, ok := p.arrivals[key]; !ok {
					p.arrivals[key] = time.Now()
				}
				p.mu.Unlock()
			}
		}
	}()
	select {
	case <-ready:
		a.metrics.record("connect", time.Since(start), true, 101)
		return ticket.URL, nil
	case <-ctx.Done():
		conn.CloseNow()
		return "", ctx.Err()
	case <-time.After(10 * time.Second):
		conn.CloseNow()
		return "", fmt.Errorf("ready_timeout")
	}
}

// closePeer 只关闭负载生成器自身socket，重新连接仍复用同一接收去重集合。
func closePeer(p *peer) {
	p.mu.Lock()
	conn := p.conn
	p.conn = nil
	p.connected = false
	p.mu.Unlock()
	if conn != nil {
		_ = conn.CloseNow()
	}
}

// peerStates 读取仍在持续Read和响应控制帧的活连接数，不把曾建立过的socket当并发数。
func peerStates(peers ...[]*peer) (int, int) {
	live, unexpected := 0, 0
	for _, group := range peers {
		for _, p := range group {
			p.mu.Lock()
			if p.connected {
				live++
			}
			unexpected += p.unexpected
			p.mu.Unlock()
		}
	}
	return live, unexpected
}

// newPeer 为一个真实参与者维护独立接收状态，不用多个标签冒充更多在线身份。
func newPeer(who identity, conversation string) *peer {
	return &peer{auth: who, conversation: conversation, events: map[string]int{}, arrivals: map[string]time.Time{}}
}
