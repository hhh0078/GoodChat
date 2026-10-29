// 文件作用说明：注册管理员模板创作助手，持久保存私有对话、图片引用、幂等消息与分阶段任务，不开放任意业务工具。
package decoration

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// registerAssistant 注册租户管理员与平台管理员各自的配置和创作接口。
func (s *Service) registerAssistant(mux *http.ServeMux) {
	for _, method := range []string{"GET", "PUT"} {
		mux.HandleFunc(method+" /api/v1/tenant/assistant/configuration", s.auth.RequireAdmin(s.assistantSettings))
		mux.HandleFunc(method+" /api/v1/platform/assistant/configuration", s.auth.RequirePlatform(s.assistantSettings))
		mux.HandleFunc(method+" /api/v1/platform/assistant/foundation", s.auth.RequirePlatform(s.assistantFoundationSettings))
	}
	mux.HandleFunc("POST /api/v1/tenant/assistant/configuration/test", s.auth.RequireAdmin(s.assistantTestConfiguration))
	mux.HandleFunc("POST /api/v1/platform/assistant/configuration/test", s.auth.RequirePlatform(s.assistantTestConfiguration))
	mux.HandleFunc("GET /api/v1/tenant/assistant/threads", s.auth.RequireAdmin(s.assistantThreads))
	mux.HandleFunc("POST /api/v1/tenant/assistant/threads", s.auth.RequireAdmin(s.assistantThreads))
	mux.HandleFunc("GET /api/v1/tenant/assistant/threads/{id}", s.auth.RequireAdmin(s.assistantGetThread))
	mux.HandleFunc("POST /api/v1/tenant/assistant/threads/{id}/messages", s.auth.RequireAdmin(s.assistantCreateMessage))
	mux.HandleFunc("POST /api/v1/tenant/assistant/threads/{id}/cancel", s.auth.RequireAdmin(s.assistantCancel))
	mux.HandleFunc("GET /api/v1/tenant/assistant/jobs/{id}", s.auth.RequireAdmin(s.assistantGetJob))
	mux.HandleFunc("POST /api/v1/tenant/assistant/jobs/{id}/retry", s.auth.RequireAdmin(s.assistantRetry))
	mux.HandleFunc("POST /api/v1/tenant/assistant/assets", s.auth.RequireAdmin(s.assistantUploadAsset))
	mux.HandleFunc("GET /api/v1/tenant/assistant/assets/{id}", s.auth.RequireAdmin(s.assistantGetAsset))
	mux.HandleFunc("GET /api/v1/platform/assistant/foundation/versions", s.auth.RequirePlatform(s.assistantFoundationVersions))
}

// assistantThreadInput 指定创作目标，不接受租户或任意模型凭证。
type assistantThreadInput struct {
	Title      string `json:"title"`
	TemplateID string `json:"template_id"`
	Source     string `json:"source"`
}

// assistantThreads 列出或创建本租户持久对话，平台配置源要求实时平台管理员身份。
func (s *Service) assistantThreads(w http.ResponseWriter, r *http.Request) {
	var input assistantThreadInput
	if r.Method == http.MethodPost {
		if err := platform.Decode(w, r, &input); err != nil {
			respond(w, r, nil, err)
			return
		}
		if err := validName(input.Title); err != nil {
			respond(w, r, nil, err)
			return
		}
		if input.Source == "" {
			input.Source = "tenant"
			if platform.Current(r).PlatformAdmin {
				input.Source = "platform"
			}
		}
		if input.Source != "tenant" && input.Source != "platform" {
			respond(w, r, nil, invalidDesign("请选择租户或平台自己的AI配置"))
			return
		}
		if input.TemplateID != "" {
			if err := idValue(input.TemplateID); err != nil {
				respond(w, r, nil, err)
				return
			}
		}
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		p := platform.Current(r)
		if r.Method == http.MethodGet {
			rows, err := tx.Query(r.Context(), "SELECT to_jsonb(t) FROM assistant_threads t WHERE source='tenant' OR $1 ORDER BY updated_at DESC,id LIMIT 100", p.PlatformAdmin)
			if err != nil {
				return nil, err
			}
			items, err := jsonRows(rows)
			return map[string]any{"items": items}, err
		}
		if input.Source == "platform" && !p.PlatformAdmin {
			return nil, platform.Fail(403, "platform_required", "平台AI配置仅供平台管理员使用")
		}
		if input.TemplateID != "" {
			copied, err := s.assistantRemixTemplate(r.Context(), tx, p, input.TemplateID, input.Title)
			if err != nil {
				return nil, err
			}
			input.TemplateID = copied
		}

		var threadID string
		if err := tx.QueryRow(r.Context(), "INSERT INTO assistant_threads(tenant_id,created_by,title,source,template_id) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid) RETURNING id::text", p.TenantID, p.MembershipID, input.Title, input.Source, input.TemplateID).Scan(&threadID); err != nil {
			return nil, err
		}
		if input.TemplateID != "" {
			var pageID string
			if err := tx.QueryRow(r.Context(), "INSERT INTO decoration_pages(tenant_id,name,template_id,render_mode,draft) SELECT tenant_id,$2,id,render_mode,design FROM tenant_templates WHERE id=$1 RETURNING id::text", input.TemplateID, input.Title).Scan(&pageID); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(r.Context(), "UPDATE assistant_threads SET page_id=$2 WHERE id=$1", threadID, pageID); err != nil {
				return nil, err
			}
		}
		return record(r.Context(), tx, "SELECT to_jsonb(t) FROM assistant_threads t WHERE id=$1", threadID)
	})
}

// assistantGetThread 返回对话、当前任务及真实静态预览地址，不在后台执行模板脚本。
func (s *Service) assistantGetThread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		var raw json.RawMessage
		if err := tx.QueryRow(r.Context(), "SELECT to_jsonb(t) FROM assistant_threads t WHERE id=$1", id).Scan(&raw); err != nil {
			return nil, err
		}
		result := map[string]any{}
		_ = json.Unmarshal(raw, &result)
		if result["source"] == "platform" && !platform.Current(r).PlatformAdmin {
			return nil, platform.Fail(403, "platform_required", "此对话使用平台AI配置，需要平台管理员权限")
		}
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(m) FROM (SELECT * FROM assistant_messages WHERE thread_id=$1 ORDER BY created_at DESC,id DESC LIMIT 60) m ORDER BY created_at,id", id)
		if err != nil {
			return nil, err
		}
		messages, err := jsonRows(rows)
		if err != nil {
			return nil, err
		}
		result["messages"] = messages
		var jobID string
		err = tx.QueryRow(r.Context(), "SELECT id::text FROM assistant_jobs WHERE thread_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1", id).Scan(&jobID)
		result["active_job"] = nil
		result["latest_job"] = nil
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		if jobID != "" {
			job, e := s.assistantJobData(r, tx, jobID)
			if e != nil {
				return nil, e
			}
			result["latest_job"] = job
			if status, _ := job["status"].(string); status == "queued" || status == "running" || status == "validating" {
				result["active_job"] = job
			}
			for _, key := range []string{"preview_url", "page_id"} {
				if value, ok := job[key]; ok {
					result[key] = value
				}
			}
		}
		if result["preview_url"] == nil && result["page_id"] != nil {
			var design json.RawMessage
			if err := tx.QueryRow(r.Context(), "SELECT draft FROM decoration_pages WHERE id=$1", result["page_id"]).Scan(&design); err != nil {
				return nil, err
			}
			preview, err := s.assistantPreviewURL(r.Context(), tx, platform.Current(r).TenantID, result["page_id"].(string), design)
			if err != nil {
				return nil, err
			}
			result["preview_url"] = preview
		}
		return result, nil
	})
}

// assistantMessageInput 通过随机客户端编号实现提交幂等，图片只能引用已校验资产。
type assistantMessageInput struct {
	Content         string   `json:"content"`
	AssetIDs        []string `json:"asset_ids"`
	ClientMessageID string   `json:"client_message_id"`
}

// assistantCreateMessage 原子写入消息及排队任务，不会为纯文本答复预先创建空模板。
func (s *Service) assistantCreateMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input assistantMessageInput
	if err := idValue(id); err != nil {
		respond(w, r, nil, err)
		return
	}
	if err := platform.Decode(w, r, &input); err != nil {
		respond(w, r, nil, err)
		return
	}
	if len(strings.TrimSpace(input.Content)) < 1 || len(input.Content) > 16000 || len(input.AssetIDs) > 4 {
		respond(w, r, nil, invalidDesign("请填写1至16000字节的需求，每条消息最多4张图片"))
		return
	}
	if input.AssetIDs == nil {
		input.AssetIDs = []string{}
	}
	if err := idValue(input.ClientMessageID); err != nil {
		respond(w, r, nil, err)
		return
	}
	seen := map[string]bool{}
	for _, asset := range input.AssetIDs {
		if err := idValue(asset); err != nil {
			respond(w, r, nil, err)
			return
		}
		if seen[asset] {
			respond(w, r, nil, invalidDesign("图片引用不能重复"))
			return
		}
		seen[asset] = true
	}
	s.tenant(w, r, func(tx pgx.Tx) (any, error) {
		p := platform.Current(r)
		var source, templateID string
		var version int64
		var baseDesign json.RawMessage
		if err := tx.QueryRow(r.Context(), "SELECT source,coalesce(template_id::text,'') FROM assistant_threads WHERE id=$1 FOR UPDATE", id).Scan(&source, &templateID); err != nil {
			return nil, err
		}
		if source == "platform" && !p.PlatformAdmin {
			return nil, platform.Fail(403, "platform_required", "此对话需要平台管理员权限")
		}
		var existing string
		err := tx.QueryRow(r.Context(), "SELECT coalesce(job_id::text,'') FROM assistant_messages WHERE thread_id=$1 AND client_message_id=$2", id, input.ClientMessageID).Scan(&existing)
		if err == nil && existing != "" {
			job, e := s.assistantJobData(r, tx, existing)
			if e != nil {
				return nil, e
			}
			message, e := record(r.Context(), tx, "SELECT to_jsonb(m) FROM assistant_messages m WHERE thread_id=$1 AND client_message_id=$2", id, input.ClientMessageID)
			return map[string]any{"message": message, "job": job, "thread_id": id}, e
		}
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		cfg, err := s.loadAssistantConfiguration(r.Context(), tx, source, p.TenantID, false)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled || !cfg.Configured {
			return nil, platform.Fail(503, "ai_unavailable", "请先配置并启用当前使用方自己的AI模型")
		}
		if err := s.checkFeature(r.Context(), tx, p.TenantID, "ai_design"); err != nil {
			return nil, err
		}
		if err := s.checkFeature(r.Context(), tx, p.TenantID, "advanced_decoration"); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", p.TenantID); err != nil {
			return nil, err
		}
		var active, used int
		if err := tx.QueryRow(r.Context(), "SELECT count(*) FROM assistant_jobs WHERE status IN('queued','running','validating')").Scan(&active); err != nil {
			return nil, err
		}
		if active >= 2 {
			return nil, platform.Fail(429, "ai_concurrency_limit", "当前租户已有两个创作任务")
		}
		var threadActive bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM assistant_jobs WHERE thread_id=$1 AND status IN('queued','running','validating'))", id).Scan(&threadActive); err != nil {
			return nil, err
		}
		if threadActive {
			return nil, platform.Fail(409, "assistant_busy", "请等待当前对话任务结束或先取消")
		}
		if err := tx.QueryRow(r.Context(), "SELECT count(*) FROM assistant_jobs WHERE created_at>=((now() AT TIME ZONE (SELECT timezone FROM tenants WHERE id=$1))::date::timestamp AT TIME ZONE (SELECT timezone FROM tenants WHERE id=$1))", p.TenantID).Scan(&used); err != nil {
			return nil, err
		}
		if s.cfg.AIDailyLimit <= 0 || used >= s.cfg.AIDailyLimit {
			return nil, platform.Fail(429, "ai_daily_limit", "当前租户今日AI创作次数已用完或已停用")
		}
		for _, asset := range input.AssetIDs {
			var exists bool
			if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM assistant_assets WHERE id=$1)", asset).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, platform.Fail(404, "not_found", "参考图片不存在或不属于当前租户")
			}
		}
		if templateID != "" {
			if err := tx.QueryRow(r.Context(), "SELECT version,design FROM tenant_templates WHERE id=$1 FOR KEY SHARE", templateID).Scan(&version, &baseDesign); err != nil {
				return nil, err
			}
		}
		foundation, err := s.currentAssistantFoundation(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		var messageID, jobID string
		if err := tx.QueryRow(r.Context(), "INSERT INTO assistant_messages(tenant_id,thread_id,role,content,asset_ids,client_message_id) VALUES($1,$2,'user',$3,$4,$5) RETURNING id::text", p.TenantID, id, input.Content, input.AssetIDs, input.ClientMessageID).Scan(&messageID); err != nil {
			return nil, err
		}
		if err := tx.QueryRow(r.Context(), "INSERT INTO assistant_jobs(tenant_id,thread_id,message_id,created_by,source,config_version,contract_version,base_template_id,base_version,base_design) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10) RETURNING id::text", p.TenantID, id, messageID, p.MembershipID, source, cfg.Version, foundation.Version, templateID, version, baseDesign).Scan(&jobID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE assistant_messages SET job_id=$2 WHERE id=$1", messageID, jobID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO assistant_queue(job_id,tenant_id) VALUES($1,$2)", jobID, p.TenantID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE assistant_threads SET updated_at=now() WHERE id=$1", id); err != nil {
			return nil, err
		}
		message, err := record(r.Context(), tx, "SELECT to_jsonb(m) FROM assistant_messages m WHERE id=$1", messageID)
		if err != nil {
			return nil, err
		}
		job, err := s.assistantJobData(r, tx, jobID)
		return map[string]any{"message": message, "job": job, "thread_id": id}, err
	})
}
