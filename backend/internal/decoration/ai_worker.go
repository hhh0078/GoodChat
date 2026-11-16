// 文件作用说明：使用无载荷调度目录和租户RLS事务恢复AI任务，迟到、取消或并发编辑均保留有效草稿。
package decoration

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// Start 启动最多两个并发模型调用，持久租约防止重启后静默重复计费。
func (s *Service) Start(ctx context.Context) {
	s.startAssistant(ctx)
	go func() {
		slots := make(chan struct{}, 2)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case slots <- struct{}{}:
				default:
					continue
				}
				tenant, id, err := s.claimAIJob(ctx)
				if err != nil {
					<-slots
					if ctx.Err() == nil {
						slog.Error("AI任务调度失败", "error", err)
					}
					continue
				}
				if id == "" {
					<-slots
					continue
				}
				go func() {
					defer func() { <-slots }()
					if err := s.processAIJob(ctx, tenant, id); err != nil && ctx.Err() == nil {
						slog.Error("AI设计任务状态保存失败", "job_id", id, "error", err)
					}
				}()
			}
		}
	}()
}

// claimAIJob 只从全局调度索引认领随机标识，不在无租户上下文读取需求载荷。
func (s *Service) claimAIJob(ctx context.Context) (string, string, error) {
	var tenant, id string
	err := s.pool.QueryRow(ctx, `WITH next AS(SELECT job_id FROM ai_design_queue WHERE available_at<=now() ORDER BY available_at,job_id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE ai_design_queue q SET available_at=now()+interval '150 seconds' FROM next WHERE q.job_id=next.job_id RETURNING q.tenant_id::text,q.job_id::text`).Scan(&tenant, &id)
	if err == pgx.ErrNoRows {
		return "", "", nil
	}
	return tenant, id, err
}

// processAIJob 获取租户载荷后调用模型，租约失效的已调用任务失败并标记费用待核对。
func (s *Service) processAIJob(ctx context.Context, tenant, id string) error {
	var mode, prompt, model, status string
	var base json.RawMessage
	run := false
	err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		var expired bool
		err := tx.QueryRow(ctx, `SELECT j.status,j.render_mode,j.prompt,j.model,t.design,coalesce(j.lease_until<now(),false) FROM ai_design_jobs j JOIN tenant_templates t ON t.id=j.template_id AND t.tenant_id=j.tenant_id WHERE j.id=$1 FOR UPDATE OF j`, id).Scan(&status, &mode, &prompt, &model, &base, &expired)
		if err != nil {
			return err
		}
		if status == "running" || status == "validating" {
			if expired {
				return s.failAIJob(ctx, tx, id, "ai_interrupted", "模型调用中断，供应商用量待核对；为避免重复计费未自动重试", providerResult{Billing: "uncertain", Usage: json.RawMessage(`{}`)})
			}
			return nil
		}
		if status != "queued" {
			_, err := tx.Exec(ctx, "DELETE FROM ai_design_queue WHERE job_id=$1", id)
			return err
		}
		if err := s.jobPermission(ctx, tx, tenant, id, mode); err != nil {
			return s.failPermission(ctx, tx, id, err, providerResult{Billing: "not_called", Usage: json.RawMessage(`{}`)})
		}
		if !s.aiReady() {
			return s.failAIJob(ctx, tx, id, "ai_unavailable", "平台模型配置当前不可用", providerResult{Billing: "not_called", Usage: json.RawMessage(`{}`)})
		}
		if s.cfg.AIDailyLimit <= 0 {
			return s.failAIJob(ctx, tx, id, "ai_daily_disabled", "平台已禁用AI设计提交，排队任务未调用模型", providerResult{Billing: "not_called", Usage: json.RawMessage(`{}`)})
		}
		if _, err := tx.Exec(ctx, "UPDATE ai_design_jobs SET status='running',started_at=now(),lease_until=now()+interval '120 seconds',billing_status='uncertain' WHERE id=$1", id); err != nil {
			return err
		}
		run = true
		return nil
	})
	if err != nil || !run {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	result, callErr := s.callProvider(callCtx, mode, prompt, model, base)
	return platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		var current, templateID string
		var version int64
		if err := tx.QueryRow(ctx, "SELECT status,template_id::text,base_version FROM ai_design_jobs WHERE id=$1 FOR UPDATE", id).Scan(&current, &templateID, &version); err != nil {
			return err
		}
		if current == "cancelled" {
			_, err := tx.Exec(ctx, "UPDATE ai_design_jobs SET usage=$2,billing_status=$3 WHERE id=$1", id, result.Usage, result.Billing)
			return err
		}
		if current != "running" {
			return nil
		}
		if callErr != nil {
			code, message := "ai_failed", "模型生成失败，原草稿已保留"
			var api *platform.APIError
			if errors.As(callErr, &api) {
				code, message = api.Code, api.Message
			}
			return s.failAIJob(ctx, tx, id, code, message, result)
		}
		if err := s.jobPermission(ctx, tx, tenant, id, mode); err != nil {
			return s.failPermission(ctx, tx, id, err, result)
		}
		if _, err := tx.Exec(ctx, "UPDATE ai_design_jobs SET status='validating' WHERE id=$1", id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE tenant_templates SET design=$3,version=version+1,status='ready',updated_at=now() WHERE id=$1 AND version=$2", templateID, version, result.Design)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE ai_design_jobs SET status='succeeded',result=$2,applied=$3,usage=$4,billing_status=$5,completed_at=now(),lease_until=NULL WHERE id=$1", id, result.Design, tag.RowsAffected() == 1, result.Usage, result.Billing); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM ai_design_queue WHERE job_id=$1", id)
		return err
	})
}

// failAIJob 保存可追踪失败、实际用量和原始草稿，不制造成功结果。
func (s *Service) failAIJob(ctx context.Context, tx pgx.Tx, id, code, message string, result providerResult) error {
	if len(result.Usage) == 0 {
		result.Usage = json.RawMessage(`{}`)
	}
	if result.Billing == "" {
		result.Billing = "uncertain"
	}
	if _, err := tx.Exec(ctx, "UPDATE ai_design_jobs SET status='failed',error_code=$2,error_message=$3,usage=$4,billing_status=$5,result=coalesce($6::jsonb,result),completed_at=now(),lease_until=NULL WHERE id=$1", id, code, message, result.Usage, result.Billing, result.Design); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE tenant_templates t SET status='draft' FROM ai_design_jobs j WHERE j.id=$1 AND t.id=j.template_id AND t.tenant_id=j.tenant_id AND t.version=j.base_version AND t.status='generating'", id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM ai_design_queue WHERE job_id=$1", id)
	return err
}

// jobPermission 锁定租户及任务创建者身份，并重新校验有效管理角色与当前套餐权益。
func (s *Service) jobPermission(ctx context.Context, tx pgx.Tx, tenant, id, mode string) error {
	var active, authorized bool
	err := tx.QueryRow(ctx, `SELECT t.status='active',m.role IN('owner','admin') AND NOT m.disabled AND m.removed_at IS NULL FROM ai_design_jobs j JOIN tenants t ON t.id=j.tenant_id JOIN tenant_memberships m ON m.id=j.created_by AND m.tenant_id=j.tenant_id WHERE j.id=$1 FOR SHARE OF t,m`, id).Scan(&active, &authorized)
	if err == pgx.ErrNoRows || err == nil && (!active || !authorized) {
		return platform.Fail(403, "ai_permission_changed", "租户状态或创建者管理权限已变化，AI结果不会覆盖草稿")
	}
	if err != nil {
		return err
	}
	if err := s.checkFeature(ctx, tx, tenant, "ai_design"); err != nil {
		return err
	}
	return s.checkFeature(ctx, tx, tenant, decorationFeature(mode))
}

// failPermission 仅将安全业务错误记为任务失败，数据库暂时错误保留任务供恢复。
func (s *Service) failPermission(ctx context.Context, tx pgx.Tx, id string, err error, result providerResult) error {
	var api *platform.APIError
	if !errors.As(err, &api) {
		return err
	}
	return s.failAIJob(ctx, tx, id, api.Code, api.Message, result)
}
