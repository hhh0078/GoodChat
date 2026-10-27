// 文件作用说明：使用一次性票据建立WebSocket，实时推送可靠事件与短期输入状态。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// liveClient 保存本进程实时连接的授权身份和有界发送队列。
type liveClient struct {
	actor      actor
	send       chan []byte
	cancel     context.CancelFunc
	lastTyping map[string]time.Time
}

// realtimeTicket 建立最短30秒有效且只能消费一次的实时凭证。
func (s *Service) realtimeTicket(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	var input struct {
		ConversationID string `json:"conversation_id"`
	}
	if r.ContentLength != 0 {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
	}
	value, err := platform.Token()
	if err != nil {
		replyError(w, r, err)
		return
	}
	ticket := "ws_" + value
	err = platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if a.ContactID != "" {
			if err := blocked(r.Context(), tx, a.ContactID); err != nil {
				return err
			}
		} else {
			if err := liveMember(r.Context(), tx, a); err != nil {
				return err
			}
		}
		if input.ConversationID != "" {
			if err := canReadConversation(r.Context(), tx, a, input.ConversationID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), "INSERT INTO chat_ws_tickets(tenant_id,token_hash,contact_id,membership_id) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid)", a.TenantID, hash(ticket), a.ContactID, a.MemberID)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"ticket": ticket, "url": "/ws/v1?ticket=" + url.QueryEscape(ticket), "expires_in": 30})
}

// consumeTicket 原子消费实时票据并重新检查成员或访客的当前授权。
func (s *Service) consumeTicket(ctx context.Context, ticket string) (actor, error) {
	var a actor
	var tenant *string
	if err := s.pool.QueryRow(ctx, "SELECT chat_ticket_tenant($1)::text", hash(ticket)).Scan(&tenant); err != nil {
		return a, err
	}
	if tenant == nil {
		return a, platform.Fail(401, "ticket_expired", "实时连接凭证失效，请重新连接")
	}
	a.TenantID = *tenant
	err := platform.TenantTx(ctx, s.pool, *tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "UPDATE chat_ws_tickets SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL AND expires_at>now() RETURNING COALESCE(contact_id::text,''),COALESCE(membership_id::text,'')", hash(ticket)).Scan(&a.ContactID, &a.MemberID); err != nil {
			return err
		}
		if a.ContactID != "" {
			return blocked(ctx, tx, a.ContactID)
		}
		if err := tx.QueryRow(ctx, "SELECT user_id::text,role,can_serve FROM tenant_memberships WHERE id=$1 AND tenant_id=$2 AND NOT disabled", a.MemberID, a.TenantID).Scan(&a.UserID, &a.Role, &a.CanServe); err != nil {
			return err
		}
		return liveMember(ctx, tx, a)
	})
	return a, err
}

// websocket 接受已经消费的票据，并把慢连接断开交由持久游标恢复。
func (s *Service) websocket(w http.ResponseWriter, r *http.Request) {
	a, err := s.consumeTicket(r.Context(), r.URL.Query().Get("ticket"))
	if err != nil {
		replyError(w, r, err)
		return
	}
	origins := []string{}
	if u, e := url.Parse(s.cfg.PublicURL); e == nil && u.Host != "" {
		origins = append(origins, u.Host)
	}
	if !s.cfg.Production {
		origins = append(origins, "localhost:3000", "127.0.0.1:3000", "localhost:13000", "127.0.0.1:13000")
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: origins})
	if err != nil {
		return
	}
	conn.SetReadLimit(4096)
	ctx, cancel := context.WithCancel(r.Context())
	client := &liveClient{actor: a, send: make(chan []byte, 128), cancel: cancel, lastTyping: map[string]time.Time{}}
	s.mu.Lock()
	s.clients[client] = struct{}{}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "连接结束")
	}()
	client.send <- jsonValue(map[string]any{"type": "ready", "data": map[string]any{"protocol_version": 1, "heartbeat_seconds": 15}})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case value := <-client.send:
				writeCtx, stop := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, value)
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var frame struct {
			Type           string `json:"type"`
			ConversationID string `json:"conversation_id"`
			Typing         bool   `json:"is_typing"`
		}
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		switch frame.Type {
		case "heartbeat":
			if err := s.heartbeat(ctx, a); err != nil {
				return
			}
			select {
			case client.send <- jsonValue(map[string]any{"type": "heartbeat", "data": map[string]any{"time": nowUTC()}}):
			default:
				return
			}
		case "typing":
			if time.Since(client.lastTyping[frame.ConversationID]) < time.Second && frame.Typing {
				continue
			}
			client.lastTyping[frame.ConversationID] = time.Now()
			_ = s.pushTyping(ctx, a, frame.ConversationID, frame.Typing)
		case "subscribe":
			_ = platform.TenantTx(ctx, s.pool, a.TenantID, func(tx pgx.Tx) error { return canReadConversation(ctx, tx, a, frame.ConversationID) })
		}
	}
}

// heartbeat 更新在线租约，停用或身份角色变化后拒绝继续维持连接。
func (s *Service) heartbeat(ctx context.Context, a actor) error {
	return platform.TenantTx(ctx, s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := tenantActive(ctx, tx, a.TenantID); err != nil {
			return err
		}
		if a.ContactID != "" {
			return blocked(ctx, tx, a.ContactID)
		}
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if a.CanServe {
			_, err := tx.Exec(ctx, "UPDATE agent_runtime SET online_until=now()+interval '45 seconds',updated_at=now() WHERE membership_id=$1", a.MemberID)
			return err
		}
		return nil
	})
}

// typing 处理HTTP输入状态，不保存草稿正文或增加未读。
func (s *Service) typing(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Typing bool `json:"is_typing"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		replyError(w, r, err)
		return
	}
	if err := s.pushTyping(r.Context(), currentActor(r), r.PathValue("id"), input.Typing); err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"expires_in": 5})
}

// pushTyping 在每次发送前检查当前咨询权限，并只发布短期布尔状态。
func (s *Service) pushTyping(ctx context.Context, a actor, id string, on bool) error {
	err := platform.TenantTx(ctx, s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := canReadConversation(ctx, tx, a, id); err != nil {
			return err
		}
		var status, agent string
		if err := tx.QueryRow(ctx, "SELECT status,COALESCE(current_agent_id::text,'') FROM conversations WHERE id=$1", id).Scan(&status, &agent); err != nil {
			return err
		}
		if status == "closed" || a.ContactID == "" && agent != a.MemberID {
			return platform.Fail(403, "typing_not_allowed", "当前不能发送输入状态")
		}
		return nil
	})
	if err != nil {
		return err
	}
	data := map[string]any{"conversation_id": id, "participant_id": actorID(a), "participant_kind": actorKind(a), "is_typing": on, "expires_at": time.Now().Add(5 * time.Second).UTC().Format(time.RFC3339Nano)}
	s.broadcast(ctx, a.TenantID, id, "public", map[string]any{"type": "typing", "data": data})
	return nil
}

// broadcast 只向当前仍获授权的连接投递，连接溢出后依赖数据库补拉。
func (s *Service) broadcast(ctx context.Context, tenant, conversation, audience string, value map[string]any) {
	s.mu.Lock()
	clients := []*liveClient{}
	for c := range s.clients {
		if c.actor.TenantID == tenant && (audience != "internal" || c.actor.ContactID == "") {
			clients = append(clients, c)
		}
	}
	s.mu.Unlock()
	raw := jsonValue(value)
	for _, c := range clients {
		err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error { return canReadConversation(ctx, tx, c.actor, conversation) })
		if err != nil {
			continue
		}
		select {
		case c.send <- raw:
		default:
			c.cancel()
		}
	}
}
