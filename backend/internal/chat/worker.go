// 文件作用说明：恢复持久outbox任务，调度排队会话并回收过期接单预留。
package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// Start 启动可取消的持久任务调度器，不依赖内存定时器保存任务事实。
func (s *Service) Start(ctx context.Context) {
	s.startExportWorker(ctx)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.tick(ctx); err != nil && ctx.Err() == nil {
					slog.Error("客服后台任务暂未完成", "error", err)
				}
			}
		}
	}()
}

// tick 从全局租户目录逐租户进入RLS事务，避免无上下文读取业务表。
func (s *Service) tick(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, "SELECT id::text FROM tenants WHERE status='active'")
	if err != nil {
		return err
	}
	tenants := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		tenants = append(tenants, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, tenant := range tenants {
		if err := s.routeQueues(ctx, tenant); err != nil {
			return err
		}
		if err := s.deliverOutbox(ctx, tenant); err != nil {
			return err
		}
		if err := s.runAutomatic(ctx, tenant); err != nil {
			return err
		}
	}
	return nil
}

// routeQueues 读取租户池目录，每池使用独立事务，避免跨池等待时继续持有共享客服容量锁。
func (s *Service) routeQueues(ctx context.Context, tenant string) error {
	pools := []string{}
	err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT id::text FROM routing_pools WHERE NOT archived ORDER BY id")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			pools = append(pools, id)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, pool := range pools {
		if err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error { return s.routePool(ctx, tx, tenant, pool) }); err != nil {
			return err
		}
	}
	return nil
}

// outboxItem 保存领取后的投递任务及本次租约版本。
type outboxItem struct {
	id      string
	attempt int
	event   map[string]any
}

// deliverOutbox 提交认领事务后再投递，完成标记受本次尝试编号保护。
func (s *Service) deliverOutbox(ctx context.Context, tenant string) error {
	items := []outboxItem{}
	err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "UPDATE outbox_jobs SET lease_until=now()+interval '30 seconds',attempts=attempts+1 WHERE id IN(SELECT id FROM outbox_jobs WHERE completed_at IS NULL AND (lease_until IS NULL OR lease_until<now()) ORDER BY created_at,id LIMIT 100 FOR UPDATE SKIP LOCKED) RETURNING id::text,attempts,event_id::text")
		if err != nil {
			return err
		}
		type claim struct {
			id, event string
			attempt   int
		}
		claims := []claim{}
		for rows.Next() {
			var c claim
			if err := rows.Scan(&c.id, &c.attempt, &c.event); err != nil {
				rows.Close()
				return err
			}
			claims = append(claims, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range claims {
			event, err := txJSON(ctx, tx, "SELECT to_jsonb(e)||jsonb_build_object('seq',seq::text,'protocol_version',1) FROM conversation_events e WHERE id=$1", c.event)
			if err != nil {
				return err
			}
			items = append(items, outboxItem{c.id, c.attempt, event})
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, item := range items {
		conversation, _ := item.event["conversation_id"].(string)
		audience, _ := item.event["audience"].(string)
		s.broadcast(ctx, tenant, conversation, audience, map[string]any{"type": "event", "data": item.event})
		if err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE outbox_jobs SET completed_at=now(),lease_until=NULL WHERE id=$1 AND attempts=$2 AND completed_at IS NULL", item.id, item.attempt)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
