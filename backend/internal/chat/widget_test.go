// 文件作用说明：验证悬浮窗输入边界、配置并发保存、租户隔离及公开脚本禁用行为。
package chat

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

// TestWidgetHours 使用实际嵌入脚本的时段计算，覆盖企业时区、跨午夜、星期和全时段。
func TestWidgetHours(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要Node运行JS时段检查")
	}
	start := strings.Index(widgetScript, "function withinHours(")
	end := strings.Index(widgetScript, "\n/** openChat")
	if start < 0 || end <= start {
		t.Fatal("未找到实际悬浮窗时段函数")
	}
	script := `const assert=require('node:assert/strict');let config={schedule_enabled:true,timezone:'Asia/Shanghai',start:'22:00',end:'02:00',weekdays:[1]};` + widgetScript[start:end] + `
assert.equal(withinHours(new Date('2026-11-16T15:00:00Z')),true);
assert.equal(withinHours(new Date('2026-11-16T17:00:00Z')),true);
assert.equal(withinHours(new Date('2026-11-16T19:00:00Z')),false);
assert.equal(withinHours(new Date('2026-11-17T15:00:00Z')),false);
config.start='00:00';config.end='23:59';config.weekdays=[0,1,2,3,4,5,6];
assert.equal(withinHours(new Date('2026-11-16T16:00:00Z')),true);
config.schedule_enabled=false;config.weekdays=[];assert.equal(withinHours(),true);`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("悬浮窗时段计算错误: %s", out)
	}
}

// TestWidgetValidation 防止不合法尺寸、时段和图标脚本进入跨站展示脚本。
func TestWidgetValidation(t *testing.T) {
	if err := validateWidgetSettings(defaultWidgetSettings()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*widgetSettings){
		func(s *widgetSettings) { s.Color = "red;display:none" },
		func(s *widgetSettings) { s.LogoURL = "javascript:alert(1)" },
		func(s *widgetSettings) { s.LogoURL = "https://user:password@example.com/a.png" },
		func(s *widgetSettings) { s.Width = 0 },
		func(s *widgetSettings) { s.ShowDelay = -1 },
		func(s *widgetSettings) { s.AutoOpenDelay = -2 },
		func(s *widgetSettings) { s.Start = "25:00" },
		func(s *widgetSettings) { s.Weekdays = []int{1, 1} },
	} {
		value := defaultWidgetSettings()
		change(&value)
		if validateWidgetSettings(value) == nil {
			t.Fatal("非法悬浮窗设置被接受")
		}
	}
}

// TestWidgetTenantIsolationAndScript 使用真实数据库和HTTP检查版本冲突、写权限、停用及公开脚本。
func TestWidgetTenantIsolationAndScript(t *testing.T) {
	f := newFixture(t)
	var linkID string
	if err := f.admin.QueryRow(context.Background(), "SELECT id::text FROM access_links WHERE tenant_id=$1 AND entry_key=$2", f.tenant, f.entry).Scan(&linkID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/tenant/access-links/" + linkID + "/widget"
	status, reply := f.request("GET", path, f.owner, "", nil)
	settings := mustData(t, status, reply)
	settings["label"] = "官网客服</script>"
	settings["version"] = float64(1)
	status, reply = f.request("PUT", path, f.owner, "", settings)
	mustData(t, status, reply)
	status, _ = f.request("PUT", path, f.owner, "", settings)
	if status != 409 {
		t.Fatal("旧配置版本覆盖了新版本")
	}
	status, _ = f.request("PUT", path, f.agent, "", settings)
	if status != 403 {
		t.Fatal("普通客服写入了管理员配置")
	}
	status, _ = f.request("GET", path, f.otherOwner, "", nil)
	if status != 404 {
		t.Fatalf("跨租户读取配置:%d", status)
	}
	response, err := http.Get(f.server.URL + "/api/v1/widget/" + f.entry + ".js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(response.Header.Get("Content-Type"), "application/javascript") || !strings.Contains(string(body), `\u003c/script\u003e`) || !strings.Contains(string(body), "attachShadow") {
		t.Fatal("公开脚本格式或转义错误")
	}
	settings["version"] = float64(2)
	settings["enabled"] = false
	status, reply = f.request("PUT", path, f.owner, "", settings)
	mustData(t, status, reply)
	response, err = http.Get(f.server.URL + "/api/v1/widget/" + f.entry + ".js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(body), "widget disabled") || strings.Contains(string(body), "attachShadow") {
		t.Fatal("停用后仍加载悬浮窗")
	}
}
