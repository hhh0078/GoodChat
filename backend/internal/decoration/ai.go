// 文件作用说明：创建归属租户的真实AI设计任务，并提供持久进度、候选结果与取消接口。
package decoration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiInput 定义租户主动提交的设计需求，不接受模型密钥或客户聊天数据。
type aiInput struct {
	Name        string `json:"name"`
	Prompt      string `json:"prompt"`
	RenderMode  string `json:"render_mode"`
	TemplateID  string `json:"template_id,omitempty"`
	BaseVersion int64  `json:"base_version,omitempty"`
}

// aiReady 检查服务端供应商配置，缺少配置不会创建假任务。
func (s *Service) aiReady() bool {
	return strings.TrimSpace(s.cfg.AIEndpoint) != "" && strings.TrimSpace(s.cfg.AIKey) != "" && strings.TrimSpace(s.cfg.AIModel) != ""
}

// aiConfiguration 返回模型配置与租户本地自然日提交额度，token用量不视为费用。
func (s *Service) aiConfiguration(w http.ResponseWriter, r *http.Request) {
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		used, timezone, err := dailyAIUsage(r.Context(), tx, platform.Current(r).TenantID)
		if err != nil {
			return nil, err
		}
		remaining := max(0, s.cfg.AIDailyLimit-used)
		return map[string]any{"configured": s.aiReady(), "enabled": s.aiReady() && s.cfg.AIDailyLimit > 0, "model": s.cfg.AIModel, "daily_limit": s.cfg.AIDailyLimit, "used_today": used, "remaining_today": remaining, "timezone": timezone}, nil
	})
}

// dailyAIUsage 按租户IANA时区计算今天全部已提交任务，失败和取消也不返还提交次数。
func dailyAIUsage(ctx context.Context, tx pgx.Tx, tenant string) (int, string, error) {
	var used int
	var timezone string
	err := tx.QueryRow(ctx, `SELECT t.timezone,(SELECT count(*) FROM ai_design_jobs j WHERE j.tenant_id=t.id AND j.created_at>=((now() AT TIME ZONE t.timezone)::date::timestamp AT TIME ZONE t.timezone) AND j.created_at<(((now() AT TIME ZONE t.timezone)::date+1)::timestamp AT TIME ZONE t.timezone)) FROM tenants t WHERE t.id=$1`, tenant).Scan(&timezone, &used)
	return used, timezone, err
}

// createAIJob 在同一租户事务内创建私有草稿、持久任务和无载荷调度目录。
func (s *Service) createAIJob(w http.ResponseWriter, r *http.Request) {
	if !s.aiReady() {
		respond(w, r, nil, platform.Fail(503, "ai_unavailable", "平台尚未配置可用的AI模型，请联系平台管理员"))
		return
	}
	if s.cfg.AIDailyLimit <= 0 {
		respond(w, r, nil, platform.Fail(503, "ai_daily_disabled", "平台当前已禁用AI设计提交，请联系平台管理员"))
		return
	}
	var input aiInput
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := validName(input.Name); err != nil {
		respond(w, r, nil, err)
		return
	}
	if len(strings.TrimSpace(input.Prompt)) < 5 || len(input.Prompt) > 16000 {
		respond(w, r, nil, platform.Fail(400, "invalid_prompt", "请填写5至16000字节的设计需求"))
		return
	}
	if input.RenderMode != "schema" && input.RenderMode != "code" {
		respond(w, r, nil, invalidDesign("请选择schema或code装修模式"))
		return
	}
	if input.TemplateID != "" {
		if err := idValue(input.TemplateID); err != nil {
			respond(w, r, nil, err)
			return
		}
		if input.BaseVersion < 1 {
			respond(w, r, nil, conflict())
			return
		}
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		ctx, p := r.Context(), platform.Current(r)
		if err := s.checkFeature(ctx, tx, p.TenantID, "ai_design"); err != nil {
			return nil, err
		}
		if err := s.checkFeature(ctx, tx, p.TenantID, decorationFeature(input.RenderMode)); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", p.TenantID); err != nil {
			return nil, err
		}
		used, _, err := dailyAIUsage(ctx, tx, p.TenantID)
		if err != nil {
			return nil, err
		}
		if used >= s.cfg.AIDailyLimit {
			return nil, platform.Fail(429, "ai_daily_limit", "当前租户今日AI设计提交次数已用完，请在租户本地日期更新后重试")
		}
		var active int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM ai_design_jobs WHERE status IN('queued','running','validating')").Scan(&active); err != nil {
			return nil, err
		}
		if active >= 2 {
			return nil, platform.Fail(429, "ai_concurrency_limit", "当前租户已有两个生成任务，请等待或取消后再试")
		}
		templateID, base := input.TemplateID, input.BaseVersion
		if templateID == "" {
			if err := tx.QueryRow(ctx, `INSERT INTO tenant_templates(tenant_id,owner_membership_id,name,render_mode,design,status) VALUES($1,$2,$3,$4,$5,'generating') RETURNING id::text,version`, p.TenantID, p.MembershipID, input.Name, input.RenderMode, defaultDesign(input.RenderMode, input.Name)).Scan(&templateID, &base); err != nil {
				return nil, err
			}
		} else {
			var version int64
			var mode string
			var packageID string
			if err := tx.QueryRow(ctx, "SELECT version,render_mode,coalesce(package_id::text,'') FROM tenant_templates WHERE id=$1 FOR UPDATE", templateID).Scan(&version, &mode, &packageID); err != nil {
				return nil, err
			}
			if version != base {
				return nil, conflict()
			}
			if mode != input.RenderMode {
				return nil, invalidDesign("修改模式请先新建私有模板，以保留原设计")
			}
			if packageID != "" {
				return nil, platform.Fail(409, "ai_package_edit_requires_source", "资源包请在源码编辑器另存新包；AI可另建内联代码草稿，不会覆盖或丢弃当前包的依赖")
			}
		}
		var jobID string
		if err := tx.QueryRow(ctx, `INSERT INTO ai_design_jobs(tenant_id,template_id,created_by,name,prompt,render_mode,model,base_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`, p.TenantID, templateID, p.MembershipID, input.Name, input.Prompt, input.RenderMode, s.cfg.AIModel, base).Scan(&jobID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO ai_design_queue(job_id,tenant_id) VALUES($1,$2)", jobID, p.TenantID); err != nil {
			return nil, err
		}
		return record(ctx, tx, "SELECT to_jsonb(j) FROM ai_design_jobs j WHERE id=$1", jobID)
	})
}

// listAIJobs 列出当前租户任务，失败和未应用候选结果可离开页面后继续查看。
func (s *Service) listAIJobs(w http.ResponseWriter, r *http.Request) {
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(j) FROM ai_design_jobs j ORDER BY created_at DESC,id LIMIT 100")
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(rows)
		return map[string]any{"items": items}, err
	})
}

// getAIJob 按认证租户读取任务详情，其他租户任务不可枚举。
func (s *Service) getAIJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		return record(r.Context(), tx, "SELECT to_jsonb(j) FROM ai_design_jobs j WHERE id=$1", id)
	})
}

// cancelAIJob 将任务置为终态，迟到结果在锁内复查状态后不得写入草稿。
func (s *Service) cancelAIJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		ctx := r.Context()
		var status, templateID string
		var base int64
		if err := tx.QueryRow(ctx, "SELECT status,template_id::text,base_version FROM ai_design_jobs WHERE id=$1 FOR UPDATE", id).Scan(&status, &templateID, &base); err != nil {
			return nil, err
		}
		if status == "queued" || status == "running" || status == "validating" {
			if _, err := tx.Exec(ctx, "UPDATE ai_design_jobs SET status='cancelled',completed_at=now(),billing_status=CASE WHEN status='queued' THEN billing_status ELSE 'uncertain' END WHERE id=$1", id); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, "UPDATE tenant_templates SET status='draft' WHERE id=$1 AND version=$2 AND status='generating'", templateID, base); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, "DELETE FROM ai_design_queue WHERE job_id=$1", id); err != nil {
			return nil, err
		}
		return record(ctx, tx, "SELECT to_jsonb(j) FROM ai_design_jobs j WHERE id=$1", id)
	})
}

// providerResult 保存通过安全校验的设计及模型实际报告的用量。
type providerResult struct {
	Design  json.RawMessage
	Usage   json.RawMessage
	Billing string
}
