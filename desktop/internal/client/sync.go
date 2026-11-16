// 文件作用说明：从服务端授权快照与签名事件游标恢复消息，原子提交后才推进本机水位。
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// syncDirty 只补拉收到变更提示的咨询，处理中再次到达的提示保留到下一轮。
func (c *Client) syncDirty(ctx context.Context, s *session, tenant, scope string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.mu.Lock()
	dirty := make(map[string]uint64, len(s.dirty))
	for id, revision := range s.dirty {
		dirty[id] = revision
	}
	s.mu.Unlock()
	for id, revision := range dirty {
		if err := c.syncConversation(ctx, s, tenant, scope, id); err != nil {
			return err
		}
		s.mu.Lock()
		if ctx.Err() == nil && s.scope == scope && s.dirty[id] == revision {
			delete(s.dirty, id)
		}
		s.mu.Unlock()
	}
	if len(dirty) > 0 {
		c.publish(ctx, s, scope, "desktop:sync", map[string]string{"scope": scope})
	}
	return nil
}

// synchronize 扫描当前客服可见的所有非结束咨询，再补齐其持久事件。
func (c *Client) synchronize(ctx context.Context, s *session, tenant, scope string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	for _, status := range []string{"queued", "assigned", "active"} {
		cursor := ""
		for {
			path := "/api/v1/agent/conversations?limit=100&status=" + status + "&cursor=" + url.QueryEscape(cursor)
			result, err := c.requestContext(ctx, s, http.MethodGet, path, tenant, nil, "")
			if err != nil {
				return err
			}
			if result.Status == 401 || result.Status == 403 {
				return errDenied
			}
			if result.Status != 200 {
				return errors.New("读取咨询列表失败")
			}
			var list struct {
				Data struct {
					Items []struct {
						ID string `json:"id"`
					} `json:"items"`
					More bool   `json:"has_more"`
					Next string `json:"next_cursor"`
				} `json:"data"`
			}
			if json.Unmarshal(result.Body, &list) != nil {
				return errors.New("咨询列表格式无效")
			}
			for _, item := range list.Data.Items {
				if item.ID == "" {
					continue
				}
				if err = c.syncConversation(ctx, s, tenant, scope, item.ID); err != nil {
					return err
				}
			}
			if !list.Data.More {
				break
			}
			if list.Data.Next == "" || list.Data.Next == cursor {
				return errors.New("咨询列表恢复位置无效")
			}
			cursor = list.Data.Next
		}
	}
	c.publish(ctx, s, scope, "desktop:sync", map[string]string{"scope": scope})
	return nil
}

// syncConversation 遇到游标失效时重建授权快照，禁止以最大已见序号猜测水位。
func (c *Client) syncConversation(ctx context.Context, s *session, tenant, scope, id string) error {
	cursor, err := c.store.cursor(scope, id)
	if err != nil {
		return err
	}
	resets := 0
	for ctx.Err() == nil {
		reset := cursor == ""
		path := "/api/v1/conversations/" + url.PathEscape(id) + "/events?after_cursor=" + url.QueryEscape(cursor)
		if reset {
			path = "/api/v1/conversations/" + url.PathEscape(id) + "/messages?limit=50"
		}
		result, err := c.requestContext(ctx, s, http.MethodGet, path, tenant, nil, "")
		if err != nil {
			return err
		}
		if result.Status == 401 {
			return errDenied
		}
		if result.Status == 403 || result.Status == 404 {
			return c.store.forgetConversation(scope, id)
		}
		var batch struct {
			Code string `json:"code"`
			Data struct {
				Items    []json.RawMessage `json:"items"`
				Next     string            `json:"next_cursor"`
				Snapshot string            `json:"sync_cursor"`
				More     bool              `json:"has_more"`
			} `json:"data"`
		}
		if json.Unmarshal(result.Body, &batch) != nil {
			return errors.New("消息恢复数据无效")
		}
		if batch.Code == "resync_required" && resets == 0 {
			cursor = ""
			resets++
			continue
		}
		if result.Status != 200 {
			return errors.New("消息补拉未完成")
		}
		next := batch.Data.Next
		if reset {
			next = batch.Data.Snapshot
		}
		if next == "" || (!reset && batch.Data.More && next == cursor) {
			return errors.New("消息恢复游标未推进")
		}
		if err = c.store.checkpoint(scope, id, next, batch.Data.Items, reset); err != nil {
			return err
		}
		cursor = next
		if !reset && !batch.Data.More {
			return nil
		}
	}
	return ctx.Err()
}

// remind 使用服务端提醒偏好和原子领取，后台窗口不再依赖网页通知或音频时钟。
func (c *Client) remind(ctx context.Context, s *session, tenant string) {
	c.mu.Lock()
	background, notify, allowed := c.background, c.notify, c.notificationsAllowed
	c.mu.Unlock()
	if !background || notify == nil {
		return
	}
	result, err := c.requestContext(ctx, s, http.MethodGet, "/api/v1/tenant/notification-preferences", tenant, nil, "")
	if err != nil || result.Status != 200 {
		return
	}
	var prefs struct {
		Data struct {
			System bool    `json:"system_enabled"`
			Sound  bool    `json:"sound_enabled"`
			Muted  bool    `json:"muted"`
			Volume float64 `json:"volume"`
		} `json:"data"`
	}
	if json.Unmarshal(result.Body, &prefs) != nil || prefs.Data.Muted {
		return
	}
	system := prefs.Data.System && allowed != nil && allowed()
	volume := prefs.Data.Volume
	if !prefs.Data.Sound {
		volume = 0
	}
	if !system && volume <= 0 {
		return
	}
	result, err = c.requestContext(ctx, s, http.MethodPost, "/api/v1/agent/attention-reminders/claim", tenant, []byte(`{"initial":true}`), "application/json")
	if err != nil || result.Status != 200 {
		return
	}
	var claimed struct {
		Data struct {
			Items []json.RawMessage `json:"items"`
			Sound string            `json:"notification_sound_id"`
		} `json:"data"`
	}
	if json.Unmarshal(result.Body, &claimed) != nil || len(claimed.Data.Items) == 0 {
		return
	}
	// 通知不含聊天正文，锁屏或共享屏幕时不泄露客户内容。
	if err = notify("有咨询尚未查看，请回到GoodChat工作台处理。", system, claimed.Data.Sound, volume); err != nil {
		c.emit("desktop:notification-error", "系统通知未送达，请检查系统通知权限")
	}
}
