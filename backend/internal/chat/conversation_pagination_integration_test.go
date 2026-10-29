// 文件作用说明：在隔离真实PostgreSQL验证会话活动分页、完整自然日边界、旧咨询新消息回归及游标授权隔离。
package chat

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestConversationActivityPagination 验证稳定游标不遗漏同时间会话，并保留创建时间筛选和旧调用兼容。
func TestConversationActivityPagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ids, tokens := make([]string, 7), make([]string, 7)
	for index := range ids {
		tokens[index], _, ids[index] = f.visitor("")
	}
	stamps := []string{"2026-10-25T16:00:00Z", "2026-10-26T04:00:00Z", "2026-10-26T04:00:00Z", "2026-10-26T15:59:59.999999Z", "2026-10-26T16:00:00Z", "2026-10-25T15:59:59.999999Z"}
	for index, stamp := range stamps {
		if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at='2026-10-01T00:00:00Z',last_message_at=$2::timestamptz WHERE id=$1", ids[index], stamp); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at='2026-10-26T08:00:00Z',last_message_at=NULL WHERE id=$1", ids[6]); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/conversations?limit=2&activity_from=2026-10-26&activity_to=2026-10-26"
	status, reply := f.request("GET", path, f.owner, "", nil)
	page := mustData(t, status, reply)
	if page["timezone"] != "Asia/Shanghai" || page["has_more"] != true || len(page["items"].([]any)) != 2 {
		t.Fatal("第一页应有两条、后续页及租户时区")
	}
	firstCursor := page["next_cursor"].(string)
	seen := map[string]bool{}
	var ordered []string
	for {
		for _, raw := range page["items"].([]any) {
			id := raw.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatal("稳定分页重复返回会话")
			}
			seen[id] = true
			ordered = append(ordered, id)
		}
		if page["has_more"] != true {
			if page["next_cursor"] != "" {
				t.Fatal("末页不应继续提供分页游标")
			}
			break
		}
		status, reply = f.request("GET", path+"&cursor="+url.QueryEscape(page["next_cursor"].(string)), f.owner, "", nil)
		page = mustData(t, status, reply)
	}
	tied := []string{ids[1], ids[2]}
	sort.Strings(tied)
	want := []string{ids[3], ids[6], tied[0], tied[1], ids[0]}
	if strings.Join(ordered, ",") != strings.Join(want, ",") {
		t.Fatal("自然日边界、无消息时回退创建时间或相同时间的编号排序不正确")
	}
	status, reply = f.request("GET", "/api/v1/conversations?from=2026-10-26&to=2026-10-26", f.owner, "", nil)
	created := mustData(t, status, reply)["items"].([]any)
	if len(created) != 1 || created[0].(map[string]any)["id"] != ids[6] {
		t.Fatal("原创建时间筛选不应被工作台活动时间筛选改写")
	}
	status, reply = f.request("GET", "/api/v1/conversations", f.owner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != len(ids) {
		t.Fatal("无分页参数旧调用应保持原有结果")
	}
	for _, request := range []struct{ member, path string }{
		{f.otherOwner, path + "&cursor=" + url.QueryEscape(firstCursor)},
		{f.agent, path + "&cursor=" + url.QueryEscape(firstCursor)},
		{f.owner, path + "&status=closed&cursor=" + url.QueryEscape(firstCursor)},
		{f.owner, path + "&cursor=" + url.QueryEscape(firstCursor+"!")},
		{f.owner, "/api/v1/conversations?limit=0"},
		{f.owner, "/api/v1/conversations?limit=101"},
		{f.owner, "/api/v1/conversations?activity_from=not-a-date"},
		{f.owner, "/api/v1/conversations?activity_from=2026-10-27&activity_to=2026-10-25"},
	} {
		status, _ = f.request("GET", request.path, request.member, "", nil)
		if status != 400 {
			t.Fatalf("跨授权或筛选游标及非法分页日期应拒绝，实际HTTP%d", status)
		}
	}
	// 全部咨询曾长期无公开消息，内部备注仍不能让它重新进入近期活动；真实公开消息必须立即使它可见。
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at=now()-interval '10 days',last_message_at=now()-interval '10 days'"); err != nil {
		t.Fatal(err)
	}
	f.online(f.agent)
	status, reply = f.request("POST", "/api/v1/conversations/"+ids[0]+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+ids[0]+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "内部备注不改变会话活动日期", "audience": "internal"})
	mustData(t, status, reply)
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	today := time.Now().In(location)
	recent := "/api/v1/conversations?limit=2&activity_from=" + today.AddDate(0, 0, -2).Format("2006-01-02") + "&activity_to=" + today.Format("2006-01-02")
	status, reply = f.request("GET", recent, f.owner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("旧会话不能因领取或内部备注进入最近3天")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+ids[0]+"/messages", "", tokens[0], map[string]any{"client_message_id": newID(), "content": "十天前的客户再次发来消息"})
	mustData(t, status, reply)
	status, reply = f.request("GET", recent, f.owner, "", nil)
	returned := mustData(t, status, reply)["items"].([]any)
	if len(returned) != 1 || returned[0].(map[string]any)["id"] != ids[0] || returned[0].(map[string]any)["unread_count"] != float64(1) {
		t.Fatal("旧会话真实新消息必须进入最近3天列表并保留未读数")
	}
}
