// 文件作用说明：调度持久模板助手任务，逐阶段保存实际结果，取消、撤权和人工并发编辑均不会破坏已有效的模板。
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

// assistantWork 保存一个已认领任务的固定输入和当前进度，不把凭据写入任务表。
type assistantWork struct {
	ID, Tenant, Thread, Member, Source, Phase, BaseTemplate string
	ConfigVersion, ContractVersion, BaseVersion             int64
	BaseDesign                                              json.RawMessage
	Output                                                  assistantOutput
	Configuration                                           assistantConfiguration
	Foundation                                              assistantFoundation
	Principal                                               platform.Principal
}

// startAssistant 启动两路持久任务处理，过期的已调用任务只标记失败，不静默重试计费阶段。
func (s *Service) startAssistant(ctx context.Context) {
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
				var tenant, id string
				err := s.pool.QueryRow(ctx, `WITH next AS(SELECT job_id FROM assistant_queue WHERE available_at<=now() ORDER BY available_at,job_id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE assistant_queue q SET available_at=now()+interval '150 seconds' FROM next WHERE q.job_id=next.job_id RETURNING q.tenant_id::text,q.job_id::text`).Scan(&tenant, &id)
				if err != nil {
					<-slots
					if err != pgx.ErrNoRows && ctx.Err() == nil {
						slog.Error("模板助手调度失败")
					}
					continue
				}
				go func() {
					defer func() { <-slots }()
					if err := s.processAssistantJob(ctx, tenant, id); err != nil && ctx.Err() == nil {
						slog.Error("模板助手任务状态保存失败", "job_id", id)
					}
				}()
			}
		}
	}()
}

// assistantPermission 在调用及应用结果前重新检查租户、创建者身份和平台配置使用权限。
func (s *Service) assistantPermission(ctx context.Context, tx pgx.Tx, work *assistantWork) error {
	var active, authorized, platformAdmin bool
	err := tx.QueryRow(ctx, `SELECT t.status='active',m.role IN('owner','admin') AND NOT m.disabled AND m.removed_at IS NULL,u.is_platform_admin,u.id::text FROM assistant_jobs j JOIN tenants t ON t.id=j.tenant_id JOIN tenant_memberships m ON m.id=j.created_by AND m.tenant_id=j.tenant_id JOIN users u ON u.id=m.user_id WHERE j.id=$1 FOR SHARE OF t,m,u`, work.ID).Scan(&active, &authorized, &platformAdmin, &work.Principal.UserID)
	if err == pgx.ErrNoRows || err == nil && (!active || !authorized || work.Source == "platform" && !platformAdmin) {
		return platform.Fail(403, "ai_permission_changed", "租户状态或创建者管理权限已变化，结果未应用")
	}
	if err != nil {
		return err
	}
	work.Principal.TenantID = work.Tenant
	work.Principal.MembershipID = work.Member
	work.Principal.PlatformAdmin = platformAdmin
	if err := s.checkFeature(ctx, tx, work.Tenant, "ai_design"); err != nil {
		return err
	}
	return s.checkFeature(ctx, tx, work.Tenant, "advanced_decoration")
}

// assistantFail 保存安全失败原因及供应商用量，同时保留所有已完成阶段和原模板。
func (s *Service) assistantFail(ctx context.Context, tx pgx.Tx, id string, cause error, usage json.RawMessage, billing string) error {
	code, message := "assistant_failed", "本次创作未完成，已完成阶段和原模板已保留"
	var api *platform.APIError
	if errors.As(cause, &api) {
		code, message = api.Code, api.Message
	}
	if len(usage) == 0 {
		usage = json.RawMessage(`{}`)
	}
	if billing == "" {
		billing = "not_called"
	}
	if _, err := tx.Exec(ctx, "UPDATE assistant_jobs SET status=CASE WHEN status IN('queued','running','validating') THEN 'failed' ELSE status END,error_code=CASE WHEN status IN('queued','running','validating') THEN $2 ELSE error_code END,error_message=CASE WHEN status IN('queued','running','validating') THEN $3 ELSE error_message END,usage=usage||$4::jsonb,billing_status=CASE WHEN $5='not_called' THEN billing_status ELSE $5 END,completed_at=coalesce(completed_at,now()),lease_until=NULL WHERE id=$1", id, code, message, usage, billing); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM assistant_queue WHERE job_id=$1", id)
	return err
}

// processAssistantJob 固定配置及约束后依次运行文本、图片和静态包阶段。
func (s *Service) processAssistantJob(ctx context.Context, tenant, id string) error {
	work := assistantWork{ID: id, Tenant: tenant}
	run := false
	err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		var status string
		var expired bool
		var output json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT thread_id::text,created_by::text,source,phase,config_version,contract_version,coalesce(base_template_id::text,''),base_version,base_design,source_result,status,coalesce(lease_until<now(),false) FROM assistant_jobs WHERE id=$1 FOR UPDATE`, id).Scan(&work.Thread, &work.Member, &work.Source, &work.Phase, &work.ConfigVersion, &work.ContractVersion, &work.BaseTemplate, &work.BaseVersion, &work.BaseDesign, &output, &status, &expired); err != nil {
			return err
		}
		if status == "running" || status == "validating" {
			if expired {
				if _, err := tx.Exec(ctx, "UPDATE assistant_image_tasks SET status='failed',billing_status='uncertain',error_message='图片调用中断，用量待核对' WHERE job_id=$1 AND status='running'", id); err != nil {
					return err
				}
				return s.assistantFail(ctx, tx, id, platform.Fail(502, "assistant_interrupted", "任务调用中断，用量待核对；不会自动重复调用，请查看已完成阶段后决定是否重试"), nil, "uncertain")
			}
			return nil
		}
		if status != "queued" {
			_, err := tx.Exec(ctx, "DELETE FROM assistant_queue WHERE job_id=$1", id)
			return err
		}
		if err := s.assistantPermission(ctx, tx, &work); err != nil {
			return s.assistantFail(ctx, tx, id, err, nil, "not_called")
		}
		var err error
		work.Configuration, err = s.loadAssistantConfiguration(ctx, tx, work.Source, tenant, true)
		if err != nil {
			return s.assistantFail(ctx, tx, id, err, nil, "not_called")
		}
		if !work.Configuration.Enabled || !work.Configuration.Configured || work.Configuration.Version != work.ConfigVersion {
			return s.assistantFail(ctx, tx, id, platform.Fail(409, "assistant_configuration_changed", "AI配置已变化或停用，请检查配置后明确重试"), nil, "not_called")
		}
		if work.ContractVersion == 0 {
			if _, err := s.currentAssistantFoundation(ctx, tx); err != nil {
				return err
			}
			var scripts []byte
			if err := tx.QueryRow(ctx, "SELECT version,prompt,scripts FROM assistant_contract_versions ORDER BY version LIMIT 1").Scan(&work.Foundation.Version, &work.Foundation.Prompt, &scripts); err != nil {
				return err
			}
			if err := json.Unmarshal(scripts, &work.Foundation.Scripts); err != nil {
				return err
			}
		} else {
			var scripts []byte
			if err := tx.QueryRow(ctx, "SELECT version,prompt,scripts FROM assistant_contract_versions WHERE version=$1", work.ContractVersion).Scan(&work.Foundation.Version, &work.Foundation.Prompt, &scripts); err != nil {
				return err
			}
			if err := json.Unmarshal(scripts, &work.Foundation.Scripts); err != nil {
				return err
			}
		}
		if len(output) > 0 {
			if err := json.Unmarshal(output, &work.Output); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE assistant_jobs SET status='running',started_at=coalesce(started_at,now()),lease_until=now()+interval '140 seconds' WHERE id=$1", id); err != nil {
			return err
		}
		run = true
		return nil
	})
	if err != nil || !run {
		return err
	}
	callCtx, cancel := context.WithCancel(ctx)
	s.assistantCancels.Store(id, context.CancelFunc(cancel))
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		s.assistantHeartbeat(callCtx, tenant, id, 30*time.Second, 140*time.Second, cancel)
	}()
	defer func() { cancel(); <-heartbeatDone; s.assistantCancels.Delete(id) }()
	if work.Phase == "source" {
		if err := s.assistantRunSource(callCtx, &work); err != nil {
			return err
		}
		if work.Output.Type != "files" {
			return nil
		}
	}
	if err := s.assistantRunImages(callCtx, &work); err != nil {
		return err
	}
	return s.assistantFinalize(ctx, &work)
}

// assistantRunSource 对文本候选进行有界预检修复后持久保存，后续图片失败不会再次生成文本。
func (s *Service) assistantRunSource(ctx context.Context, work *assistantWork) error {
	var messages []map[string]any
	err := platform.TenantTx(ctx, s.pool, work.Tenant, func(tx pgx.Tx) error { var err error; messages, err = s.assistantMessages(ctx, tx, work); return err })
	if err != nil {
		return s.assistantSaveFailure(context.WithoutCancel(ctx), work.ID, work.Tenant, err, nil, "not_called")
	}
	output, usage, billing, callErr := s.assistantGenerateCheckedSource(ctx, work, messages)

	return platform.TenantTx(context.WithoutCancel(ctx), s.pool, work.Tenant, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(context.WithoutCancel(ctx), "SELECT status FROM assistant_jobs WHERE id=$1 FOR UPDATE", work.ID).Scan(&status); err != nil {
			return err
		}
		if status != "running" {
			_, err := tx.Exec(context.WithoutCancel(ctx), "UPDATE assistant_jobs SET usage=usage||$2::jsonb,billing_status=$3 WHERE id=$1", work.ID, assistantUsage(usage), billing)
			return err
		}
		if callErr != nil {
			if output.Type == "files" {
				candidate, _ := json.Marshal(output)
				if _, err := tx.Exec(context.WithoutCancel(ctx), "UPDATE assistant_jobs SET source_result=$2 WHERE id=$1", work.ID, candidate); err != nil {
					return err
				}
			}
			return s.assistantFail(context.WithoutCancel(ctx), tx, work.ID, callErr, usage, billing)
		}

		work.Output = output
		raw, _ := json.Marshal(output)
		if output.Type == "text" {
			if _, err := tx.Exec(ctx, "INSERT INTO assistant_messages(tenant_id,thread_id,role,content,job_id) VALUES($1,$2,'assistant',$3,$4)", work.Tenant, work.Thread, output.Message, work.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE assistant_jobs SET status='succeeded',source_result=$2,usage=$3,billing_status=$4,completed_at=now(),lease_until=NULL WHERE id=$1", work.ID, raw, assistantUsage(usage), billing); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "DELETE FROM assistant_queue WHERE job_id=$1", work.ID)
			return err
		}
		for _, task := range output.ImageTasks {
			if _, err := tx.Exec(ctx, "INSERT INTO assistant_image_tasks(tenant_id,job_id,task_key,path,prompt,width,height) VALUES($1,$2,$3,$4,$5,$6,$7)", work.Tenant, work.ID, task.ID, task.Path, task.Prompt, task.Width, task.Height); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE assistant_jobs SET source_result=$2,phase='images',usage=$3,billing_status=$4,lease_until=now()+interval '140 seconds' WHERE id=$1", work.ID, raw, assistantUsage(usage), billing); err != nil {
			return err
		}
		work.Phase = "images"
		return nil
	})
}

// assistantUsage 为空用量提供有效JSON，供应商扩展字段已在读取时过滤。
func assistantUsage(usage json.RawMessage) json.RawMessage {
	if len(usage) == 0 {
		return json.RawMessage(`{}`)
	}
	return usage
}

// assistantSaveFailure 在调用上下文取消后仍用独立短事务保存失败结果。
func (s *Service) assistantSaveFailure(ctx context.Context, id, tenant string, cause error, usage json.RawMessage, billing string) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return platform.TenantTx(saveCtx, s.pool, tenant, func(tx pgx.Tx) error { return s.assistantFail(saveCtx, tx, id, cause, usage, billing) })
}

// assistantRunImages 逐张读取任务并复用成功项，每次真实调用前持久标记费用待核对。
func (s *Service) assistantRunImages(ctx context.Context, work *assistantWork) error {
	for _, task := range work.Output.ImageTasks {
		run := false
		err := platform.TenantTx(ctx, s.pool, work.Tenant, func(tx pgx.Tx) error {
			var status, parent string
			if err := tx.QueryRow(ctx, "SELECT status FROM assistant_jobs WHERE id=$1 FOR UPDATE", work.ID).Scan(&parent); err != nil {
				return err
			}
			if parent != "running" {
				return platform.Fail(409, "assistant_not_running", "任务已结束，不再继续调用图片模型")
			}
			if err := s.assistantPermission(ctx, tx, work); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, "SELECT status FROM assistant_image_tasks WHERE job_id=$1 AND task_key=$2 FOR UPDATE", work.ID, task.ID).Scan(&status); err != nil {
				return err
			}
			if status == "succeeded" {
				return nil
			}
			if status == "running" {
				return platform.Fail(409, "assistant_image_uncertain", "图片已有未完成调用，需要明确重试")
			}
			if _, err := tx.Exec(ctx, "UPDATE assistant_image_tasks SET status='running',billing_status='uncertain' WHERE job_id=$1 AND task_key=$2", work.ID, task.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE assistant_jobs SET phase='images',lease_until=now()+interval '140 seconds' WHERE id=$1", work.ID); err != nil {
				return err
			}
			run = true
			return nil
		})
		if err != nil {
			return s.assistantSaveFailure(ctx, work.ID, work.Tenant, err, nil, "not_called")
		}
		if !run {
			continue
		}
		content, mime, usage, billing, callErr := s.assistantImageCall(ctx, work.Configuration, task)
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		err = platform.TenantTx(saveCtx, s.pool, work.Tenant, func(tx pgx.Tx) error {
			var status string
			if err := tx.QueryRow(saveCtx, "SELECT status FROM assistant_jobs WHERE id=$1 FOR UPDATE", work.ID).Scan(&status); err != nil {
				return err
			}
			state, message := "succeeded", ""
			if callErr != nil {
				state = "failed"
				message = "图片模型调用未完成，请检查配置后重试"
				var api *platform.APIError
				if errors.As(callErr, &api) {
					message = api.Message
				}
			}
			if _, err := tx.Exec(saveCtx, "UPDATE assistant_image_tasks SET status=$3,content=$4,content_type=$5,usage=$6,billing_status=$7,error_message=$8 WHERE job_id=$1 AND task_key=$2", work.ID, task.ID, state, content, mime, assistantUsage(usage), billing, message); err != nil {
				return err
			}
			if status != "running" {
				return nil
			}
			if callErr != nil {
				return s.assistantFail(saveCtx, tx, work.ID, callErr, nil, billing)
			}
			return nil
		})
		cancel()
		if err != nil {
			return err
		}
		if callErr != nil {
			return nil
		}
	}
	return nil
}
