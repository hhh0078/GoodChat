// 文件作用说明：向平台监控提供本实例实时连接的去重计数，不导出任何参与者编号或会话内容。
package chat

// RealtimeStats 在连接锁内取得快照并在锁外去重，避免平台采样长时间阻塞消息广播。
// 多个客服标签页只计一个在线账号，参与者身份在连接创建后保持不变。
func (s *Service) RealtimeStats() map[string]int64 {
	s.mu.Lock()
	clients := make([]*liveClient, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.Unlock()
	agents, visitors, tenants := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, client := range clients {
		a := client.actor
		tenants[a.TenantID] = true
		if a.ContactID != "" {
			visitors[a.TenantID+":"+a.ContactID] = true
		} else {
			key := a.UserID
			if key == "" {
				key = a.TenantID + ":" + a.MemberID
			}
			agents[key] = true
		}
	}
	return map[string]int64{"connections": int64(len(clients)), "online_agents": int64(len(agents)), "online_visitors": int64(len(visitors)), "online_people": int64(len(agents) + len(visitors)), "online_tenants": int64(len(tenants))}
}
