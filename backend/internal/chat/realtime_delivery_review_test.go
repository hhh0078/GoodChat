// 文件作用说明：使用真实PostgreSQL、HTTP与WebSocket验收初始续租阻塞、满队列恢复、响应丢失重试、并发出站及跨重启恢复游标。
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// drainReviewClients 释放故障版启动时可能塞满的队列，确保红测不会把httptest清理永久挂住。
func drainReviewClients(s *Service) {
	s.mu.Lock()
	clients := []*liveClient{}
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		for {
			select {
			case <-c.send:
				continue
			default:
				goto next
			}
		}
	next:
	}
}

// TestRealtimeDeliveryReadyBeforeBlockedRenewBacklog 验证初始续租被行锁阻塞期间128个真实事件不能让ready在writer启动前死锁。
func TestRealtimeDeliveryReadyBeforeBlockedRenewBacklog(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	snapshot := mustData(t, status, reply)
	lock, err := f.admin.Begin(t.Context())
	if err != nil {
		t.Fatal("无法建立隔离行锁")
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(t.Context(), "SELECT membership_id FROM agent_runtime WHERE membership_id=$1 FOR UPDATE", f.agent); err != nil {
		t.Fatal("无法锁定初始续租行")
	}
	status, reply = f.request("POST", "/api/v1/realtime/ticket", f.agent, "", map[string]any{})
	ticket := mustData(t, status, reply)
	dialCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(f.server.URL, "http")+ticket["url"].(string), nil)
	cancel()
	if err != nil {
		t.Fatal("真实WS握手失败")
	}
	defer func() { conn.CloseNow(); drainReviewClients(f.s) }()
	const messages = 126
	for i := 0; i < messages; i++ {
		status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": fmt.Sprintf("隔离WS恢复消息%d", i)})
		mustData(t, status, reply)
	}
	if f.countRows("SELECT count(*) FROM outbox_jobs WHERE completed_at IS NULL") != 128 {
		t.Fatal("初始队列必须精确包含128个真实持久事件")
	}
	for i := 0; i < 2; i++ {
		if err := f.s.deliverOutbox(t.Context(), f.tenant); err != nil {
			t.Fatal("真实出站投递失败")
		}
	}
	f.s.mu.Lock()
	queued := 0
	for client := range f.s.clients {
		queued += len(client.send)
	}
	f.s.mu.Unlock()
	t.Logf("initial heartbeat blocked; pre-ready queued frames=%d", queued)
	if err := lock.Commit(t.Context()); err != nil {
		t.Fatal("释放初始续租行失败")
	}
	readCtx, stop := context.WithTimeout(t.Context(), 2*time.Second)
	_, raw, err := conn.Read(readCtx)
	stop()
	if err != nil {
		drainReviewClients(f.s)
		t.Fatal("初始续租恢复后未收到ready，满队列阻塞了writer启动")
	}
	var frame map[string]any
	if json.Unmarshal(raw, &frame) != nil || frame["type"] != "ready" {
		t.Fatal("ready必须先于任何持久事件")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/events?after_cursor="+url.QueryEscape(snapshot["sync_cursor"].(string)), f.agent, "", nil)
	batch := mustData(t, status, reply)
	count := 0
	for _, raw := range batch["items"].([]any) {
		if raw.(map[string]any)["event_type"] == "message.created" {
			count++
		}
	}
	if count != messages {
		t.Fatalf("握手阻塞期间HTTP补拉遗漏消息: %d/%d", count, messages)
	}
}

// TestRealtimeDeliveryCommittedHTTPResponseIgnored 验证真实HTTP已提交但客户端未读取结果后，原编号重试仍只保留同一消息、事件和出站任务。
func TestRealtimeDeliveryCommittedHTTPResponseIgnored(t *testing.T) {
	f := newFixture(t)
	token, _, conversation := f.visitor("")
	client := newID()
	input := map[string]any{"client_message_id": client, "content": "隔离响应丢失验收正文"}
	request, err := http.NewRequest("POST", f.server.URL+"/api/v1/conversations/"+conversation+"/messages", bytes.NewReader(jsonValue(input)))
	if err != nil {
		t.Fatal("无法构造真实HTTP请求")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal("真实HTTP提交失败")
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		t.Fatal("持久提交没有成功")
	}
	response.Body.Close()
	var id string
	var seq int64
	if err := f.admin.QueryRow(t.Context(), "SELECT id::text,event_seq FROM messages WHERE conversation_id=$1 AND client_message_id=$2", conversation, client).Scan(&id, &seq); err != nil {
		t.Fatal("确认持久消息失败")
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, input)
	restored := mustData(t, status, reply)
	if restored["id"] != id || restored["seq"] != fmt.Sprint(seq) {
		t.Fatal("丢失HTTP结果后原编号重试创建了新消息或新序号")
	}
	if f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND client_message_id=$2", conversation, client) != 1 || f.countRows("SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND event_type='message.created' AND payload->>'id'=$2", conversation, id) != 1 || f.countRows("SELECT count(*) FROM outbox_jobs j JOIN conversation_events e ON e.id=j.event_id WHERE e.conversation_id=$1 AND e.payload->>'id'=$2", conversation, id) != 1 {
		t.Fatal("消息幂等未覆盖事件与出站任务")
	}
	t.Logf("ack ignored then retried: same message id/seq, one message/event/outbox; seq=%d", seq)
}

// TestRealtimeDeliveryConcurrentWorkersAndRestartCursor 验证同进程多goroutine认领不重复、队列满取消慢连接，重启后从同密钥签名游标HTTP补齐全部消息。
func TestRealtimeDeliveryConcurrentWorkersAndRestartCursor(t *testing.T) {
	f := newFixture(t)
	token, contact, conversation := f.visitor("")
	const count = 24
	for i := 0; i < count; i++ {
		status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": fmt.Sprintf("并发恢复消息%d", i)})
		mustData(t, status, reply)
	}
	cfg := platform.Config{AuthSecret: "isolated-delivery-review-signing-secret-12345"}
	service := New(f.pool, testAuthorizer{f.principals}, cfg)
	cancelled := make(chan struct{}, 1)
	slow := &liveClient{actor: actor{TenantID: f.tenant, ContactID: contact}, send: make(chan []byte, 1), cancel: func() {
		select {
		case cancelled <- struct{}{}:
		default:
		}
	}}
	slow.send <- []byte("full")
	fast := &liveClient{actor: slow.actor, send: make(chan []byte, 64), cancel: func() { t.Error("正常接收者不应被慢队列取消") }}
	service.clients[slow] = struct{}{}
	service.clients[fast] = struct{}{}
	group := sync.WaitGroup{}
	problems := make(chan error, 4)
	for i := 0; i < 4; i++ {
		group.Add(1)
		go func() { defer group.Done(); problems <- service.deliverOutbox(t.Context(), f.tenant) }()
	}
	group.Wait()
	close(problems)
	for err := range problems {
		if err != nil {
			t.Fatal("并发真实出站失败")
		}
	}
	if f.countRows("SELECT count(*) FROM outbox_jobs WHERE completed_at IS NULL") != 0 || f.countRows("SELECT count(*) FROM outbox_jobs WHERE attempts<>1") != 0 {
		t.Fatal("并行领取重复执行或遗漏出站任务")
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("慢消费者队列满后未主动取消")
	}
	observed := map[string]bool{}
	for len(fast.send) > 0 {
		var frame map[string]any
		if json.Unmarshal(<-fast.send, &frame) != nil {
			t.Fatal("出站帧格式错误")
		}
		event := frame["data"].(map[string]any)
		if event["event_type"] == "message.created" {
			observed[event["id"].(string)] = true
		}
	}
	if len(observed) != count {
		t.Fatalf("健康接收者遗漏并发出站消息: %d/%d", len(observed), count)
	}
	restarted := New(f.pool, testAuthorizer{f.principals}, cfg)
	identity := slow.actor
	cursor := service.encodeScopedCursor(identity, conversation, 0, "visitor")
	seq, err := restarted.decodeHistoryCursor(identity, conversation, cursor, "visitor")
	if err != nil || seq != 0 {
		t.Fatal("重启后同密钥签名游标无法恢复")
	}
	var rows int
	err = platform.TenantTx(t.Context(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND seq>$2 AND event_type='message.created' AND chat_event_visible(NULL,$3::uuid,false,id)", conversation, seq, contact).Scan(&rows)
	})
	if err != nil || rows != count {
		t.Fatal("慢连接取消后持久事件不可恢复")
	}
	firstMux := http.NewServeMux()
	service.Register(firstMux)
	firstHTTP := httptest.NewServer(firstMux)
	defer firstHTTP.Close()
	before := reviewDeliveryEvents(t, firstHTTP.URL, conversation, token, "")
	if len(before["items"].([]any)) != count+1 {
		t.Fatal("真实HTTP不能补齐队列中未收取的事件")
	}
	firstHTTP.Close()
	for i := 0; i < 2; i++ {
		status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "服务重启期间持久消息"})
		mustData(t, status, reply)
	}
	restartMux := http.NewServeMux()
	restarted.Register(restartMux)
	secondHTTP := httptest.NewServer(restartMux)
	defer secondHTTP.Close()
	after := reviewDeliveryEvents(t, secondHTTP.URL, conversation, token, before["next_cursor"].(string))
	if len(after["items"].([]any)) != 2 {
		t.Fatal("跨真实HTTP服务重启的旧签名游标遗漏期间提交消息")
	}
}

// reviewDeliveryEvents 用真实HTTP服务和访客凭证读取补拉结果，失败不输出令牌或响应正文。
func reviewDeliveryEvents(t *testing.T, base, conversation, token, cursor string) map[string]any {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), "GET", base+"/api/v1/conversations/"+conversation+"/events?after_cursor="+url.QueryEscape(cursor), nil)
	if err != nil {
		t.Fatal("构造跨重启HTTP请求失败")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal("跨重启HTTP请求失败")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("跨重启HTTP状态%d", response.StatusCode)
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if json.NewDecoder(response.Body).Decode(&envelope) != nil {
		t.Fatal("跨重启事件响应格式错误")
	}
	return envelope.Data
}

// TestRealtimeDeliveryInitialRenewDeadline 验证初始续租长时间等待行锁时十秒主动关闭，不泄漏注册且失败事务不能延长在线租约。
func TestRealtimeDeliveryInitialRenewDeadline(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	var before time.Time
	if err := f.admin.QueryRow(t.Context(), "SELECT online_until FROM agent_runtime WHERE membership_id=$1", f.agent).Scan(&before); err != nil {
		t.Fatal("读取初始租约失败")
	}
	lock, err := f.admin.Begin(t.Context())
	if err != nil {
		t.Fatal("建立续租阻塞事务失败")
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(t.Context(), "SELECT membership_id FROM agent_runtime WHERE membership_id=$1 FOR UPDATE", f.agent); err != nil {
		t.Fatal("锁定初始续租行失败")
	}
	status, reply := f.request("POST", "/api/v1/realtime/ticket", f.agent, "", map[string]any{})
	ticket := mustData(t, status, reply)
	dialCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(f.server.URL, "http")+ticket["url"].(string), nil)
	cancel()
	if err != nil {
		t.Fatal("建立真实握手失败")
	}
	defer conn.CloseNow()
	readCtx, stop := context.WithTimeout(t.Context(), 13*time.Second)
	_, _, err = conn.Read(readCtx)
	stop()
	if websocket.CloseStatus(err) != websocket.StatusInternalError {
		t.Fatal("初始续租等待超过时限未由服务端主动关闭")
	}
	f.s.mu.Lock()
	registered := len(f.s.clients)
	f.s.mu.Unlock()
	if registered != 0 {
		t.Fatal("初始续租失败泄漏了连接注册")
	}
	var after time.Time
	if err := f.admin.QueryRow(t.Context(), "SELECT online_until FROM agent_runtime WHERE membership_id=$1", f.agent).Scan(&after); err != nil {
		t.Fatal("读取失败后的租约失败")
	}
	if !after.Equal(before) {
		t.Fatal("初始续租失败仍延长了在线租约")
	}
}

// TestRealtimeDeliveryTypingRejectsUnownedIDs 验证真实访客发送任意非法或无权咨询编号不能累积连接map，只有成功授权的输入状态才留下节流记录。
func TestRealtimeDeliveryTypingRejectsUnownedIDs(t *testing.T) {
	f := newFixture(t)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/realtime/ticket", "", token, map[string]any{})
	ticket := mustData(t, status, reply)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.server.URL, "http")+ticket["url"].(string), nil)
	if err != nil {
		t.Fatal("访客真实WS握手失败")
	}
	defer conn.CloseNow()
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal("访客ready失败")
	}
	f.s.mu.Lock()
	var client *liveClient
	for candidate := range f.s.clients {
		client = candidate
	}
	f.s.mu.Unlock()
	if client == nil {
		t.Fatal("访客连接没有注册")
	}
	for i := 0; i < 32; i++ {
		for _, id := range []string{newID(), fmt.Sprintf("invalid-conversation-%d", i)} {
			if err := conn.Write(ctx, websocket.MessageText, jsonValue(map[string]any{"type": "typing", "conversation_id": id, "is_typing": true})); err != nil {
				t.Fatal("发送隔离非法输入状态失败")
			}
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, jsonValue(map[string]any{"type": "typing", "conversation_id": conversation, "is_typing": true})); err != nil {
		t.Fatal("发送合法输入状态失败")
	}
	if err := conn.Write(ctx, websocket.MessageText, jsonValue(map[string]any{"type": "heartbeat"})); err != nil {
		t.Fatal("发送处理完成屏障失败")
	}
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatal("未收到处理完成屏障")
		}
		var frame map[string]any
		if json.Unmarshal(raw, &frame) == nil && frame["type"] == "heartbeat" {
			break
		}
	}
	conn.CloseNow()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.s.mu.Lock()
		_, registered := f.s.clients[client]
		f.s.mu.Unlock()
		if !registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("测试连接关闭后未释放注册")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 删除注册与检查使用同一把锁，建立服务端退出后的同步边界，避免并发读取仍在写入的map。
	if len(client.lastTyping) != 1 {
		t.Fatalf("非法或未授权编号进入了输入状态节流map: keys=%d", len(client.lastTyping))
	}
	if _, ok := client.lastTyping[conversation]; !ok {
		t.Fatal("合法输入状态没有记录节流位置")
	}
}

// broadcastReviewCounter 只计SQL种类，不记录参数、正文或连接配置，量化不相关访客的事务放大。
type broadcastReviewCounter struct {
	total, transactions, contacts, members, visibility, eventPayload, contactLookup atomic.Int64
	failLookup                                                                      bool
}

// TraceQueryStart 统计真实PostgreSQL查询，故障场景仅取消早筛子查询而不取消广播父上下文。
func (c *broadcastReviewCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.total.Add(1)
	sql := strings.TrimSpace(data.SQL)
	if strings.EqualFold(sql, "begin") {
		c.transactions.Add(1)
	}
	if strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM block_rules") {
		c.contacts.Add(1)
	}
	if strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM tenant_memberships m JOIN tenants") {
		c.members.Add(1)
	}
	if strings.HasPrefix(sql, "SELECT chat_conversation_visible(") {
		c.visibility.Add(1)
	}
	if strings.HasPrefix(sql, "SELECT to_jsonb(e)||jsonb_build_object(") {
		c.eventPayload.Add(1)
	}
	if strings.HasPrefix(sql, "SELECT contact_id::text FROM conversations WHERE") {
		c.contactLookup.Add(1)
		if c.failLookup {
			failed, cancel := context.WithCancel(ctx)
			cancel()
			return failed
		}
	}
	return ctx
}

// TraceQueryEnd 不输出底层错误，测试仅通过收件结果和计数核对早筛失败回退。
func (c *broadcastReviewCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// snapshot 返回无敏感内容的计数证据。
func (c *broadcastReviewCounter) snapshot() map[string]int64 {
	return map[string]int64{"total_queries": c.total.Load(), "tenant_transactions": c.transactions.Load(), "visitor_live_checks": c.contacts.Load(), "member_live_checks": c.members.Load(), "conversation_visibility_checks": c.visibility.Load(), "current_event_payload_reads": c.eventPayload.Load(), "contact_lookups": c.contactLookup.Load()}
}

// reviewBroadcastMember 为统计模型创建不在当前接待中的真实普通客服，保留完整组成员资格。
func reviewBroadcastMember(t *testing.T, f *fixture) actor {
	t.Helper()
	user, member := newID(), newID()
	if _, err := f.admin.Exec(t.Context(), "INSERT INTO users(id,email,display_name,password_hash) VALUES($1,$2,'隔离广播计数成员','test-only-unusable-password')", user, "broadcast-"+user+"@example.invalid"); err != nil {
		t.Fatal("计数成员初始化失败")
	}
	if _, err := f.admin.Exec(t.Context(), "INSERT INTO tenant_memberships(id,tenant_id,user_id,role,can_serve) VALUES($1,$2,$3,'agent',true)", member, f.tenant, user); err != nil {
		t.Fatal("计数成员关系初始化失败")
	}
	if err := platform.TenantTx(t.Context(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "SELECT ensure_chat_defaults($1::uuid,$2::uuid)", f.tenant, user)
		return err
	}); err != nil {
		t.Fatal("计数成员组初始化失败")
	}
	return actor{TenantID: f.tenant, MemberID: member, UserID: user, Role: "agent", CanServe: true}
}

// TestRealtimeBroadcastVisitorPrefilterQueryCount 用真PG计数50连接模型，保持所有客服和目标访客的完整权限检查。
func TestRealtimeBroadcastVisitorPrefilterQueryCount(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, contact, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	message := historySend(t, f, conversation, "", token, "广播计数隔离正文")
	var event map[string]any
	if err := f.admin.QueryRow(t.Context(), "SELECT to_jsonb(e) FROM conversation_events e WHERE conversation_id=$1 AND event_type='message.created' AND payload->>'id'=$2", conversation, message["id"]).Scan(&event); err != nil {
		t.Fatal("读取计数持久事件失败")
	}
	actors := []actor{{TenantID: f.tenant, ContactID: contact}}
	for i := 0; i < 44; i++ {
		id := newID()
		if _, err := f.admin.Exec(t.Context(), "INSERT INTO contacts(id,tenant_id,display_name) VALUES($1,$2,'隔离广播计数访客')", id, f.tenant); err != nil {
			t.Fatal("计数访客初始化失败")
		}
		actors = append(actors, actor{TenantID: f.tenant, ContactID: id})
	}
	for _, member := range []string{f.owner, f.agent} {
		p := f.principals[member]
		actors = append(actors, actor{TenantID: f.tenant, MemberID: member, UserID: p.UserID, Role: p.Role, CanServe: p.CanServe})
	}
	for i := 0; i < 3; i++ {
		actors = append(actors, reviewBroadcastMember(t, f))
	}
	for _, failLookup := range []bool{false, true} {
		t.Run(fmt.Sprintf("lookup_failure_%t", failLookup), func(t *testing.T) {
			counter := &broadcastReviewCounter{failLookup: failLookup}
			config := f.pool.Config().Copy()
			config.ConnConfig.Tracer = counter
			pool, err := pgxpool.NewWithConfig(t.Context(), config)
			if err != nil {
				t.Fatal("创建只计数连接池失败")
			}
			defer pool.Close()
			service := New(pool, testAuthorizer{f.principals}, platform.Config{})
			clients := []*liveClient{}
			for _, identity := range actors {
				client := &liveClient{actor: identity, send: make(chan []byte, 2), cancel: func() { t.Error("计数模型不应满队列") }}
				service.clients[client] = struct{}{}
				clients = append(clients, client)
			}
			service.broadcast(t.Context(), f.tenant, conversation, "public", map[string]any{"type": "event", "data": event})
			counts := counter.snapshot()
			raw, _ := json.Marshal(counts)
			t.Logf("50 connections (45 visitors, 5 agents), query counts: %s", raw)
			if len(clients[0].send) != 1 {
				t.Fatal("目标访客没有收到真实消息")
			}
			for _, client := range clients[1:45] {
				if len(client.send) != 0 {
					t.Fatal("无关访客收到了私有咨询消息")
				}
			}
			if counts["member_live_checks"] != 5 {
				t.Fatal("早筛绕过了客服当前资格复核")
			}
			if counts["current_event_payload_reads"] != 3 {
				t.Fatal("目标访客和两个合法客服必须重读当前事件以保留撤回脱敏")
			}
			if !failLookup {
				if counts["visitor_live_checks"] != 1 || counts["conversation_visibility_checks"] != 6 {
					t.Fatal("成功早筛仍对44个无关访客查询数据库或跳过目标完整鉴权")
				}
			} else if counts["visitor_live_checks"] != 45 || counts["conversation_visibility_checks"] != 50 {
				t.Fatal("早筛失败没有回到原完整权限路径")
			}
		})
	}
}
