// 文件作用说明：通过真实客户查询验证字段搜索、额外资料和时间投影，当前客服不能借同客户其他范围的会话或搜索读取私有上下文。
package chat

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// TestContactCombinedSearchQuery 验证组合字段各自绑定、忽略空白，并保持旧查询及授权参数位置不变。
func TestContactCombinedSearchQuery(t *testing.T) {
	a := actor{MemberID: "member", Role: "owner"}
	values := url.Values{
		"query": {" 原查询 "}, "search_field": {"phone"},
		"name": {" 名称 "}, "phone": {"123"}, "channel": {"  "},
		"extra": {"' OR 1=1 --"}, "tag": {"标签"}, "category": {"分类"},
	}
	query, args, err := contactListQuery(httptest.NewRequest("GET", "/?"+values.Encode(), nil), a)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []any{"member", true, "原查询", "名称", "123", "' OR 1=1 --", "标签", "分类"}) {
		t.Fatalf("筛选绑定值或顺序不正确：%v", args)
	}
	for _, part := range []string{
		"strpos(lower(t.phone),lower($3))>0",
		"strpos(lower(t.display_name||' '||t.remark_name),lower($4))>0",
		"strpos(lower(t.phone),lower($5))>0",
		"::text),lower($6))>0", "strpos(lower(g.name),lower($7))>0", "strpos(lower(cc.name),lower($8))>0",
	} {
		if !strings.Contains(query, " AND (") || !strings.Contains(query, part) {
			t.Fatalf("缺少组合参数占位符：%s", part)
		}
	}
	if strings.Contains(query, "' OR 1=1 --") {
		t.Fatal("筛选值不能拼入SQL")
	}
	_, emptyArgs, err := contactListQuery(httptest.NewRequest("GET", "/?name=+&phone=+&extra=+", nil), a)
	if err != nil || !reflect.DeepEqual(emptyArgs, []any{"member", true}) {
		t.Fatal("空白筛选不应加入查询")
	}
	for _, key := range []string{"query", "name", "phone", "channel", "extra", "tag", "category"} {
		for _, length := range []int{200, 201} {
			params := url.Values{key: {strings.Repeat("字", length)}}
			_, _, err := contactListQuery(httptest.NewRequest("GET", "/?"+params.Encode(), nil), a)
			if (err != nil) != (length > 200) {
				t.Fatalf("%s 应以中文字符计算200字上限：%d", key, length)
			}
		}
	}
}

// TestContactCombinedFieldSearch 验证六字段联合匹配、条件取交集和旧关键词兼容，数据只写入独立测试schema。
func TestContactCombinedFieldSearch(t *testing.T) {
	f := newFixture(t)
	_, contact, _ := f.visitor("13876543210")
	_, second, _ := f.visitor("13976543210")
	category, tagCategory, tag := newID(), newID(), newID()
	for _, step := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO contact_categories(id,tenant_id,name) VALUES($1,$2,'重点分类')", []any{category, f.tenant}},
		{"INSERT INTO tag_categories(id,tenant_id,name) VALUES($1,$2,'测试标签类')", []any{tagCategory, f.tenant}},
		{"INSERT INTO tags(id,tenant_id,category_id,name) VALUES($1,$2,$3,'意向标签')", []any{tag, f.tenant, tagCategory}},
		{"INSERT INTO contact_tags(tenant_id,contact_id,tag_id) VALUES($1,$2,$3)", []any{f.tenant, contact, tag}},
		{`UPDATE contacts SET display_name='联合测试甲',category_id=$2,attributes='{"产品":"测试产品100%"}',last_source='{"channel_code":"test-channel"}' WHERE id=$1`, []any{contact, category}},
		{"UPDATE contacts SET display_name='联合测试乙' WHERE id=$1", []any{second}},
	} {
		if _, err := f.admin.Exec(t.Context(), step.query, step.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		params url.Values
		count  int
	}{
		{"六字段", url.Values{"name": {" 甲 "}, "phone": {"138"}, "channel": {"channel"}, "extra": {"产品100%"}, "tag": {"意向"}, "category": {"重点"}}, 1},
		{"同名", url.Values{"name": {"联合测试"}}, 2},
		{"条件交集", url.Values{"name": {"甲"}, "phone": {"139"}}, 0},
		{"旧关键词兼容", url.Values{"query": {"138"}, "search_field": {"phone"}, "name": {"甲"}}, 1},
		{"旧关键词交集", url.Values{"query": {"139"}, "search_field": {"phone"}, "name": {"甲"}}, 0},
		{"忽略空白", url.Values{"name": {" "}, "phone": {" "}}, 2},
		{"百分号字面匹配", url.Values{"phone": {"%"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, reply := f.request("GET", "/api/v1/tenant/contacts?"+tc.params.Encode(), f.owner, "", nil)
			items := mustData(t, status, reply)["items"].([]any)
			if len(items) != tc.count {
				t.Fatalf("组合筛选应返回%d条，实际%d条", tc.count, len(items))
			}
		})
	}
	status, reply := f.request("GET", "/api/v1/tenant/contacts?name="+url.QueryEscape("联合测试"), f.otherOwner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("组合字段搜索不能跨租户")
	}
}

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
		for _, filter := range []string{"search_field=extra&query=", "name=" + url.QueryEscape("集成测试访客") + "&extra="} {
			query := "/api/v1/tenant/contacts?" + filter + url.QueryEscape(value)
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
