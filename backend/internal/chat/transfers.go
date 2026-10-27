// 文件作用说明：仅向管理员、原接待客服和有效待转接目标返回待确认转接摘要，供工作台执行真实确认和拒绝。
package chat

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// addPendingTransfers 批量为已授权会话补充当前有效转接请求，不向访客或其他客服返回内部处理标识。
func addPendingTransfers(ctx context.Context, tx pgx.Tx, items []map[string]any, a actor) error {
	if a.ContactID != "" || len(items) == 0 {
		return nil
	}
	byID := map[string]map[string]any{}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		item["pending_transfer"] = nil
		if id, ok := item["id"].(string); ok {
			ids = append(ids, id)
			byID[id] = item
		}
	}
	rows, err := tx.Query(ctx, `SELECT conversation_id::text,jsonb_build_object('id',id,'from_agent_id',from_agent_id,'target_agent_id',target_agent_id,'target_member_id',target_agent_id,'status',status,'expires_at',expires_at,'created_at',created_at)
 FROM transfer_requests WHERE conversation_id=ANY($1::uuid[]) AND status='pending' AND expires_at>statement_timestamp() AND ($2 OR from_agent_id=NULLIF($3,'')::uuid OR target_agent_id=NULLIF($3,'')::uuid)`, ids, isAdmin(a), a.MemberID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var pending map[string]any
		if err := rows.Scan(&id, &pending); err != nil {
			return err
		}
		if item := byID[id]; item != nil {
			item["pending_transfer"] = pending
		}
	}
	return rows.Err()
}

// conversationJSONForActor 在会话快照上增加当前成员可操作的转接摘要，基础访客投影保持精简。
func conversationJSONForActor(ctx context.Context, tx pgx.Tx, id string, a actor) (map[string]any, error) {
	item, err := conversationJSON(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := addPendingTransfers(ctx, tx, []map[string]any{item}, a); err != nil {
		return nil, err
	}
	return item, nil
}
