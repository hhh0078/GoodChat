// 文件作用说明：提供当前客服个人欢迎语、关键词规则及已启用团队规则的只读视图，服务端固定成员归属。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// registerPersonalAutomation 注册成员自己的自动回复管理接口，公共规则仍由原管理员接口维护。
func (s *Service) registerPersonalAutomation(mux *http.ServeMux) {
	for _, resource := range []string{"welcome-rules", "keyword-rules"} {
		handler := s.personalWelcomeRules
		if resource == "keyword-rules" {
			handler = s.personalKeywordRules
		}
		mux.HandleFunc("GET /api/v1/me/"+resource, s.auth.RequireTenant(handler))
		mux.HandleFunc("POST /api/v1/me/"+resource, s.auth.RequireTenant(handler))
		mux.HandleFunc("PATCH /api/v1/me/"+resource+"/{id}", s.auth.RequireTenant(handler))
		mux.HandleFunc("DELETE /api/v1/me/"+resource+"/{id}", s.auth.RequireTenant(handler))
	}
}

// personalWelcomeRules 仅写入本人规则，已开启团队欢迎与默认欢迎在个人管理内只读展示。
func (s *Service) personalWelcomeRules(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	input := welcomeRule{Scenario: "all", Enabled: true, Contents: []welcomeContent{}}
	if r.Method != http.MethodGet {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
	}
	input.ScopeType, input.ScopeID, input.StopOnAgent = "member", a.MemberID, false
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodGet {
			settings, err := ensureSettings(ctx, tx, a.TenantID)
			if err != nil {
				return err
			}
			enabled, _ := settings["welcome_enabled"].(bool)
			rows, err := tx.Query(ctx, `SELECT to_jsonb(w)||jsonb_build_object('editable',scope_type='member','source_scope',CASE WHEN scope_type='member' THEN 'personal' ELSE scope_type END) FROM welcome_rules w WHERE NOT archived AND (scope_type='member' AND scope_id=$1 OR scope_type<>'member' AND enabled AND $2) ORDER BY scope_type='member' DESC,scope_type,scenario,name,id`, a.MemberID, enabled)
			if err != nil {
				return err
			}
			items, err := jsonRows(rows)
			if err != nil {
				return err
			}
			text, _ := settings["welcome_text"].(string)
			if enabled && text != "" {
				items = append(items, map[string]any{"id": "legacy", "name": "系统默认欢迎语", "scope_type": "tenant", "source_scope": "legacy", "editable": false, "enabled": true, "scenario": "all", "stop_on_agent": true, "contents": []map[string]any{{"type": "text", "content": text, "delay_seconds": settings["welcome_delay_seconds"]}}})
			}
			result = map[string]any{"items": items}
			return nil
		}
		id := r.PathValue("id")
		if r.Method != http.MethodPost {
			if !uuidPattern.MatchString(id) || input.Version < 1 {
				return platform.Fail(400, "invalid_welcome_version", "规则编号和当前版本不能为空")
			}
			var owned bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM welcome_rules WHERE id=$1 AND scope_type='member' AND scope_id=$2 AND NOT archived)`, id, a.MemberID).Scan(&owned); err != nil {
				return err
			}
			if !owned {
				return platform.Fail(403, "personal_rule_forbidden", "只能操作自己的欢迎语")
			}
		}
		if r.Method != http.MethodDelete {
			if err := validateWelcomeRule(ctx, tx, input); err != nil {
				return err
			}
			for _, content := range input.Contents {
				if content.Type != "image" {
					continue
				}
				var own bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE id=$1 AND uploader_kind='agent' AND uploader_id=$2)`, content.AttachmentID, a.MemberID).Scan(&own); err != nil {
					return err
				}
				if !own {
					return platform.Fail(403, "personal_welcome_attachment_forbidden", "个人欢迎语只能使用本人上传的图片")
				}
			}
		}
		var item map[string]any
		var err error
		if r.Method == http.MethodPost {
			item, err = txJSON(ctx, tx, `INSERT INTO welcome_rules(tenant_id,name,scope_type,scope_id,scenario,enabled,stop_on_agent,contents) VALUES($1,$2,'member',$3,$4,$5,false,$6) RETURNING to_jsonb(welcome_rules)`, a.TenantID, input.Name, a.MemberID, input.Scenario, input.Enabled, jsonValue(input.Contents))
		} else if r.Method == http.MethodDelete {
			item, err = txJSON(ctx, tx, `UPDATE welcome_rules SET archived=true,enabled=false,version=version+1,updated_at=now() WHERE id=$1 AND scope_type='member' AND scope_id=$2 AND version=$3 AND NOT archived RETURNING to_jsonb(welcome_rules)`, id, a.MemberID, input.Version)
		} else {
			item, err = txJSON(ctx, tx, `UPDATE welcome_rules SET name=$4,scenario=$5,enabled=$6,stop_on_agent=false,contents=$7,version=version+1,updated_at=now() WHERE id=$1 AND scope_type='member' AND scope_id=$2 AND version=$3 AND NOT archived RETURNING to_jsonb(welcome_rules)`, id, a.MemberID, input.Version, input.Name, input.Scenario, input.Enabled, jsonValue(input.Contents))
		}
		if err == pgx.ErrNoRows {
			return platform.Fail(409, "welcome_version_conflict", "欢迎语已更新，请重新读取后保存")
		}
		if err != nil {
			return err
		}
		item["editable"], item["source_scope"] = true, "personal"
		result = item
		return audit(ctx, tx, a, "personal_welcome.updated", item["id"].(string), map[string]any{"version": item["version"]})
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// personalKeyword 保存客服自己的文字匹配、冷却和并发版本。
type personalKeyword struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Mode     string `json:"match_mode"`
	Pattern  string `json:"pattern"`
	Reply    string `json:"reply"`
	Priority int32  `json:"priority"`
	Cooldown int32  `json:"cooldown_seconds"`
	Enabled  bool   `json:"enabled"`
	Version  int    `json:"version"`
}

// personalKeywordRules 通过当前认证成员隔离个人规则，禁止管理员从个人入口修改公共规则。
func (s *Service) personalKeywordRules(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	values := map[string]any{}
	if r.Method != http.MethodGet {
		if err := platform.Decode(w, r, &values); err != nil {
			replyError(w, r, err)
			return
		}
		allowed := fieldSet("name match_mode pattern reply priority cooldown_seconds enabled version")
		for key := range values {
			if !allowed[key] {
				returnError(w, r, "unsupported_field", "不支持该字段："+key)
				return
			}
		}
		if r.Method != http.MethodDelete {
			if err := validateOperation("keyword-rules", values, r.Method == http.MethodPost); err != nil {
				replyError(w, r, err)
				return
			}
		}
	}
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodGet {
			settings, err := ensureSettings(ctx, tx, a.TenantID)
			if err != nil {
				return err
			}
			enabled, _ := settings["keywords_enabled"].(bool)
			rows, err := tx.Query(ctx, `SELECT to_jsonb(k)||jsonb_build_object('editable',owner_membership_id IS NOT NULL,'source_scope',CASE WHEN owner_membership_id IS NULL THEN 'tenant' ELSE 'personal' END) FROM keyword_rules k WHERE NOT archived AND (owner_membership_id=$1 OR owner_membership_id IS NULL AND enabled AND $2) ORDER BY owner_membership_id IS NOT NULL DESC,priority DESC,id`, a.MemberID, enabled)
			if err != nil {
				return err
			}
			items, err := jsonRows(rows)
			result = map[string]any{"items": items}
			return err
		}
		item := personalKeyword{ID: newID(), Mode: "contains", Cooldown: 60, Enabled: true, Version: 1}
		if r.Method != http.MethodPost {
			id := r.PathValue("id")
			version, ok := values["version"].(float64)
			if !uuidPattern.MatchString(id) || !ok || version < 1 || version != float64(int(version)) {
				return platform.Fail(400, "invalid_keyword_version", "规则编号和当前版本不能为空")
			}
			raw, err := txJSON(ctx, tx, `SELECT to_jsonb(k) FROM keyword_rules k WHERE id=$1 AND owner_membership_id=$2 AND NOT archived FOR UPDATE`, id, a.MemberID)
			if err == pgx.ErrNoRows {
				return platform.Fail(403, "personal_rule_forbidden", "只能操作自己的关键词回复")
			}
			if err != nil {
				return err
			}
			data, _ := json.Marshal(raw)
			if err = json.Unmarshal(data, &item); err != nil {
				return err
			}
			if version != float64(item.Version) {
				return platform.Fail(409, "keyword_version_conflict", "关键词规则已更新，请重新读取后保存")
			}
		}
		if r.Method == http.MethodDelete {
			_, err := tx.Exec(ctx, `UPDATE keyword_rules SET archived=true,enabled=false,version=version+1,updated_at=now() WHERE id=$1 AND owner_membership_id=$2`, item.ID, a.MemberID)
			if err != nil {
				return err
			}
			result = map[string]any{"id": item.ID, "enabled": false}
			return audit(ctx, tx, a, "personal_keyword.deleted", item.ID, nil)
		}
		raw, err := json.Marshal(values)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return platform.Fail(400, "invalid_keyword_rule", "请检查开关、整数优先级和冷却时间")
		}
		if item.Cooldown < 0 || item.Cooldown > 86400 {
			return platform.Fail(400, "invalid_cooldown", "冷却时间应为0至86400秒整数")
		}
		if r.Method == http.MethodPost {
			_, err = tx.Exec(ctx, `INSERT INTO keyword_rules(id,tenant_id,owner_membership_id,name,match_mode,pattern,reply,priority,cooldown_seconds,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, item.ID, a.TenantID, a.MemberID, item.Name, item.Mode, item.Pattern, item.Reply, item.Priority, item.Cooldown, item.Enabled)
		} else {
			_, err = tx.Exec(ctx, `UPDATE keyword_rules SET name=$3,match_mode=$4,pattern=$5,reply=$6,priority=$7,cooldown_seconds=$8,enabled=$9,version=version+1,updated_at=now() WHERE id=$1 AND owner_membership_id=$2`, item.ID, a.MemberID, item.Name, item.Mode, item.Pattern, item.Reply, item.Priority, item.Cooldown, item.Enabled)
		}
		if err != nil {
			return err
		}
		record, err := txJSON(ctx, tx, `SELECT to_jsonb(k)||jsonb_build_object('editable',true,'source_scope','personal') FROM keyword_rules k WHERE id=$1 AND owner_membership_id=$2`, item.ID, a.MemberID)
		if err != nil {
			return err
		}
		result = record
		return audit(ctx, tx, a, "personal_keyword.updated", item.ID, nil)
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// personalWelcomeRule 只按当前正式接待成员解析个人问候，同层具体营业场景优先。
func personalWelcomeRule(ctx context.Context, tx pgx.Tx, tenant, member string) (*welcomeRule, error) {
	config, err := loadAutomationConfig(ctx, tx, tenant)
	if err != nil {
		return nil, err
	}
	open, err := businessAt(config, time.Now())
	if err != nil {
		return nil, err
	}
	scenario := "outside"
	if open {
		scenario = "business"
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT to_jsonb(w) FROM welcome_rules w WHERE scope_type='member' AND scope_id=$1 AND enabled AND NOT archived AND scenario IN('all',$2) ORDER BY (scenario=$2) DESC,id LIMIT 1`, member, scenario).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rule welcomeRule
	if err = json.Unmarshal(raw, &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}
