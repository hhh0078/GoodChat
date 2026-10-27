// 文件作用说明：管理分层欢迎配置、真实图片和可点击选项，并将内容冻结到持久延迟任务。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// welcomeOption 分开保存显示文案、访客实际发送内容和一次消息的稳定去重编号。
type welcomeOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Value    string `json:"value"`
	ClientID string `json:"client_message_id,omitempty"`
}

// welcomeContent 保存从会话创建时刻计算的绝对延迟，不累计上一条延迟。
type welcomeContent struct {
	Type         string          `json:"type"`
	Content      string          `json:"content"`
	AttachmentID string          `json:"attachment_id,omitempty"`
	Options      []welcomeOption `json:"options,omitempty"`
	DelaySeconds int             `json:"delay_seconds"`
}

// welcomeRule 表示某个层级和营业场景的一组完整覆盖内容。
type welcomeRule struct {
	ID          string           `json:"id,omitempty"`
	Name        string           `json:"name"`
	ScopeType   string           `json:"scope_type"`
	ScopeID     string           `json:"scope_id,omitempty"`
	Scenario    string           `json:"scenario"`
	Enabled     bool             `json:"enabled"`
	StopOnAgent bool             `json:"stop_on_agent"`
	Contents    []welcomeContent `json:"contents"`
	Version     int              `json:"version"`
}

// validateWelcomeRule 校验纯文本内容和本租户内部图片，禁止外链、视频及跨租户素材。
func validateWelcomeRule(ctx context.Context, tx pgx.Tx, item welcomeRule) error {
	if strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 100 || !inStrings(item.ScopeType, "tenant", "space", "link") || !inStrings(item.Scenario, "all", "business", "outside") || len(item.Contents) < 1 || len(item.Contents) > 12 {
		return platform.Fail(400, "invalid_welcome_rule", "请检查规则名称、层级、场景和1至12条欢迎内容")
	}
	if item.ScopeType == "tenant" && item.ScopeID != "" || item.ScopeType != "tenant" && !uuidPattern.MatchString(item.ScopeID) {
		return platform.Fail(400, "invalid_welcome_scope", "欢迎规则层级和关联编号不匹配")
	}
	if item.ScopeType != "tenant" {
		table := "service_spaces"
		if item.ScopeType == "link" {
			table = "access_links"
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE id=$1)", item.ScopeID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return platform.Fail(404, "welcome_scope_not_found", "规则关联范围不存在或不属于本租户")
		}
	}
	for _, content := range item.Contents {
		if !inStrings(content.Type, "text", "image", "options") || len([]rune(content.Content)) > 2000 || content.DelaySeconds < 0 || content.DelaySeconds > 3600 {
			return platform.Fail(400, "invalid_welcome_content", "欢迎内容仅支持文字、图片或选项，延迟为0至3600秒")
		}
		if content.Type == "text" && strings.TrimSpace(content.Content) == "" {
			return platform.Fail(400, "empty_welcome_content", "文字欢迎内容不能为空")
		}
		if content.Type == "image" {
			if !uuidPattern.MatchString(content.AttachmentID) {
				return platform.Fail(400, "invalid_welcome_image", "请选择已上传的内部图片附件")
			}
			var valid bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM attachments WHERE id=$1 AND uploader_kind='agent' AND content_type LIKE 'image/%')", content.AttachmentID).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return platform.Fail(403, "welcome_image_forbidden", "图片必须来自本租户成员上传的图片附件")
			}
		} else if content.AttachmentID != "" {
			return platform.Fail(400, "invalid_welcome_image", "仅图片欢迎内容可以附带图片编号")
		}
		if content.Type == "options" {
			if len(content.Options) < 1 || len(content.Options) > 8 {
				return platform.Fail(400, "invalid_welcome_options", "请配置1至8个快捷选项")
			}
			seen := map[string]bool{}
			for _, option := range content.Options {
				if len(option.ID) < 1 || len(option.ID) > 64 || seen[option.ID] || strings.TrimSpace(option.Label) == "" || len([]rune(option.Label)) > 80 || strings.TrimSpace(option.Value) == "" || len([]rune(option.Value)) > 1000 || option.ClientID != "" {
					return platform.Fail(400, "invalid_welcome_options", "选项编号须唯一，显示文案最多80字，发送内容最多1000字；消息编号由服务端生成")
				}
				seen[option.ID] = true
			}
		} else if len(content.Options) > 0 {
			return platform.Fail(400, "invalid_welcome_options", "仅选项欢迎内容可以配置快捷选项")
		}
	}
	return nil
}

// welcomeRules 维护真实持久规则，更新和归档都通过版本避免静默覆盖。
func (s *Service) welcomeRules(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	input := welcomeRule{ScopeType: "tenant", Scenario: "all", Enabled: true, StopOnAgent: true}
	if r.Method != http.MethodGet {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
	}
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodGet {
			rows, err := tx.Query(ctx, "SELECT to_jsonb(w) FROM welcome_rules w WHERE NOT archived ORDER BY scope_type,scope_id,scenario,name,id")
			if err != nil {
				return err
			}
			items, err := jsonRows(rows)
			result = map[string]any{"items": items}
			return err
		}
		if r.Method != http.MethodDelete {
			if err := validateWelcomeRule(ctx, tx, input); err != nil {
				return err
			}
		}
		var item map[string]any
		var err error
		if r.Method == http.MethodPost {
			item, err = txJSON(ctx, tx, "INSERT INTO welcome_rules(tenant_id,name,scope_type,scope_id,scenario,enabled,stop_on_agent,contents) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8) RETURNING to_jsonb(welcome_rules)", a.TenantID, input.Name, input.ScopeType, input.ScopeID, input.Scenario, input.Enabled, input.StopOnAgent, jsonValue(input.Contents))
		} else {
			id := r.PathValue("id")
			if !uuidPattern.MatchString(id) || input.Version < 1 {
				return platform.Fail(400, "invalid_welcome_version", "规则编号和当前版本不能为空")
			}
			if r.Method == http.MethodDelete {
				item, err = txJSON(ctx, tx, "UPDATE welcome_rules SET archived=true,enabled=false,version=version+1,updated_at=now() WHERE id=$1 AND version=$2 AND NOT archived RETURNING to_jsonb(welcome_rules)", id, input.Version)
			} else {
				item, err = txJSON(ctx, tx, "UPDATE welcome_rules SET name=$3,scope_type=$4,scope_id=NULLIF($5,'')::uuid,scenario=$6,enabled=$7,stop_on_agent=$8,contents=$9,version=version+1,updated_at=now() WHERE id=$1 AND version=$2 AND NOT archived RETURNING to_jsonb(welcome_rules)", id, input.Version, input.Name, input.ScopeType, input.ScopeID, input.Scenario, input.Enabled, input.StopOnAgent, jsonValue(input.Contents))
			}
			if err == pgx.ErrNoRows {
				return platform.Fail(409, "welcome_version_conflict", "规则已更新、归档或不属于当前租户，请重新读取")
			}
		}
		if err != nil {
			return err
		}
		result = item
		return audit(ctx, tx, a, "welcome_rule.updated", item["id"].(string), map[string]any{"version": item["version"], "archived": r.Method == http.MethodDelete})
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// resolveWelcomeRule 按链接、服务空间、租户的顺序选择一组启用配置，同层优先具体营业场景。
func resolveWelcomeRule(ctx context.Context, tx pgx.Tx, tenant, space, link string, point time.Time) (*welcomeRule, automationConfig, bool, error) {
	config, err := loadAutomationConfig(ctx, tx, tenant)
	if err != nil {
		return nil, config, false, err
	}
	open, err := businessAt(config, point)
	if err != nil {
		return nil, config, false, err
	}
	scenario := "outside"
	if open {
		scenario = "business"
	}
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT to_jsonb(w) FROM welcome_rules w WHERE enabled AND NOT archived AND scenario IN('all',$3) AND (scope_type='tenant' OR scope_type='space' AND scope_id=$1::uuid OR scope_type='link' AND scope_id=$2::uuid) ORDER BY CASE scope_type WHEN 'link' THEN 3 WHEN 'space' THEN 2 ELSE 1 END DESC,(scenario=$3) DESC,id LIMIT 1", space, link, scenario).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, config, open, nil
	}
	if err != nil {
		return nil, config, open, err
	}
	var item welcomeRule
	err = json.Unmarshal(raw, &item)
	return &item, config, open, err
}

// enqueueRichWelcome 将最终覆盖内容冻结到独立任务，迟到重试仍指向原咨询。
func (s *Service) enqueueRichWelcome(ctx context.Context, tx pgx.Tx, tenant, conversation string) (bool, error) {
	var space, link string
	var created time.Time
	if err := tx.QueryRow(ctx, "SELECT service_space_id::text,created_entry_id::text,created_at FROM conversations WHERE id=$1", conversation).Scan(&space, &link, &created); err != nil {
		return false, err
	}
	rule, config, _, err := resolveWelcomeRule(ctx, tx, tenant, space, link, created)
	if err != nil || rule == nil {
		return false, err
	}
	for index, content := range rule.Contents {
		options := append([]welcomeOption(nil), content.Options...)
		for i := range options {
			options[i].ClientID = newID()
		}
		payload := map[string]any{"content": content.Content, "message_type": content.Type, "attachment_id": content.AttachmentID, "options": options, "welcome_rule_id": rule.ID, "welcome_rule_version": rule.Version, "policy_hash": welcomePolicyHash(config), "stop_on_agent": rule.StopOnAgent}
		_, err = tx.Exec(ctx, "INSERT INTO chat_operation_jobs(tenant_id,conversation_id,job_type,trigger_key,payload,run_after,sort_order) VALUES($1,$2,'welcome',$3,$4,$5,$6) ON CONFLICT(tenant_id,trigger_key) DO NOTHING", tenant, conversation, "welcome:"+conversation+":"+rule.ID+":"+strconv.Itoa(index), jsonValue(payload), created.Add(time.Duration(content.DelaySeconds)*time.Second), index)
		if err != nil {
			return true, err
		}
	}
	return true, nil
}

// richWelcomeCancelled 在执行前重查继承、日夜条件和规则版本，已失效任务只取消不改写历史。
func richWelcomeCancelled(ctx context.Context, tx pgx.Tx, tenant, conversation string, payload map[string]any) (bool, error) {
	var space, link string
	if err := tx.QueryRow(ctx, "SELECT service_space_id::text,created_entry_id::text FROM conversations WHERE id=$1", conversation).Scan(&space, &link); err != nil {
		return false, err
	}
	rule, config, _, err := resolveWelcomeRule(ctx, tx, tenant, space, link, time.Now())
	if err != nil {
		return false, err
	}
	if rule == nil || rule.ID != payload["welcome_rule_id"] || float64(rule.Version) != payload["welcome_rule_version"] || payload["policy_hash"] != welcomePolicyHash(config) {
		return true, nil
	}
	if stop, _ := payload["stop_on_agent"].(bool); stop {
		var accepted bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM conversation_assignments WHERE conversation_id=$1 AND accepted_at IS NOT NULL)", conversation).Scan(&accepted); err != nil {
			return false, err
		}
		return accepted, nil
	}
	return false, nil
}

// welcomePreview 展示某链接在指定时刻实际生效的层级和内容，不建立咨询或自动消息。
func (s *Service) welcomePreview(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	point := time.Now()
	if value := r.URL.Query().Get("at"); value != "" {
		var err error
		point, err = time.Parse(time.RFC3339, value)
		if err != nil {
			returnError(w, r, "invalid_preview_time", "预览时间须为带时区的ISO时间")
			return
		}
	}
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		link := r.URL.Query().Get("access_link_id")
		var space string
		if link == "" {
			if err := tx.QueryRow(r.Context(), "SELECT id::text,service_space_id::text FROM access_links WHERE is_default LIMIT 1").Scan(&link, &space); err != nil {
				return err
			}
		} else {
			if !uuidPattern.MatchString(link) {
				return platform.Fail(400, "invalid_link", "请选择合法链接")
			}
			if err := tx.QueryRow(r.Context(), "SELECT service_space_id::text FROM access_links WHERE id=$1", link).Scan(&space); err != nil {
				return err
			}
		}
		rule, config, open, err := resolveWelcomeRule(r.Context(), tx, a.TenantID, space, link, point)
		if err != nil {
			return err
		}
		settings, err := ensureSettings(r.Context(), tx, a.TenantID)
		if err != nil {
			return err
		}
		result = map[string]any{"access_link_id": link, "timezone": config.Timezone, "business_open": open, "at": point, "enabled": settings["welcome_enabled"], "source": "legacy", "rule": rule}
		if rule != nil {
			result["source"] = rule.ScopeType
			result["contents"] = rule.Contents
		} else {
			result["contents"] = []map[string]any{{"type": "text", "content": settings["welcome_text"], "delay_seconds": settings["welcome_delay_seconds"]}}
		}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}
