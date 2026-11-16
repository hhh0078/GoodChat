// 文件作用说明：验证原生强制更新门禁、相同版本放行及检查失败不允许登录。
package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpdateGate 确保门禁在原生层生效，而不依赖前端是否展示更新弹窗。
func TestUpdateGate(t *testing.T) {
	latest := "3.0.0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data":{"release":{"version":%q,"description":"update"},"download_url":"https://example.com/app.zip"}}`, latest)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL, nil)
	s, _ := c.current()
	s.updateChecked = false
	if compatible(s) == nil {
		t.Fatal("未检查版本就允许业务请求")
	}
	value, err := c.CheckUpdate()
	if err != nil || !value.Required || compatible(s) == nil {
		t.Fatalf("未强制更新：%+v %v", value, err)
	}
	latest = "2.3.4"
	value, err = c.CheckUpdate()
	if err != nil || value.Required || compatible(s) != nil {
		t.Fatal("相同版本仍被拦截")
	}
	if err = c.SelectServer(server.URL); err != nil {
		t.Fatal(err)
	}
	s, _ = c.current()
	if compatible(s) == nil {
		t.Fatal("重新选择服务器未重新检查版本")
	}
}
