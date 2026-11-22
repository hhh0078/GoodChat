// 文件作用说明：向租户授权管理员提醒真实待补充知识，按待办编号去重并在发送前重新检查处理状态。
package notifications

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// scanKnowledgeGaps 仅扫描启用服务提醒后的新待办，重复发现不重复发信。
func (s *Service) scanKnowledgeGaps(ctx context.Context, tx pgx.Tx, tenant, name string, installed time.Time) error {
	rows, err := tx.Query(ctx, `SELECT p.id::text,p.question,p.reason FROM ai_pending_items p JOIN notification_settings n ON n.tenant_id=p.tenant_id WHERE p.tenant_id=$1 AND n.service_enabled AND p.status IN('pending','supplementing') AND p.created_at>=$2 AND NOT EXISTS(SELECT 1 FROM notification_jobs j WHERE j.tenant_id=p.tenant_id AND j.kind='knowledge_gap' AND j.event_key=p.tenant_id::text||':knowledge-gap:'||p.id::text) ORDER BY p.created_at,p.id LIMIT 100`, tenant, installed)
	if err != nil {
		return err
	}
	type item struct{ id, question, reason string }
	items := []item{}
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.question, &i.reason); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, i := range items {
		if err = enqueueAdmins(ctx, tx, tenant, name, "knowledge_gap", "knowledge-gap:"+i.id, map[string]string{"reference": i.id, "question": i.question, "reason": i.reason}); err != nil {
			return err
		}
	}
	return nil
}
