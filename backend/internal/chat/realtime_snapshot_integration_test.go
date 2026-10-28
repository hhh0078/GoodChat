// 文件作用说明：通过真实HTTP并发发送与分页读取，验证消息快照恢复游标无缺口且不会重放撤回旧正文。
package chat

import (
	"fmt"
	"net/url"
	"sync"
	"testing"
)

// TestRealtimeSnapshotConcurrentSend 验证快照与其后事件的并集覆盖竞态消息，分页不会回退同步游标。
func TestRealtimeSnapshotConcurrentSend(t *testing.T) {
	f := newFixture(t)
	token, _, conversation := f.visitor("13800000000")
	var last string
	for i := 0; i < 15; i++ {
		status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": fmt.Sprintf("旧消息%d", i)})
		last = mustData(t, status, reply)["id"].(string)
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+last+"/withdraw", "", token, map[string]any{})
	mustData(t, status, reply)
	const count = 8
	var group sync.WaitGroup
	gate := make(chan struct{})
	results := make(chan map[string]any, count)
	statuses := make(chan int, count)
	for i := 0; i < count; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-gate
			status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": fmt.Sprintf("并发新消息%d", index)})
			statuses <- status
			results <- reply
		}(i)
	}
	close(gate)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages?limit=10", "", token, nil)
	snapshot := mustData(t, status, reply)
	group.Wait()
	close(results)
	close(statuses)
	for status := range statuses {
		if status != 200 {
			t.Fatalf("并发发送HTTP%d", status)
		}
	}
	history := snapshot["items"].([]any)
	if len(history) != 10 || snapshot["has_more"] != true {
		t.Fatal("最近历史分页失效")
	}
	syncCursor, ok := snapshot["sync_cursor"].(string)
	if !ok || syncCursor == "" {
		t.Fatal("快照缺少签名恢复游标")
	}
	observed := map[string]bool{}
	for _, raw := range history {
		item := raw.(map[string]any)
		observed[item["id"].(string)] = true
		if item["id"] == last && item["content"] != "" {
			t.Fatal("撤回正文在历史快照中恢复")
		}
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/events?after_cursor="+url.QueryEscape(syncCursor), "", token, nil)
	events := mustData(t, status, reply)["items"].([]any)
	for _, raw := range events {
		event := raw.(map[string]any)
		if event["event_type"] == "message.created" {
			item := event["payload"].(map[string]any)
			observed[item["id"].(string)] = true
			if item["id"] == last {
				t.Fatal("初始化快照后的事件重新播放了旧撤回消息")
			}
		}
	}
	for result := range results {
		message := result["data"].(map[string]any)
		if !observed[message["id"].(string)] {
			t.Fatal("快照与恢复事件遗漏了并发新消息")
		}
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages?limit=10&before_seq="+url.QueryEscape(snapshot["next_cursor"].(string)), "", token, nil)
	older := mustData(t, status, reply)
	if _, ok := older["sync_cursor"]; ok {
		t.Fatal("向前翻页不能返回会导致实时游标倒退的同步起点")
	}
	if len(older["items"].([]any)) == 0 {
		t.Fatal("旧历史分页未返回记录")
	}
}
