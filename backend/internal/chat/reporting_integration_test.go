// 文件作用说明：用真实访客与消息接口验证来源快照优先级、访问咨询去重、首响口径，以及聊天筛选、CSV和统计跨模块一致性。
package chat

import (
	"context"
	"math"
	"net/url"
	"testing"
)

// TestReportingSourceMatchesChatAndCSV 验证tracking来源优先、entry兼容兜底与未标记分组一致，系统和内部消息不会冒充人工首次公开回复。
func TestReportingSourceMatchesChatAndCSV(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.online(f.agent)
	status, reply := f.request("GET", "/api/v1/entries/"+f.entry, "", "", nil)
	entryID := mustData(t, status, reply)["id"].(string)
	status, reply = f.request("PATCH", "/api/v1/tenant/access-links/"+entryID, f.owner, "", map[string]any{"channel_code": "广告追踪"})
	mustData(t, status, reply)
	token, _, first := f.visitor("")
	visit := newID()
	for index := 0; index < 3; index++ {
		status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": visit})
		if mustData(t, status, reply)["conversation"].(map[string]any)["id"] != first {
			t.Fatal("初始化重试应复用进行中咨询")
		}
	}
	if err := f.s.runAutomatic(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "访客咨询"})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "内部操作备注", "audience": "internal"})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "首次人工公开回复"})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+first+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	second := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	if first == second {
		t.Fatal("结束后必须产生新的独立咨询")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/access-links/"+entryID, f.owner, "", map[string]any{"channel_code": "入口兜底"})
	mustData(t, status, reply)
	_, _, fallback := f.visitor("")
	status, reply = f.request("PATCH", "/api/v1/tenant/access-links/"+entryID, f.owner, "", map[string]any{"channel_code": ""})
	mustData(t, status, reply)
	f.visitor("")
	// 有冲突的旧入口快照与缺失tracking键是明确的兼容性测试数据，来源主体仍由真实访客接口建立。
	if _, err := f.admin.Exec(ctx, `UPDATE conversations SET source_snapshot=jsonb_set(source_snapshot,'{entry,channel_code}','"不得覆盖tracking"')||'{"channel_code":"不得读取顶层"}'::jsonb WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, `UPDATE conversations SET source_snapshot=source_snapshot#-'{tracking,channel_code}' WHERE id=$1`, fallback); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`UPDATE conversations SET created_at='2026-10-26T00:00:00+08:00'`, `UPDATE contacts SET created_at='2026-10-26T00:01:00+08:00'`, `UPDATE contact_visits SET created_at='2026-10-26T00:01:00+08:00'`, `UPDATE conversation_assignments SET accepted_at='2026-10-26T00:00:05+08:00'`, `UPDATE messages SET created_at=CASE WHEN sender_kind='system' THEN '2026-10-26T00:00:01+08:00'::timestamptz WHEN sender_kind='visitor' THEN '2026-10-26T00:00:02+08:00'::timestamptz WHEN audience='internal' THEN '2026-10-26T00:00:03+08:00'::timestamptz ELSE '2026-10-26T00:00:12+08:00'::timestamptz END`} {
		if _, err := f.admin.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	period := "from=2026-10-26&to=2026-10-26"
	status, reply = f.request("GET", "/api/v1/tenant/statistics?"+period, f.owner, "", nil)
	statistics := mustData(t, status, reply)
	overview := statistics["overview"].(map[string]any)
	if overview["visitors"] != float64(3) || overview["contacts"] != float64(3) || overview["conversations"] != float64(4) || overview["messages"] != float64(3) || overview["closed"] != float64(1) || overview["queued"] != float64(3) {
		t.Fatalf("真实访问与咨询去重或公开消息统计错误: %+v", overview)
	}
	if f.countRows("SELECT count(*) FROM contact_visits") != 5 {
		t.Fatal("同visit_id的网络重试不得增加访问次数")
	}
	if math.Abs(overview["avg_first_response_seconds"].(float64)-12) > 0.00001 || math.Abs(overview["avg_wait_seconds"].(float64)-5) > 0.00001 {
		t.Fatalf("系统欢迎或内部备注错误计入人工首响: %+v", overview)
	}
	channels := map[string]map[string]any{}
	for _, raw := range statistics["by_channel"].([]any) {
		item := raw.(map[string]any)
		channels[item["channel_code"].(string)] = item
	}
	if len(channels) != 3 || channels["广告追踪"] == nil || channels["入口兜底"] == nil || channels["未标记"] == nil {
		t.Fatalf("来源应按tracking优先、entry兜底分组，得到%v", channels)
	}
	if channels["广告追踪"]["conversations"] != float64(2) || channels["广告追踪"]["visitors"] != float64(1) || channels["入口兜底"]["conversations"] != float64(1) {
		t.Fatalf("同访客两次独立咨询的来源聚合不正确: %+v", channels)
	}
	for _, channel := range []string{"广告追踪", "入口兜底"} {
		query := period + "&channel_code=" + url.QueryEscape(channel)
		status, reply = f.request("GET", "/api/v1/tenant/chat-records?"+query, f.owner, "", nil)
		items := mustData(t, status, reply)["items"].([]any)
		rows := csvRecords(t, f.downloadCSV("/api/v1/tenant/chat-records/export?"+query, f.owner, ""))
		if float64(len(items)) != channels[channel]["conversations"] || len(rows)-1 != len(items) {
			t.Fatalf("%s的列表、CSV与统计数量不一致", channel)
		}
		ids := map[string]bool{}
		for _, raw := range items {
			ids[raw.(map[string]any)["id"].(string)] = true
		}
		for _, row := range rows[1:] {
			if !ids[row[0]] || row[7] != channel {
				t.Fatalf("%s的CSV来源字段或咨询范围不一致", channel)
			}
		}
	}
	// 空tracking来源也必须兼容旧entry来源，不能把空字符串当作一个覆盖值。
	if _, err := f.admin.Exec(ctx, `UPDATE conversations SET source_snapshot=jsonb_set(source_snapshot,'{tracking,channel_code}','""') WHERE id=$1`, fallback); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("GET", "/api/v1/tenant/chat-records?"+period+"&channel_code="+url.QueryEscape("入口兜底"), f.owner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 1 {
		t.Fatal("空tracking值没有回退entry来源")
	}
	status, reply = f.request("GET", "/api/v1/tenant/statistics?"+period, f.otherOwner, "", nil)
	other := mustData(t, status, reply)
	if other["overview"].(map[string]any)["conversations"] != float64(0) || len(other["by_channel"].([]any)) != 0 {
		t.Fatal("统计跨租户泄漏")
	}
	daily := statistics["daily"].([]any)
	if len(daily) != 1 || daily[0].(map[string]any)["date"] != "2026-10-26" || daily[0].(map[string]any)["visitors"] != float64(3) || daily[0].(map[string]any)["conversations"] != float64(4) {
		t.Fatal("上海自然日期的每日统计与概览不一致")
	}
}
