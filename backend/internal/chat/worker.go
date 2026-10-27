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

// routeQueues 先锁池再选择最早会话，回收超时预留并限制每轮重试次数。
func (s *Service) routeQueues(ctx context.Context, tenant string) error {
	return platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT id::text FROM routing_pools WHERE NOT archived ORDER BY id")
		if err != nil {
			return err
		}
		pools := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			pools = append(pools, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, pool := range pools {
			if _, err := tx.Exec(ctx, "SELECT id FROM routing_pools WHERE id=$1 FOR UPDATE", pool); err != nil {
				return err
			}
			expiredRows, err := tx.Query(ctx, "SELECT c.id::text,c.current_agent_id::text FROM conversations c JOIN conversation_assignments a ON a.id=c.current_assignment_id AND a.tenant_id=c.tenant_id WHERE c.routing_pool_id=$1 AND c.status='assigned' AND a.expires_at<now() ORDER BY c.id FOR UPDATE OF c", pool)
			if err != nil {
				return err
			}
			type expired struct{ id, agent string }
			expiredItems := []expired{}
			for expiredRows.Next() {
				var item expired
				if err := expiredRows.Scan(&item.id, &item.agent); err != nil {
					expiredRows.Close()
					return err
				}
				expiredItems = append(expiredItems, item)
			}
			expiredRows.Close()
			if err := expiredRows.Err(); err != nil {
				return err
			}
			for _, item := range expiredItems {
				if err := releaseHolds(ctx, tx, item.id, item.agent); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "UPDATE conversation_assignments SET released_at=now() WHERE conversation_id=$1 AND released_at IS NULL", item.id); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "UPDATE conversations SET status='queued',current_agent_id=NULL,current_assignment_id=NULL,rejected_agents=rejected_agents||jsonb_build_object($2::text,true),version=version+1 WHERE id=$1", item.id, item.agent); err != nil {
					return err
				}
				if _, err := s.emit(ctx, tx, tenant, item.id, "assignment.expired", "public", map[string]any{"status": "queued"}); err != nil {
					return err
				}
			}
			transferRows, err := tx.Query(ctx, "SELECT t.id::text,t.conversation_id::text,t.target_agent_id::text FROM transfer_requests t JOIN conversations c ON c.id=t.conversation_id AND c.tenant_id=t.tenant_id WHERE c.routing_pool_id=$1 AND t.status='pending' AND t.expires_at<now() ORDER BY t.conversation_id", pool)
			if err != nil {
				return err
			}
			type expiredTransfer struct{ id, conversation, agent string }
			transfers := []expiredTransfer{}
			for transferRows.Next() {
				var v expiredTransfer
				if err := transferRows.Scan(&v.id, &v.conversation, &v.agent); err != nil {
					transferRows.Close()
					return err
				}
				transfers = append(transfers, v)
			}
			transferRows.Close()
			if err := transferRows.Err(); err != nil {
				return err
			}
			for _, v := range transfers {
				if _, err := tx.Exec(ctx, "SELECT id FROM conversations WHERE id=$1 FOR UPDATE", v.conversation); err != nil {
					return err
				}
				if err := releaseHolds(ctx, tx, v.conversation, v.agent); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "UPDATE transfer_requests SET status='expired' WHERE id=$1 AND status='pending'", v.id); err != nil {
					return err
				}
				if _, err := s.emit(ctx, tx, tenant, v.conversation, "transfer.expired", "public", map[string]any{"transfer_request_id": v.id}); err != nil {
					return err
				}
			}
			var conversation string
			err = tx.QueryRow(ctx, "SELECT id::text FROM conversations WHERE routing_pool_id=$1 AND status='queued' AND routing_attempts<3 ORDER BY priority DESC,created_at,id LIMIT 1 FOR UPDATE", pool).Scan(&conversation)
			if err == pgx.ErrNoRows {
				continue
			}
			if err != nil {
				return err
			}
			if err := s.assignLocked(ctx, tx, tenant, conversation, "", false); err != nil {
				return err
			}
		}
		return nil
	})
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
