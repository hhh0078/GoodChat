// 文件作用说明：通过真实PostgreSQL和HTTP验证跨咨询备注的幂等、分页、旧备注兼容与客户权限隔离。
package chat

import (
	"context"
	"net/url"
	"testing"
)

// TestCustomerNotesPersistAndRespectCurrentScope 验证备注跨咨询保存但读取权随当前接待关系撤销，网络重试不重复创建。
func TestCustomerNotesPersistAndRespectCurrentScope(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, contact, conversation := f.visitor("")
	path := "/api/v1/tenant/contacts/" + contact + "/notes"
	for _, member := range []string{f.agent, f.otherOwner} {
		status, _ := f.request("GET", path, member, "", nil)
		if status != 404 {
			t.Fatalf("未接待或跨租户应拒绝备注：%d", status)
		}
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	clientID := newID()
	input := map[string]any{"content": "客户首次跟进", "client_note_id": clientID}
	status, reply = f.request("POST", path, f.agent, "", input)
	first := mustData(t, status, reply)
	status, reply = f.request("POST", path, f.agent, "", input)
	if mustData(t, status, reply)["id"] != first["id"] {
		t.Fatal("备注重试产生重复记录")
	}
	input["content"] = "同一编号不能覆盖内容"
	status, _ = f.request("POST", path, f.agent, "", input)
	if status != 409 {
		t.Fatalf("同编号不同内容应冲突：%d", status)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "旧版内部备注", "audience": "internal"})
	legacy := mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, _ = f.request("GET", path, f.agent, "", nil)
	if status != 404 {
		t.Fatalf("结束后不能凭旧关系读取客户备注：%d", status)
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	next := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	status, reply = f.request("POST", "/api/v1/conversations/"+next+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("GET", path+"?limit=1", f.agent, "", nil)
	page := mustData(t, status, reply)
	if page["has_more"] != true || len(page["items"].([]any)) != 1 {
		t.Fatal("旧内部备注和新备注应独立分页")
	}
	ids := map[any]bool{page["items"].([]any)[0].(map[string]any)["id"]: true}
	cursor := url.Values{"limit": {"1"}, "before": {page["next_before"].(string)}, "before_id": {page["next_before_id"].(string)}}
	status, reply = f.request("GET", path+"?"+cursor.Encode(), f.agent, "", nil)
	second := mustData(t, status, reply)
	if second["has_more"] != false || len(second["items"].([]any)) != 1 {
		t.Fatal("备注第二页应完整结束")
	}
	ids[second["items"].([]any)[0].(map[string]any)["id"]] = true
	if !ids[first["id"]] || !ids[legacy["id"]] {
		t.Fatal("跨咨询备注遗漏或重复")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+next+"/messages", "", token, nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("内部客户备注不应成为访客消息")
	}
	status, _ = f.request("GET", path+"?before=bad&before_id="+newID(), f.agent, "", nil)
	if status != 400 {
		t.Fatalf("非法分页参数应拒绝：%d", status)
	}
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	status, _ = f.request("POST", path, f.agent, "", map[string]any{"content": "撤权后禁止写入", "client_note_id": newID()})
	if status != 403 {
		t.Fatalf("停用客服不能继续写备注：%d", status)
	}
}

// TestHourlyStatisticsUseRealPublicMessages 验证默认当天返回完整小时桶，内部备注不增加客户消息数。
func TestHourlyStatisticsUseRealPublicMessages(t *testing.T) {
	f := newFixture(t)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "小时统计消息"})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/tenant/statistics", f.owner, "", nil)
	data := mustData(t, status, reply)
	trend := data["trend"].([]any)
	if data["trend_interval"] != "hour" || len(trend) != 24 {
		t.Fatalf("默认亚洲上海当天应有24小时桶：%v %d", data["trend_interval"], len(trend))
	}
	var conversations, messages float64
	for _, raw := range trend {
		item := raw.(map[string]any)
		conversations += item["conversations"].(float64)
		messages += item["messages"].(float64)
	}
	if conversations != 1 || messages != 1 {
		t.Fatalf("小时桶应统计真实消息：%v %v", conversations, messages)
	}
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenants SET timezone='America/New_York' WHERE id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	for day, want := range map[string]int{"2026-03-08": 23, "2026-11-01": 25} {
		status, reply = f.request("GET", "/api/v1/tenant/statistics?from="+day+"&to="+day, f.owner, "", nil)
		dayData := mustData(t, status, reply)
		if got := len(dayData["trend"].([]any)); got != want {
			t.Fatalf("夏令时%s应有%d小时，实际%d", day, want, got)
		}
	}
}
