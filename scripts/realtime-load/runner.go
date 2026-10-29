// 文件作用说明：在真实WS连接上运行双向HTTP消息、固定编号并发重试、新票据重连与持久事件/数据库对账。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type expected struct {
	Key, Conversation, MessageID string
	Receiver                     *peer
	Started                      time.Time
	Ack                          bool
	Offline                      bool
}
type ledger struct {
	mu         sync.Mutex
	Items      []*expected
	Mismatches int
}
type eventPage struct {
	Items []struct {
		Type    string `json:"event_type"`
		Payload struct {
			ClientID string `json:"client_message_id"`
		} `json:"payload"`
	} `json:"items"`
	Next string `json:"next_cursor"`
	More bool   `json:"has_more"`
}

// readFixture 只接受明确演示前缀与本机入口，不复用真实业务租户或身份。
func readFixture(path string) (fixture, error) {
	var f fixture
	b, e := os.ReadFile(path)
	if e != nil {
		return f, e
	}
	e = json.Unmarshal(b, &f)
	if e != nil {
		return f, e
	}
	u, parseErr := url.Parse(f.Base)
	if parseErr != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(f.Name, "演示数据·实时") {
		return f, fmt.Errorf("invalid_fixture_scope")
	}
	return f, nil
}

// runLoad 按显式阶梯升压，一阶失败即停止，不自动修改限流或人工容量基线。
func runLoad(ctx context.Context, o options) error {
	f, e := readFixture(o.fixture)
	if e != nil {
		return e
	}
	if o.rate < 1 || o.rate > 10 {
		return fmt.Errorf("rate_must_be_1_to_10")
	}
	if e = os.MkdirAll(o.output, 0700); e != nil {
		return e
	}
	for _, text := range strings.Split(o.stages, ",") {
		target, e := strconv.Atoi(strings.TrimSpace(text))
		if e != nil || target < 10 || target > 400 || target-len(f.Accounts) > len(f.Visitors) {
			return fmt.Errorf("invalid_stage")
		}
		report, pass := runStage(ctx, o, f, target)
		if e = writeJSON(filepath.Join(o.output, fmt.Sprintf("stage-%d.json", target)), report, false); e != nil {
			return e
		}
		fmt.Printf("stage_complete websocket_target=%d pass=%t\n", target, pass)
		if !pass {
			return fmt.Errorf("stage_failed_stop_escalation")
		}
	}
	return nil
}

// sendMessage 对稳定编号同时发起两个真实POST，比较持久消息ID，不把ACK等同对端已收。
func sendMessage(ctx context.Context, a *apiClient, clock <-chan time.Time, l *ledger, who identity, receiver *peer, conversation, content string, retry bool) {
	count := 1
	if retry {
		count = 2
	}
	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return
		case <-clock:
		}
	}
	x := &expected{Key: uuid(), Conversation: conversation, Receiver: receiver, Started: time.Now()}
	l.mu.Lock()
	l.Items = append(l.Items, x)
	l.mu.Unlock()
	ids := make([]string, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var saved struct {
				ID string `json:"id"`
			}
			start := time.Now()
			e := a.request(ctx, "POST", "/conversations/"+conversation+"/messages", who, map[string]any{"client_message_id": x.Key, "content": content}, &saved)
			a.metrics.record("message", time.Since(start), e == nil, 0)
			if e == nil {
				ids[i] = saved.ID
			}
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		if id != "" {
			x.Ack = true
			if x.MessageID != "" && x.MessageID != id {
				l.mu.Lock()
				l.Mismatches++
				l.mu.Unlock()
			}
			x.MessageID = id
		}
	}
}

// syncEvents 真实遍历服务端签名游标，既可从快照扫描也可从断线前位置补齐。
func syncEvents(ctx context.Context, a *apiClient, p *peer, conversation, cursor string) (map[string]bool, string, error) {
	found := map[string]bool{}
	for pages := 0; pages < 100; pages++ {
		var result eventPage
		e := a.request(ctx, "GET", "/conversations/"+conversation+"/events?after_cursor="+url.QueryEscape(cursor), p.auth, nil, &result)
		if e != nil {
			return found, cursor, e
		}
		for _, item := range result.Items {
			if item.Type == "message.created" && item.Payload.ClientID != "" {
				found[item.Payload.ClientID] = true
			}
		}
		cursor = result.Next
		if !result.More {
			return found, cursor, nil
		}
	}
	return found, cursor, fmt.Errorf("event_scan_limit")
}

// runStage 以至少两次服务端探活覆盖真实消息阶段，并给outbox固定20秒排空预算。
func runStage(parent context.Context, o options, f fixture, target int) (map[string]any, bool) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	start := time.Now()
	m := newMeasurements()
	s := &safety{Samples: []resource{}}
	go monitor(ctx, cancel, m, s)
	a := &apiClient{f.Base, f.TenantID, &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{MaxIdleConns: 128, MaxIdleConnsPerHost: 64, MaxConnsPerHost: 32}}, m}
	defer a.http.CloseIdleConnections()
	agents := []*peer{}
	visitors := []*peer{}
	all := []*peer{}
	defer func() {
		for _, p := range all {
			closePeer(p)
		}
	}()
	image, _ := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Image}}", "goodchat").Output()
	report := map[string]any{"started_at": start.UTC(), "target_websockets": target, "agents": len(f.Accounts), "generator_gomaxprocs": 2, "generator_memory_soft_limit_bytes": 384 << 20, "message_request_rate_limit": o.rate, "limits": "CPU>400% 3samples; memory>1.2GiB; HTTP errors>2%; message p95>2s"}
	report["application_image_id"] = strings.TrimSpace(string(image))
	finish := func(reason string) (map[string]any, bool) {
		report["error"] = reason
		report["metrics"] = m.summary()
		report["elapsed_seconds"] = time.Since(start).Seconds()
		s.mu.Lock()
		report["resources"] = append([]resource(nil), s.Samples...)
		report["safety_stop"] = s.Stopped
		s.mu.Unlock()
		return report, false
	}
	for _, acct := range f.Accounts {
		p := newPeer(identity{cookie: acct.Cookie}, "")
		all = append(all, p)
		if _, e := a.connect(ctx, p); e != nil {
			return finish("agent_connect_" + safeError(e))
		}
		agents = append(agents, p)
		if e := a.request(ctx, "PATCH", "/agent/status", p.auth, map[string]any{"available": true, "busy": false}, nil); e != nil {
			return finish("agent_available_" + safeError(e))
		}
	}
	n := target - len(agents)
	for i := 0; i < n; i++ {
		v := f.Visitors[i]
		if e := a.request(ctx, "POST", "/conversations/"+v.ConversationID+"/assignments/claim", agents[v.Agent].auth, map[string]any{}, nil); e != nil {
			return finish("claim_" + safeError(e))
		}
	}
	ticketClock := time.NewTicker(time.Second / 12)
	defer ticketClock.Stop()
	replayOK := false
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			return finish("stage_cancelled")
		case <-ticketClock.C:
		}
		v := f.Visitors[i]
		p := newPeer(identity{token: v.Token}, v.ConversationID)
		all = append(all, p)
		ticket, e := a.connect(ctx, p)
		if e != nil {
			return finish("visitor_connect_" + safeError(e))
		}
		visitors = append(visitors, p)
		if i == 0 {
			u, _ := url.Parse(f.Base + ticket)
			u.Scheme = "ws"
			conn, response, err := websocket.Dial(ctx, u.String(), nil)
			if conn != nil {
				conn.CloseNow()
			}
			replayOK = err != nil && response != nil && response.StatusCode == 401
		}
	}
	live, _ := peerStates(all)
	report["established_websockets"] = live
	if live != len(all) {
		return finish("connections_closed_during_setup")
	}
	report["ticket_replay_denied"] = replayOK
	fmt.Printf("stage_ready websocket_count=%d sending_bidirectional=true\n", len(all))
	clock := time.NewTicker(time.Second / time.Duration(o.rate))
	defer clock.Stop()
	l := &ledger{}
	sem := make(chan struct{}, 8)
	var workers sync.WaitGroup
	trafficStart := time.Now()
	for i, p := range visitors {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		workers.Add(1)
		go func(i int, p *peer) {
			defer workers.Done()
			defer func() { <-sem }()
			v := f.Visitors[i]
			sendMessage(ctx, a, clock.C, l, p.auth, agents[v.Agent], v.ConversationID, fmt.Sprintf("演示数据·阶段%d·访客%d", target, i), i%10 == 0)
			sendMessage(ctx, a, clock.C, l, agents[v.Agent].auth, p, v.ConversationID, fmt.Sprintf("演示数据·阶段%d·客服回复%d", target, i), false)
		}(i, p)
	}
	workers.Wait()
	report["traffic_seconds"] = time.Since(trafficStart).Seconds()
	report["steady_unique_messages"] = len(l.Items)
	report["steady_unique_messages_per_second"] = float64(len(l.Items)) / report["traffic_seconds"].(float64)
	if ctx.Err() != nil {
		for _, p := range all {
			closePeer(p)
		}
		auditCtx, stopAudit := context.WithTimeout(context.Background(), 45*time.Second)
		defer stopAudit()
		report["error"] = "resource_or_deadline_stop"
		result, _ := reconcile(auditCtx, o, f, a, l, agents, visitors, report, start, m, s, replayOK, 0, 0)
		result["pass"] = false
		return result, false
	}
	reconnected, recovered := 0, 0
	for i, p := range visitors {
		if i%10 != 0 {
			continue
		}
		v := f.Visitors[i]
		_, cursor, e := syncEvents(ctx, a, p, v.ConversationID, "")
		if e != nil {
			return finish("cursor_before_disconnect_" + safeError(e))
		}
		closePeer(p)
		sendMessage(ctx, a, clock.C, l, agents[v.Agent].auth, p, v.ConversationID, fmt.Sprintf("演示数据·阶段%d·离线期间回复%d", target, i), false)
		l.Items[len(l.Items)-1].Offline = true
		select {
		case <-ctx.Done():
			return finish("stage_cancelled")
		case <-time.After(150 * time.Millisecond):
		}
		if _, e = a.connect(ctx, p); e != nil {
			return finish("reconnect_" + safeError(e))
		}
		reconnected++
		found, _, e := syncEvents(ctx, a, p, v.ConversationID, cursor)
		if e != nil {
			return finish("cursor_recovery_" + safeError(e))
		}
		l.mu.Lock()
		if found[l.Items[len(l.Items)-1].Key] {
			recovered++
		}
		l.mu.Unlock()
	}
	report["reconnections_new_tickets"] = reconnected
	report["offline_messages_recovered_from_prior_cursor"] = recovered
	report["message_workload_seconds"] = time.Since(trafficStart).Seconds()
	drainUntil := time.Now().Add(20 * time.Second)
	for time.Now().Before(drainUntil) {
		missing := 0
		for _, x := range l.Items {
			x.Receiver.mu.Lock()
			if x.Receiver.events[x.Key] == 0 {
				missing++
			}
			x.Receiver.mu.Unlock()
		}
		if missing <= reconnected && time.Since(start) > 35*time.Second {
			break
		}
		select {
		case <-ctx.Done():
			return finish("resource_or_deadline_stop")
		case <-time.After(time.Second):
		}
	}
	return reconcile(ctx, o, f, a, l, agents, visitors, report, start, m, s, replayOK, reconnected, recovered)
}

// reconcile 按同一批稳定编号比较ACK、WS、HTTP游标、HTTP历史及PG唯一行。
func reconcile(ctx context.Context, o options, f fixture, a *apiClient, l *ledger, agents, visitors []*peer, report map[string]any, start time.Time, m *measurements, s *safety, replayOK bool, reconnected, recovered int) (map[string]any, bool) {
	events, historyIDs := map[*peer]map[string]bool{}, map[string]bool{}
	syncErrors := 0
	for i, p := range visitors {
		v := f.Visitors[i]
		for _, who := range []*peer{p, agents[v.Agent]} {
			if events[who] == nil {
				events[who] = map[string]bool{}
			}
			found, _, e := syncEvents(ctx, a, who, v.ConversationID, "")
			if e != nil {
				syncErrors++
			}
			for key := range found {
				events[who][key] = true
			}
		}
		var history struct {
			Items []struct {
				ClientID string `json:"client_message_id"`
			} `json:"items"`
		}
		if e := a.request(ctx, "GET", "/conversations/"+v.ConversationID+"/messages", p.auth, nil, &history); e != nil {
			syncErrors++
		}
		for _, item := range history.Items {
			historyIDs[item.ClientID] = true
		}
	}
	keys := []string{}
	acks, rawMissing, duplicates, mergedMissing, historyMissing, unexpectedMissing := 0, 0, 0, 0, 0, 0
	for _, x := range l.Items {
		keys = append(keys, x.Key)
		if x.Ack {
			acks++
		}
		x.Receiver.mu.Lock()
		count := x.Receiver.events[x.Key]
		received := x.Receiver.arrivals[x.Key]
		x.Receiver.mu.Unlock()
		if count == 0 {
			rawMissing++
			if !x.Offline {
				unexpectedMissing++
			}
		}
		if count > 1 {
			duplicates += count - 1
		}
		if !received.IsZero() {
			m.record("ws_delivery", received.Sub(x.Started), true, 0)
		}
		if count == 0 && !events[x.Receiver][x.Key] {
			mergedMissing++
		}
		if !historyIDs[x.Key] {
			historyMissing++
		}
	}
	db, e := database(ctx, o.config)
	if e != nil {
		report["error"] = "audit_database_" + safeError(e)
		return report, false
	}
	defer db.Close(ctx)
	var saved, unique int
	e = db.QueryRow(ctx, `SELECT count(*),count(DISTINCT client_message_id) FROM messages WHERE tenant_id=$1 AND client_message_id=ANY($2::uuid[])`, f.TenantID, keys).Scan(&saved, &unique)
	var pending int
	outboxErr := db.QueryRow(ctx, `SELECT count(*) FROM outbox_jobs WHERE tenant_id=$1 AND completed_at IS NULL`, f.TenantID).Scan(&pending)
	if outboxErr != nil {
		report["outbox_audit_error"] = safeError(outboxErr)
	}
	report["planned_unique_messages"] = len(keys)
	report["http_acked_unique"] = acks
	report["db_saved_rows"] = saved
	report["db_saved_unique"] = unique
	report["db_missing"] = len(keys) - unique
	report["db_duplicate_rows"] = saved - unique
	report["ws_raw_missing_including_intentional_offline"] = rawMissing
	report["ws_unexpected_missing_after_drain"] = unexpectedMissing
	report["ws_raw_duplicate_frames"] = duplicates
	report["ws_plus_events_missing"] = mergedMissing
	report["http_history_missing"] = historyMissing
	report["http_sync_errors"] = syncErrors
	report["retry_response_id_mismatch"] = l.Mismatches
	report["outbox_pending_at_audit"] = pending
	report["metrics"] = m.summary()
	report["elapsed_seconds"] = time.Since(start).Seconds()
	duration := report["traffic_seconds"].(float64)
	if full, ok := report["message_workload_seconds"].(float64); ok {
		duration = full
	}
	report["unique_messages_per_second_traffic"] = float64(len(keys)) / duration
	live, unexpected := peerStates(agents, visitors)
	report["live_websockets_at_audit"], report["unexpected_disconnects"] = live, unexpected
	m.mu.Lock()
	wsP95 := percentile(m.times["ws_delivery"], .95)
	m.mu.Unlock()
	quality := wsP95 <= 2000 && unexpectedMissing == 0 && outboxErr == nil && pending == 0 && unexpected == 0 && live == len(agents)+len(visitors)
	report["delivery_quality_pass"] = quality
	s.mu.Lock()
	report["resources"] = append([]resource(nil), s.Samples...)
	stopped := s.Stopped
	report["safety_stop"] = stopped
	s.mu.Unlock()
	integrity := e == nil && len(keys) > 0 && acks == len(keys) && saved == unique && unique == len(keys) && mergedMissing == 0 && historyMissing == 0 && syncErrors == 0 && l.Mismatches == 0 && replayOK && reconnected == recovered
	report["integrity_pass"] = integrity
	pass := integrity && quality && stopped == ""
	report["pass"] = pass
	return report, pass
}
