// 文件作用说明：验证平台实时人数按身份去重，多个标签和跨租户同客服不冒充更多在线人数。
package chat

import "testing"

// TestRealtimeMonitoringDeduplicatesPeople 验证连接数与在线人数区分且不返回任何原始身份编号。
func TestRealtimeMonitoringDeduplicatesPeople(t *testing.T) {
	s := &Service{clients: map[*liveClient]struct{}{}}
	for _, a := range []actor{{TenantID: "tenant-a", MemberID: "member-a", UserID: "user-a"}, {TenantID: "tenant-b", MemberID: "member-b", UserID: "user-a"}, {TenantID: "tenant-a", MemberID: "member-c", UserID: "user-b"}, {TenantID: "tenant-a", ContactID: "visitor-a"}, {TenantID: "tenant-a", ContactID: "visitor-a"}} {
		s.clients[&liveClient{actor: a}] = struct{}{}
	}
	stats := s.RealtimeStats()
	if stats["connections"] != 5 || stats["online_agents"] != 2 || stats["online_visitors"] != 1 || stats["online_people"] != 3 || stats["online_tenants"] != 2 {
		t.Fatal("在线身份与标签页连接未正确去重")
	}
}
