// 文件作用说明：在长时间图片调用期间持续续租，进程退出或任务终止后停止续租，防止静默重复调用计费阶段。
package decoration

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// assistantHeartbeat 按固定间隔续任务及调度目录的短租约，失去运行资格或无法续租时取消正在进行的供应商调用。
func (s *Service) assistantHeartbeat(ctx context.Context, tenant, id string, interval, lease time.Duration, cancel context.CancelFunc) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeatCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			active := false
			err := platform.TenantTx(heartbeatCtx, s.pool, tenant, func(tx pgx.Tx) error {
				tag, err := tx.Exec(heartbeatCtx, "UPDATE assistant_jobs SET lease_until=now()+($2::bigint*interval '1 millisecond') WHERE id=$1 AND status IN('running','validating')", id, lease.Milliseconds())
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 0 {
					return nil
				}
				active = true
				_, err = tx.Exec(heartbeatCtx, "UPDATE assistant_queue SET available_at=now()+($2::bigint*interval '1 millisecond') WHERE job_id=$1", id, (lease + 10*time.Second).Milliseconds())
				return err
			})
			stop()
			if err != nil || !active {
				cancel()
				return
			}
		}
	}
}
