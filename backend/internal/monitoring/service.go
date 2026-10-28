// 文件作用说明：仅向总管理员提供真实资源、实时连接和接待容量汇总，不开放租户资料或操作系统命令。
package monitoring

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// Service 保存平台监控依赖，实时连接读取器只返回无身份的计数。
type Service struct {
	pool        *pgxpool.Pool
	auth        platform.Authorizer
	sampler     *Sampler
	connections func() map[string]int64
}

// New 创建受平台角色保护的监控服务，资源范围由采样来源明确标注。
func New(pool *pgxpool.Pool, auth platform.Authorizer, connections func() map[string]int64) *Service {
	return &Service{pool: pool, auth: auth, sampler: NewSampler(), connections: connections}
}

// Start 将采样循环绑定到应用生命周期，避免每个浏览器重复采样。
func (s *Service) Start(ctx context.Context) { s.sampler.Start(ctx) }

// Register 注册独立监控接口，普通成员和租户管理员不能读取服务器资源。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/platform/monitoring", s.auth.RequirePlatform(s.monitor))
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		mux.HandleFunc(method+" /api/v1/platform/monitoring/capacity", s.auth.RequirePlatform(s.capacity))
	}
}

// monitor 返回当前实例计量和纯汇总容量；没有压测基线时不伪造预计在线人数。
func (s *Service) monitor(w http.ResponseWriter, r *http.Request) {
	resources, history, uptime := s.sampler.Snapshot()
	connections := map[string]int64{}
	if s.connections != nil {
		connections = s.connections()
	}
	stats := s.pool.Stat()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	healthy := s.pool.Ping(ctx) == nil
	database := map[string]any{"healthy": healthy, "latency_ms": time.Since(started).Seconds() * 1000, "acquired": stats.AcquiredConns(), "idle": stats.IdleConns(), "total": stats.TotalConns(), "maximum": stats.MaxConns()}
	var available, total, load, remaining int64
	err := s.pool.QueryRow(ctx, "SELECT available_agents,total_capacity,current_load,remaining FROM platform_reception_capacity()").Scan(&available, &total, &load, &remaining)
	reception := map[string]any{"available": err == nil}
	if err == nil {
		reception["available_agents"], reception["total_capacity"], reception["current_load"], reception["remaining"] = available, total, load, remaining
	}
	platform.JSON(w, 200, map[string]any{"sampled_at": resources.At, "resources": resources, "history": history, "uptime_seconds": uptime, "realtime": connections, "database": database, "reception": reception, "capacity": s.capacityState(ctx, resources, connections["online_people"]), "scope_note": "资源来自当前进程所属控制组（包含同组进程）或Go进程；实时人数按当前单实例聊天连接去重，不等于全部登录账号。物理宿主机未接入时不推断宿主机负载。"})
}
