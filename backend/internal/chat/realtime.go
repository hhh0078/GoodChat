// 文件作用说明：使用一次性票据建立WebSocket，实时推送可靠事件与短期输入状态。
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
	"supportdesk/backend/internal/presence"
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
			if err := blocked(ctx, tx, a.ContactID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "UPDATE contacts SET online_until=now()+interval '45 seconds' WHERE id=$1", a.ContactID)
			return err
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
	connectionID := newID()
	ended := make(chan realtimeClose, 1)
	// finish 先记录安全关闭原因再取消所有连接任务，避免旧任务继续续租。
	finish := func(code websocket.StatusCode, reason string) {
		select {
		case ended <- realtimeClose{code, reason}:
		default:
		}
		cancel()
	}
	client := &liveClient{actor: a, send: make(chan []byte, 128), cancel: func() { finish(websocket.StatusTryAgainLater, "slow_consumer") }, lastTyping: map[string]time.Time{}}
	s.mu.Lock()
	s.clients[client] = struct{}{}
	s.mu.Unlock()
	slog.Info("realtime_connection", "connection_id", connectionID, "request_id", r.Header.Get("X-Request-ID"), "tenant_id", a.TenantID, "actor_kind", actorKind(a), "phase", "opened")
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
		result := realtimeClose{websocket.StatusNormalClosure, "connection_ended"}
		select {
		case result = <-ended:
		default:
		}
		slog.Info("realtime_connection", "connection_id", connectionID, "phase", "closed", "close_code", int(result.code), "reason", result.reason)
		_ = conn.Close(result.code, result.reason)
	}()
	if err := s.heartbeat(ctx, a); err != nil {
		finish(realtimeErrorCode(err), "initial_authorization_failed")
		return
	}
	go keepRealtimeAlive(ctx, conn, 15*time.Second, 10*time.Second, func(renewCtx context.Context) error { return s.heartbeat(renewCtx, a) }, func(err error) {
		finish(realtimeErrorCode(err), "probe_failed")
	})
	client.send <- jsonValue(map[string]any{"type": "ready", "data": map[string]any{"protocol_version": 1, "heartbeat_seconds": 15, "connection_id": connectionID, "request_id": r.Header.Get("X-Request-ID")}})
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
					finish(websocket.StatusInternalError, "write_failed")
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
				finish(realtimeErrorCode(err), "heartbeat_authorization_failed")
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
			if err := blocked(ctx, tx, a.ContactID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "UPDATE contacts SET online_until=now()+interval '45 seconds' WHERE id=$1", a.ContactID)
			return err
		}
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if err := presence.Renew(ctx, tx, a.TenantID, a.MemberID); err != nil {
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
	for _, c := range clients {
		outgoing := value
		err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
			if err := canReadConversation(ctx, tx, c.actor, conversation); err != nil {
				return err
			}
			if value["type"] != "event" {
				return nil
			}
			data, ok := value["data"].(map[string]any)
			if !ok {
				return platform.Fail(404, "event_not_visible", "事件不存在")
			}
			id, ok := data["id"].(string)
			if !ok || !uuidPattern.MatchString(id) {
				return platform.Fail(404, "event_not_visible", "事件不存在")
			}
			// 出站任务认领后可能已撤回消息，发送前重读当前事件，不能重放认领时的旧正文副本。
			event, err := txJSON(ctx, tx, "SELECT to_jsonb(e)||jsonb_build_object('seq',e.seq::text,'protocol_version',1) FROM conversation_events e WHERE e.id=$1 AND e.conversation_id=$2 AND chat_event_visible(NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,e.id)", id, conversation, c.actor.MemberID, c.actor.ContactID, isAdmin(c.actor))
			if err != nil {
				return err
			}
			// 每个接收身份单独复制并裁剪引用，不能修改下一位接收者共享的原始事件。
			outgoing = decodeJSONMap(jsonValue(value))
			outgoing["data"] = event
			copyData := event
			payload, _ := copyData["payload"].(map[string]any)
			if payload != nil {
				return redactMessageReplies(ctx, tx, c.actor, []map[string]any{payload})
			}
			return nil
		})
		if err != nil {
			continue
		}
		select {
		case c.send <- jsonValue(outgoing):
		default:
			c.cancel()
		}
	}
}

// realtimeClose 保存不会泄露票据或消息内容的连接关闭原因。
type realtimeClose struct {
	code   websocket.StatusCode
	reason string
}

// realtimeErrorCode 将身份撤销与网络故障区分，身份失效不应由客户端无限重连。
func realtimeErrorCode(err error) websocket.StatusCode {
	var apiError *platform.APIError
	if errors.As(err, &apiError) && (apiError.Status == 401 || apiError.Status == 403 || apiError.Status == 404) {
		return websocket.StatusPolicyViolation
	}
	return websocket.StatusInternalError
}
