// 文件作用说明：提供任务查询、隔离静态预览、取消和明确重试，保留失败阶段与已完成图片，禁止静默重复调用模型。
package decoration

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// assistantJobData 返回任务公开状态和固定候选预览，响应不包含图片字节或模型密钥。
func (s *Service) assistantJobData(r *http.Request, tx pgx.Tx, id string) (map[string]any, error) {
	var raw json.RawMessage
	var source string
	if err := tx.QueryRow(r.Context(), "SELECT to_jsonb(j)-'source_result',source FROM assistant_jobs j WHERE id=$1", id).Scan(&raw, &source); err != nil {
		return nil, err
	}
	if source == "platform" && !platform.Current(r).PlatformAdmin {
		return nil, platform.Fail(403, "platform_required", "此任务使用平台自己的AI配置")
	}
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	var templateID, pageID string
	if err := tx.QueryRow(r.Context(), "SELECT coalesce(template_id::text,''),coalesce(page_id::text,'') FROM assistant_threads WHERE id=$1", result["thread_id"]).Scan(&templateID, &pageID); err != nil {
		return nil, err
	}
	if templateID != "" {
		result["template_id"] = templateID
	}
	if pageID != "" {
		result["page_id"] = pageID
	}
	rows, err := tx.Query(r.Context(), "SELECT jsonb_build_object('id',id,'path',path,'status',status,'billing_status',billing_status,'usage',usage,'error_message',error_message,'width',width,'height',height) FROM assistant_image_tasks WHERE job_id=$1 ORDER BY created_at,id", id)
	if err != nil {
		return nil, err
	}
	images, err := jsonRows(rows)
	if err != nil {
		return nil, err
	}
	result["image_tasks"] = images
	for _, rawImage := range images {
		var imageState struct {
			Billing string `json:"billing_status"`
		}
		_ = json.Unmarshal(rawImage, &imageState)
		if imageState.Billing == "uncertain" {
			result["billing_status"] = "uncertain"
		}
	}
	if result["status"] == "succeeded" && result["design"] != nil && pageID != "" {
		design, _ := json.Marshal(result["design"])
		preview, err := s.assistantPreviewURL(r.Context(), tx, platform.Current(r).TenantID, pageID, design)
		if err != nil {
			return nil, err
		}
		result["preview_url"] = preview
	}
	return result, nil
}

// assistantPreviewURL 创建绑定现有页面的短期静态快照，仅供隔离预览中的模拟SDK使用。
func (s *Service) assistantPreviewURL(ctx context.Context, tx pgx.Tx, tenant, pageID string, design json.RawMessage) (string, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM decoration_previews WHERE expires_at<=now()"); err != nil {
		return "", err
	}
	token, err := platform.Token()
	if err != nil {
		return "", err
	}
	var previewID string
	if err := tx.QueryRow(ctx, "INSERT INTO decoration_previews(tenant_id,page_id,render_mode,design,token_hash) VALUES($1,$2,'code',$3,$4) RETURNING id::text", tenant, pageID, design, digest([]byte(token))).Scan(&previewID); err != nil {
		return "", err
	}
	return s.renderURL(previewID, token), nil
}

// assistantGetJob 读取当前租户任务，其他租户编号返回不存在。
func (s *Service) assistantGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) { return s.assistantJobData(r, tx, id) })
}

// assistantCancel 将活动任务置为取消，尽力中止当前HTTP调用，迟到结果不会应用模板。
func (s *Service) assistantCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	var cancelledID string
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var source string
		if err := tx.QueryRow(r.Context(), "SELECT source FROM assistant_threads WHERE id=$1 FOR UPDATE", id).Scan(&source); err != nil {
			return nil, err
		}
		if source == "platform" && !platform.Current(r).PlatformAdmin {
			return nil, platform.Fail(403, "platform_required", "此对话需要平台管理员权限")
		}
		err := tx.QueryRow(r.Context(), "UPDATE assistant_jobs SET status='cancelled',completed_at=now(),error_code='cancelled',error_message='已取消；已经发送到供应商的调用仍可能产生用量' WHERE thread_id=$1 AND status IN('queued','running','validating') RETURNING id::text", id).Scan(&cancelledID)
		if err == pgx.ErrNoRows {
			return map[string]any{"cancelled": false}, nil
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM assistant_queue WHERE job_id=$1", cancelledID); err != nil {
			return nil, err
		}
		if cancel, ok := s.assistantCancels.Load(cancelledID); ok {
			cancel.(context.CancelFunc)()
		}
		return s.assistantJobData(r, tx, cancelledID)
	})
}

// assistantRetry 为明确重试创建全新任务，复制成功阶段，旧调用只能回写旧编号，不能污染新一轮或移除新取消句柄。
func (s *Service) assistantRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		p := platform.Current(r)
		var source, status, thread string
		if err := tx.QueryRow(r.Context(), "SELECT source,status,thread_id::text FROM assistant_jobs WHERE id=$1", id).Scan(&source, &status, &thread); err != nil {
			return nil, err
		}
		if source == "platform" && !p.PlatformAdmin {
			return nil, platform.Fail(403, "platform_required", "此任务需要平台管理员权限")
		}
		if status != "failed" && status != "cancelled" {
			return nil, platform.Fail(409, "assistant_retry_invalid", "只能重试已失败或已取消任务")
		}
		if _, err := tx.Exec(r.Context(), "SELECT id FROM assistant_threads WHERE id=$1 FOR UPDATE", thread); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", p.TenantID); err != nil {
			return nil, err
		}
		var threadActive bool
		var active, used int
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM assistant_jobs WHERE thread_id=$1 AND status IN('queued','running','validating'))", thread).Scan(&threadActive); err != nil {
			return nil, err
		}
		if threadActive {
			return nil, platform.Fail(409, "assistant_busy", "此对话已有活动任务，请等待或取消")
		}
		if err := tx.QueryRow(r.Context(), "SELECT count(*) FROM assistant_jobs WHERE status IN('queued','running','validating')").Scan(&active); err != nil {
			return nil, err
		}
		if active >= 2 {
			return nil, platform.Fail(429, "ai_concurrency_limit", "请先等待当前任务结束")
		}
		if err := tx.QueryRow(r.Context(), "SELECT count(*) FROM assistant_jobs WHERE created_at>=((now() AT TIME ZONE (SELECT timezone FROM tenants WHERE id=$1))::date::timestamp AT TIME ZONE (SELECT timezone FROM tenants WHERE id=$1))", p.TenantID).Scan(&used); err != nil {
			return nil, err
		}
		if s.cfg.AIDailyLimit <= 0 || used >= s.cfg.AIDailyLimit {
			return nil, platform.Fail(429, "ai_daily_limit", "当前租户今日AI创作次数已用完或已停用")
		}
		cfg, err := s.loadAssistantConfiguration(r.Context(), tx, source, p.TenantID, false)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled || !cfg.Configured {
			return nil, platform.Fail(503, "ai_unavailable", "请先配置并启用AI")
		}
		var nextID string
		if err := tx.QueryRow(r.Context(), `INSERT INTO assistant_jobs(tenant_id,thread_id,message_id,created_by,source,config_version,contract_version,base_template_id,base_version,base_design,phase,source_result)
    SELECT tenant_id,thread_id,message_id,$2,source,$3,contract_version,base_template_id,base_version,base_design,phase,source_result FROM assistant_jobs WHERE id=$1 RETURNING id::text`, id, p.MembershipID, cfg.Version).Scan(&nextID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO assistant_image_tasks(tenant_id,job_id,task_key,path,prompt,width,height,status,content,content_type,usage,billing_status)
    SELECT tenant_id,$2,task_key,path,prompt,width,height,CASE WHEN status='succeeded' THEN 'succeeded' ELSE 'queued' END,CASE WHEN status='succeeded' THEN content ELSE NULL END,CASE WHEN status='succeeded' THEN content_type ELSE '' END,'{}'::jsonb,'not_called' FROM assistant_image_tasks WHERE job_id=$1`, id, nextID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO assistant_queue(job_id,tenant_id) VALUES($1,$2)", nextID, p.TenantID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE assistant_threads SET updated_at=now() WHERE id=$1", thread); err != nil {
			return nil, err
		}
		return s.assistantJobData(r, tx, nextID)
	})
}

// assistantTestConfiguration 真实测试已保存的当前方配置，只返回连接结果和安全用量。
func (s *Service) assistantTestConfiguration(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Kind string `json:"kind"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	source := "tenant"
	if r.URL.Path == "/api/v1/platform/assistant/configuration/test" {
		source = "platform"
	}
	var cfg assistantConfiguration
	var loadErr error
	s.assistantScope(w, r, source, func(tx pgx.Tx) (any, error) {
		cfg, loadErr = s.loadAssistantConfiguration(r.Context(), tx, source, platform.Current(r).TenantID, true)
		if loadErr != nil {
			return nil, loadErr
		}
		if !cfg.Configured {
			return nil, platform.Fail(503, "ai_unavailable", "请先保存模型地址、模型及密钥")
		}
		var usage json.RawMessage
		var billing string
		var err error
		if input.Kind == "image" {
			_, _, usage, billing, err = s.assistantImageCall(r.Context(), cfg, assistantImageTask{Prompt: "简单的白色背景上的绿色圆形图标，不含文字", Width: 512, Height: 512})
		} else if input.Kind == "text" || input.Kind == "" {
			_, usage, billing, err = s.assistantTextCall(r.Context(), cfg, []map[string]any{{"role": "system", "content": "只返回JSON对象{\"schema_version\":1,\"type\":\"text\",\"message\":\"连接正常\"}"}, {"role": "user", "content": "测试连接"}})
		} else {
			return nil, invalidDesign("请选择text或image连接测试")
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "message": "模型连接成功", "model": cfg.Model, "usage": usage, "billing_status": billing}, nil
	})
}
