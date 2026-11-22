// 文件作用说明：验证专用WSS及网页来源配置的生产安全约束。
package platform

import "testing"

// TestRealtimeConfig 验证生产配置、旧部署回退及非法配置拒绝启动。
func TestRealtimeConfig(t *testing.T) {
	endpoint, origins, e := realtimeConfig("wss://socket.example/", "https://main.example", "https://alias.example,https://main.example", true)
	if e != nil || endpoint != "wss://socket.example" || len(origins) != 2 {
		t.Fatal("专用端点解析失败")
	}
	endpoint, _, e = realtimeConfig("", "http://localhost:13000", "", false)
	if e != nil || endpoint != "" {
		t.Fatal("旧配置失效")
	}
	for _, value := range []string{"ws://socket.example", "https://socket.example", "wss://user@socket.example", "wss://socket.example/path", "wss://socket.example?token=x"} {
		if _, _, e := realtimeConfig(value, "https://main.example", "", true); e == nil {
			t.Fatal("非法端点被接受", value)
		}
	}
	if _, _, e := realtimeConfig("wss://socket.example", "https://main.example", "https://evil.example/path", true); e == nil {
		t.Fatal("非法网页来源被接受")
	}
}
