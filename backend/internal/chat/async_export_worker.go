// 文件作用说明：以持久租约生成异步CSV，报告真实行数、检查取消和权限，24小时到期后清理临时结果。
package chat

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// startExportWorker 启动唯一应用内串行导出循环，停止后靠持久租约恢复未完成任务。
func (s *Service) startExportWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		round := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.claimExport(ctx)
				round++
				if round%30 == 0 {
					s.cleanupExports(ctx)
				}
			}
		}
	}()
}

// claimExport 从无业务载荷的全局索引原子认领任务，世代令牌防止过期worker覆写。
func (s *Service) claimExport(ctx context.Context) bool {
	var id, tenant, lease string
	err := s.pool.QueryRow(ctx, `UPDATE chat_export_dispatch SET lease_until=now()+interval '3 minutes',lease_token=gen_random_uuid() WHERE job_id=(SELECT job_id FROM chat_export_dispatch WHERE lease_until IS NULL OR lease_until<now() ORDER BY created_at,job_id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING job_id::text,tenant_id::text,lease_token::text`).Scan(&id, &tenant, &lease)
	if err != nil {
		return false
	}
	s.generateExport(ctx, id, tenant, lease)
	return true
}

// generateExport 在受限租户查询中生成CSV，最后重新授权才把完整结果写入数据库。
func (s *Service) generateExport(parent context.Context, id, tenant, lease string) {
	ctx, done := context.WithTimeout(parent, 120*time.Second)
	defer done()
	var a actor
	var filters string
	running := false
	err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", tenant); err != nil {
			return err
		}
		owned, err := tx.Exec(ctx, "UPDATE chat_export_dispatch SET lease_until=now()+interval '3 minutes' WHERE job_id=$1 AND lease_token=$2 AND lease_until>now()", id, lease)
		if err != nil {
			return err
		}
		if owned.RowsAffected() != 1 {
			return nil
		}
		var state string
		var valid bool
		err = tx.QueryRow(ctx, "SELECT j.filters,j.status,j.expires_at>now(),m.id::text,m.user_id::text,m.role,m.can_serve FROM chat_export_jobs j JOIN tenant_memberships m ON m.id=j.requested_by AND m.tenant_id=j.tenant_id WHERE j.id=$1 FOR UPDATE OF j", id).Scan(&filters, &state, &valid, &a.MemberID, &a.UserID, &a.Role, &a.CanServe)
		if err != nil {
			return err
		}
		a.TenantID = tenant
		if !valid || !inStrings(state, "queued", "running") {
			_, err = tx.Exec(ctx, "DELETE FROM chat_export_dispatch WHERE job_id=$1 AND lease_token=$2", id, lease)
			return err
		}
		// 先给认领世代，再做权限判断；权限撤销会以明确失败结束而非无限重试。
		if _, err = tx.Exec(ctx, "UPDATE chat_export_jobs SET status='running',row_count=0,started_at=now(),lease_token=$2 WHERE id=$1", id, lease); err != nil {
			return err
		}
		running = true
		return nil
	})
	if err != nil || !running {
		return
	}
	var body []byte
	var rows int
	var zone string
	var withdrawn int64
	err = platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if !isAdmin(a) {
			return platform.Fail(403, "admin_required", "申请者的管理员权限已撤销")
		}
		// shortcut: 业务消息目前只软撤回，数量单调；增加物理清理前必须改为单调撤回版本。
		if query, _ := url.ParseQuery(filters); query.Get("include_messages") == "true" {
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM messages WHERE withdrawn_at IS NOT NULL").Scan(&withdrawn); err != nil {
				return err
			}
		}
		request := &http.Request{URL: &url.URL{RawQuery: filters}}
		request = request.WithContext(ctx)
		var err error
		body, rows, zone, err = renderExportCSV(ctx, tx, request, a, asyncExportRows, asyncExportBytes, func(count int) error {
			return platform.TenantTx(ctx, s.pool, tenant, func(progress pgx.Tx) error {
				result, err := progress.Exec(ctx, "UPDATE chat_export_jobs SET row_count=$3 WHERE id=$1 AND lease_token=$2 AND status='running' AND expires_at>now()", id, lease, count)
				if err != nil {
					return err
				}
				if result.RowsAffected() != 1 {
					return platform.Fail(409, "export_cancelled", "任务已取消、过期或由新worker接管")
				}
				return nil
			})
		})
		return err
	})
	// 生成事务不能长期锁定租户，否则取消操作会等待整份CSV完成。发布阶段才检查套餐锁。
	finishCtx, finishDone := context.WithTimeout(parent, 10*time.Second)
	defer finishDone()
	_ = s.finishExport(finishCtx, id, tenant, lease, a, filters, body, rows, zone, withdrawn, err)
}

// finishExport 在最后发布事务锁定当前调度世代，再检查权限与取消状态，旧worker不能赶在新worker初始化之前覆盖结果。
func (s *Service) finishExport(finishCtx context.Context, id, tenant, lease string, a actor, filters string, body []byte, rows int, zone string, withdrawn int64, err error) error {
	return platform.TenantTx(finishCtx, s.pool, tenant, func(tx pgx.Tx) error {
		if _, lockErr := tx.Exec(finishCtx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", tenant); lockErr != nil {
			return lockErr
		}
		owned, lockErr := tx.Exec(finishCtx, "UPDATE chat_export_dispatch SET lease_until=now()+interval '3 minutes' WHERE job_id=$1 AND lease_token=$2 AND lease_until>now()", id, lease)
		if lockErr != nil {
			return lockErr
		}
		if owned.RowsAffected() != 1 {
			return nil
		}
		if permission := s.exportPermission(finishCtx, tx, a); permission != nil {
			var typed *platform.APIError
			if !errors.As(permission, &typed) {
				return permission
			}
			err = permission
		}
		var state string
		var current *string
		var valid bool
		if readErr := tx.QueryRow(finishCtx, "SELECT status,lease_token::text,expires_at>now() FROM chat_export_jobs WHERE id=$1 FOR UPDATE", id).Scan(&state, &current, &valid); readErr != nil {
			return readErr
		}
		if state != "running" || current == nil || *current != lease {
			_, deleteErr := tx.Exec(finishCtx, "DELETE FROM chat_export_dispatch WHERE job_id=$1 AND lease_token=$2", id, lease)
			return deleteErr
		}
		if !valid {
			err = platform.Fail(410, "export_expired", "导出任务已经过期，请重新创建")
		}
		status, code, message := "completed", "", ""
		if err != nil {
			status = "failed"
			code = "export_failed"
			message = "导出未完成，请重新创建任务或缩小筛选范围"
			var typed *platform.APIError
			if errors.As(err, &typed) {
				code, message = typed.Code, typed.Message
			}
			body = nil
		}
		// shortcut: 当前单应用保留最多64MiB的24小时bytea结果；结果量持续增长时迁移到私有对象存储。
		if _, writeErr := tx.Exec(finishCtx, "UPDATE chat_export_jobs SET status=$3,csv_body=$4,row_count=$5,timezone=$6,size_bytes=$7,error_code=$8,error_message=$9,withdrawn_count=$10,completed_at=now() WHERE id=$1 AND lease_token=$2", id, lease, status, body, rows, zone, len(body), code, message, withdrawn); writeErr != nil {
			return writeErr
		}
		if _, deleteErr := tx.Exec(finishCtx, "DELETE FROM chat_export_dispatch WHERE job_id=$1 AND lease_token=$2", id, lease); deleteErr != nil {
			return deleteErr
		}
		return audit(finishCtx, tx, a, "chat_export."+status, id, map[string]any{"rows": rows, "bytes": len(body), "filter_hash": hash(filters)})
	})
}

// cleanupExports 仅在逐租户事务中清理到期结果，调度索引始终不接触客户内容。
func (s *Service) cleanupExports(ctx context.Context) {
	tenants, err := s.pool.Query(ctx, "SELECT id::text FROM tenants")
	if err != nil {
		return
	}
	ids := []string{}
	for tenants.Next() {
		var id string
		if tenants.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	tenants.Close()
	for _, tenant := range ids {
		_ = platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", tenant); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE chat_export_jobs SET csv_body=NULL,status=CASE WHEN status IN('queued','running') THEN 'cancelled' ELSE status END WHERE expires_at<=now() AND (csv_body IS NOT NULL OR status IN('queued','running'))"); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "DELETE FROM chat_export_dispatch q USING chat_export_jobs j WHERE q.job_id=j.id AND j.expires_at<=now()")
			return err
		})
	}
}
