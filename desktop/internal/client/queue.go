// 文件作用说明：先持久化消息再发送，网络失败重试原编号，权限或业务拒绝停止自动发送。
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// sendMessage 在原生层落盘后才执行网络请求，响应丢失时使用原幂等编号。
func (c *Client) sendMessage(ctx context.Context, s *session, w http.ResponseWriter, r *http.Request, body []byte) {
	var value struct {
		ID string `json:"client_message_id"`
	}
	if json.Unmarshal(body, &value) != nil || len(value.ID) != 36 || len(body) > 128*1024 {
		localError(w, 400, "invalid_message", "消息编号或内容无效")
		return
	}
	s.mu.Lock()
	scope, tenant := s.scope, s.tenant
	s.mu.Unlock()
	if scope == "" || tenant != r.Header.Get("X-Tenant-ID") {
		localError(w, 401, "desktop_identity", "请重新登录并选择工作空间")
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	item := Pending{ID: value.ID, Conversation: parts[4], Body: body}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	if err := c.store.enqueue(scope, item); err != nil {
		localError(w, 500, "desktop_storage", err.Error())
		return
	}
	result, err := c.deliver(ctx, s, tenant, scope, item)
	if err != nil {
		localError(w, 503, "desktop_queued", "消息已保存在本机，连接恢复后会自动核对并重试原消息，请勿换编号重复发送")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.Body)
}

// deliver 只有服务端成功确认才删除队列；确定性失败留给用户核对与手动重试。
func (c *Client) deliver(ctx context.Context, s *session, tenant, scope string, item Pending) (reply, error) {
	result, err := c.requestContext(ctx, s, http.MethodPost, "/api/v1/conversations/"+item.Conversation+"/messages", tenant, item.Body, "application/json")
	if err == nil && result.Status >= 200 && result.Status < 300 {
		var value struct {
			Data struct {
				ID       string `json:"id"`
				ClientID string `json:"client_message_id"`
			} `json:"data"`
		}
		if json.Unmarshal(result.Body, &value) != nil || value.Data.ID == "" || value.Data.ClientID != item.ID {
			return reply{}, errors.New("服务端发送确认不完整")
		}
		if _, err = c.store.db.Exec("DELETE FROM pending WHERE scope=? AND id=?", scope, item.ID); err != nil {
			return reply{}, err
		}
		c.publish(ctx, s, scope, "desktop:sent", map[string]string{"scope": scope, "id": item.ID, "conversation_id": item.Conversation})
		c.publish(ctx, s, scope, "desktop:state", c.Status())
		return result, nil
	}
	if err == nil && result.Status >= 400 && result.Status < 500 && result.Status != 408 && result.Status != 429 {
		var value struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(result.Body, &value)
		if value.Message == "" {
			value.Message = "服务器拒绝此消息，请核对权限或会话状态"
		}
		if _, e := c.store.db.Exec("UPDATE pending SET state='failed',error=? WHERE scope=? AND id=?", value.Message, scope, item.ID); e != nil {
			return reply{}, e
		}
		c.publish(ctx, s, scope, "desktop:state", c.Status())
		return result, nil
	}
	signal(s.wake)
	return reply{}, errors.New("等待原消息核验")
}

// flush 串行重试当前已认证分区，其他账号或服务器的消息永远不会被领取。
func (c *Client) flush(ctx context.Context, s *session, tenant, scope string) error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	items, err := c.store.listPending(scope)
	if err != nil {
		return err
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if item.State != "pending" {
			continue
		}
		result, err := c.deliver(ctx, s, tenant, scope, item)
		if err != nil {
			return err
		}
		if result.Status == 401 || result.Status == 403 {
			return errors.New("登录或权限已失效，已暂停自动发送")
		}
	}
	return nil
}

// Retry 只在当前身份内重新启用原消息，不能修改原内容或客户端编号。
func (c *Client) Retry(id string) error {
	s, err := c.current()
	if err != nil {
		return err
	}
	s.mu.Lock()
	scope := s.scope
	s.mu.Unlock()
	if scope == "" {
		return errors.New("请先登录")
	}
	_, err = c.store.db.Exec("UPDATE pending SET state='pending',error='' WHERE scope=? AND id=?", scope, id)
	signal(s.wake)
	return err
}
