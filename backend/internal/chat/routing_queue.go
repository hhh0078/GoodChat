// 文件作用说明：在单池事务内回收预留与调度候选，避免同时持有多个池及共享客服锁造成交叉等待。
package chat

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// routePool 在独立池事务内执行恢复与一次分配，提交后才允许调度其他池。
func (s *Service) routePool(ctx context.Context, tx pgx.Tx, tenant, pool string) error {
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
	err = tx.QueryRow(ctx, "SELECT c.id::text FROM conversations c JOIN access_links l ON l.id=c.created_entry_id AND l.tenant_id=c.tenant_id JOIN routing_pools p ON p.id=c.routing_pool_id AND p.tenant_id=c.tenant_id WHERE c.routing_pool_id=$1 AND c.status='queued' AND c.routing_attempts<3 AND (COALESCE(c.source_snapshot->'entry'->>'reception_mode',l.reception_mode)='auto' OR (COALESCE(c.source_snapshot->'entry'->>'reception_mode',l.reception_mode)='inherit' AND p.algorithm<>'manual')) ORDER BY c.priority DESC,c.created_at,c.id LIMIT 1 FOR UPDATE OF c", pool).Scan(&conversation)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.assignLocked(ctx, tx, tenant, conversation, "", false); err != nil {
		return err
	}
	return nil
}
