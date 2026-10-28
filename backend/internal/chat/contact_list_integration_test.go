// 文件作用说明：通过真实客户查询验证字段搜索、额外资料和时间投影，当前客服不能借同客户其他范围的会话或搜索读取私有上下文。
package chat

import (
	"net/url"
	"testing"
)

// TestContactFieldSearchAndScopedContext 验证服务器字段搜索与展示投影使用同一授权上下文，跨业务空间的新会话不向旧接待人泄漏。
func TestContactFieldSearchAndScopedContext(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, contact, conversation := f.visitor("13876543210")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID(), "context": map[string]any{"产品": "当前授权100%", "零": 0, "开关": false}})
	mustData(t, status, reply)
	space := newID()
	if _, err := f.admin.Exec(t.Context(), "INSERT INTO service_spaces(id,tenant_id,name) VALUES($1,$2,'新的业务范围')", space, f.tenant); err != nil {
		t.Fatal(err)
	}
	var group string
	if err := f.admin.QueryRow(t.Context(), "SELECT service_group_id::text FROM access_links WHERE entry_key=$1", f.entry).Scan(&group); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/tenant/access-links", f.owner, "", map[string]any{"name": "其他范围入口", "service_space_id": space, "service_group_id": group, "routing_pool_id": f.poolID, "reception_mode": "manual"})
	if status != 201 {
		t.Fatalf("测试入口创建失败：%d", status)
	}
	link := reply["data"].(map[string]any)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": link["entry_key"], "visitor_token": token, "visit_id": newID(), "context": map[string]any{"秘密": "其他范围不可见业务"}})
	mustData(t, status, reply)
	// 同客户还有当前合法接待，列表仍可展示客户，但不得取其他范围最新会话内容。
	status, reply = f.request("GET", "/api/v1/tenant/contacts?search_field=phone&query=13876543210", f.agent, "", nil)
	items := mustData(t, status, reply)["items"].([]any)
	if len(items) != 1 {
		t.Fatal("电话搜索应返回当前接待客户")
	}
	row := items[0].(map[string]any)
	context := row["last_context"].(map[string]any)
	if row["id"] != contact || context["产品"] != "当前授权100%" || context["秘密"] != nil || row["last_visit_at"] == nil || row["visit_count"].(float64) != 3 {
		t.Fatal("客户上下文或访问投影未遵守当前授权")
	}
	for _, value := range []string{"其他范围不可见业务", "%"} {
		query := "/api/v1/tenant/contacts?search_field=extra&query=" + url.QueryEscape(value)
		status, reply = f.request("GET", query, f.agent, "", nil)
		got := mustData(t, status, reply)["items"].([]any)
		expected := 0
		if value == "%" {
			expected = 1
		}
		if len(got) != expected {
			t.Fatal("字段搜索应按授权内容及字面百分号匹配")
		}
	}
	status, reply = f.request("GET", "/api/v1/tenant/contacts?search_field=extra&query="+url.QueryEscape("其他范围不可见业务"), f.owner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 1 {
		t.Fatal("管理员应能按最新客户会话搜索")
	}
	status, _ = f.request("GET", "/api/v1/tenant/contacts?search_field=tenant_id&query=x", f.owner, "", nil)
	if status != 400 {
		t.Fatal("未知搜索字段必须拒绝")
	}
	status, reply = f.request("GET", "/api/v1/tenant/contacts?search_field=phone&query=13876543210", f.otherOwner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("客户搜索不能跨租户")
	}
}
