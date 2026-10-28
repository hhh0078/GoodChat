// 文件作用说明：实现租户接待配置、公共个人话术、关键词规则和通知偏好的真实管理接口。
package chat

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// registerOperations 注册接待效率和反馈接口，写操作保留成员或管理员边界。
func (s *Service) registerOperations(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenant/chat-settings", s.auth.RequireTenant(s.chatSettings))
	mux.HandleFunc("PATCH /api/v1/tenant/chat-settings", s.auth.RequireAdmin(s.chatSettings))
	mux.HandleFunc("GET /api/v1/tenant/notification-preferences", s.auth.RequireTenant(s.preferences))
	mux.HandleFunc("PATCH /api/v1/tenant/notification-preferences", s.auth.RequireTenant(s.preferences))
	for _, name := range []string{"quick-replies", "keyword-rules"} {
		resource := name
		wrap := s.auth.RequireTenant
		if name == "keyword-rules" {
			wrap = s.auth.RequireAdmin
		}
		mux.HandleFunc("GET /api/v1/tenant/"+name, s.auth.RequireTenant(s.operationResources(resource)))
		mux.HandleFunc("POST /api/v1/tenant/"+name, wrap(s.operationResources(resource)))
		mux.HandleFunc("PATCH /api/v1/tenant/"+name+"/{id}", wrap(s.operationResources(resource)))
		mux.HandleFunc("DELETE /api/v1/tenant/"+name+"/{id}", wrap(s.operationResources(resource)))
	}
	mux.HandleFunc("POST /api/v1/visitor/leads", s.withActor(s.createLead))
	mux.HandleFunc("GET /api/v1/tenant/leads", s.auth.RequireTenant(s.leads))
	mux.HandleFunc("PATCH /api/v1/tenant/leads/{id}", s.auth.RequireTenant(s.leads))
	mux.HandleFunc("POST /api/v1/conversations/{id}/evaluation", s.withActor(s.evaluate))
	mux.HandleFunc("GET /api/v1/tenant/evaluations", s.auth.RequireAdmin(s.evaluations))
}

// ensureSettings 创建持久出厂配置，返回值不是未保存的前端模拟设置。
func ensureSettings(ctx context.Context, tx pgx.Tx, tenant string) (map[string]any, error) {
	if _, err := tx.Exec(ctx, "INSERT INTO chat_settings(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", tenant); err != nil {
		return nil, err
	}
	return txJSON(ctx, tx, "SELECT settings FROM chat_settings WHERE tenant_id=$1", tenant)
}

// chatSettings 读取或按类型校验后合并租户欢迎语配置。
func (s *Service) chatSettings(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	values := map[string]any{}
	if r.Method == http.MethodPatch {
		if err := platform.Decode(w, r, &values); err != nil {
			replyError(w, r, err)
			return
		}
		for key, value := range values {
			switch key {
			case "welcome_enabled", "keywords_enabled", "typing_enabled", "share_previous_chats":
				if _, ok := value.(bool); !ok {
					returnError(w, r, "invalid_setting", "设置开关必须为布尔值")
					return
				}
			case "welcome_text":
				text, ok := value.(string)
				if !ok || len([]rune(text)) > 2000 {
					returnError(w, r, "invalid_setting", "欢迎语不能超过2000字")
					return
				}
			case "welcome_delay_seconds":
				number, ok := value.(float64)
				if !ok || number < 0 || number > 60 || number != float64(int(number)) {
					returnError(w, r, "invalid_setting", "欢迎语延迟应为0至60秒整数")
					return
				}
			case "repeat_unread_seconds":
				number, ok := value.(float64)
				if !ok || number < 1 || number > 3600 || number != float64(int(number)) {
					returnError(w, r, "invalid_setting", "重复提醒间隔应为1至3600秒整数")
					return
				}
			case "history_agent_minutes":
				number, ok := value.(float64)
				if !ok || number < 0 || number > 525600 || number != float64(int(number)) {
					returnError(w, r, "invalid_setting", "历史客服窗口应为0至525600分钟整数，0表示永久")
					return
				}
			case "notification_sound_id":
				text, ok := value.(string)
				if !ok || !inStrings(text, "gentle", "glass", "wood", "bell", "bubble") {
					returnError(w, r, "invalid_setting", "请选择平台提供的提示音")
					return
				}
			default:
				returnError(w, r, "unsupported_setting", "不支持该接待设置："+key)
				return
			}
		}
	}
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		var err error
		result, err = ensureSettings(r.Context(), tx, a.TenantID)
		if err != nil {
			return err
		}
		if r.Method == http.MethodPatch {
			if _, err := tx.Exec(r.Context(), "UPDATE chat_settings SET settings=settings||$2::jsonb,updated_at=now() WHERE tenant_id=$1", a.TenantID, jsonValue(values)); err != nil {
				return err
			}
			if err := audit(r.Context(), tx, a, "chat_settings.updated", "", values); err != nil {
				return err
			}
			result, err = txJSON(r.Context(), tx, "SELECT settings FROM chat_settings WHERE tenant_id=$1", a.TenantID)
			return err
		}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// preferences 保存当前成员的跨设备偏好，浏览器授权能力仍由客户端真实检测。
func (s *Service) preferences(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	values := map[string]any{}
	if r.Method == http.MethodPatch {
		if err := platform.Decode(w, r, &values); err != nil {
			replyError(w, r, err)
			return
		}
		for key, value := range values {
			if key == "volume" {
				n, ok := value.(float64)
				if !ok || n < 0 || n > 1 {
					returnError(w, r, "invalid_preference", "音量应在0至1之间")
					return
				}
			} else if inStrings(key, "sound_enabled", "speech_enabled", "system_enabled", "content_preview", "muted") {
				if _, ok := value.(bool); !ok {
					returnError(w, r, "invalid_preference", "提醒开关必须为布尔值")
					return
				}
			} else {
				returnError(w, r, "unsupported_preference", "不支持该提醒设置："+key)
				return
			}
		}
	}
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), "INSERT INTO notification_preferences(tenant_id,membership_id) VALUES($1,$2) ON CONFLICT DO NOTHING", a.TenantID, a.MemberID); err != nil {
			return err
		}
		if r.Method == http.MethodPatch {
			if _, err := tx.Exec(r.Context(), "UPDATE notification_preferences SET settings=settings||$2::jsonb,updated_at=now() WHERE membership_id=$1", a.MemberID, jsonValue(values)); err != nil {
				return err
			}
		}
		var err error
		result, err = txJSON(r.Context(), tx, "SELECT settings FROM notification_preferences WHERE membership_id=$1", a.MemberID)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// operationResources 提供话术与关键词规则的持久增删改查。
func (s *Service) operationResources(name string) http.HandlerFunc {
	if name == "quick-replies" {
		return s.quickReplies
	}
	return func(w http.ResponseWriter, r *http.Request) {
		a := memberActor(r)
		table := "keyword_rules"
		allowed := fieldSet("name match_mode pattern reply priority cooldown_seconds enabled")
		values := map[string]any{}
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			if err := platform.Decode(w, r, &values); err != nil {
				replyError(w, r, err)
				return
			}
			for key := range values {
				if !allowed[key] {
					returnError(w, r, "unsupported_field", "不支持该字段："+key)
					return
				}
			}
			if err := validateOperation(name, values, r.Method == http.MethodPost); err != nil {
				replyError(w, r, err)
				return
			}
			for _, key := range []string{"priority", "cooldown_seconds"} {
				if value, ok := values[key]; ok {
					n, valid := value.(float64)
					if !valid || n != float64(int32(n)) {
						returnError(w, r, "invalid_integer", "排序、优先级和冷却值必须为有效整数")
						return
					}
					values[key] = int32(n)
				}
			}
		}
		var result any
		err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
			ctx := r.Context()
			if r.Method == http.MethodGet {
				rows, err := tx.Query(ctx, "SELECT to_jsonb(t) FROM keyword_rules t ORDER BY priority DESC,id")
				if err != nil {
					return err
				}
				items, err := jsonRows(rows)
				result = map[string]any{"items": items}
				return err
			}
			id := r.PathValue("id")
			if id != "" && !uuidPattern.MatchString(id) {
				return platform.Fail(400, "invalid_id", "记录编号格式不正确")
			}
			if r.Method == http.MethodDelete {
				tag, err := tx.Exec(ctx, "UPDATE "+table+" SET enabled=false,updated_at=now() WHERE id=$1", id)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 0 {
					return pgx.ErrNoRows
				}
				result = map[string]any{"id": id, "enabled": false}
				return audit(ctx, tx, a, name+".disabled", id, nil)
			}
			keys := []string{}
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			args := []any{}
			for _, key := range keys {
				args = append(args, values[key])
			}
			if r.Method == http.MethodPost {
				id = newID()
				columns := append([]string{"id", "tenant_id"}, keys...)
				args = append([]any{id, a.TenantID}, args...)
				marks := []string{}
				for i := range args {
					marks = append(marks, fmt.Sprintf("$%d", i+1))
				}
				if _, err := tx.Exec(ctx, "INSERT INTO "+table+"("+strings.Join(columns, ",")+") VALUES("+strings.Join(marks, ",")+")", args...); err != nil {
					return err
				}
			} else {
				parts := []string{"updated_at=now()"}
				for i, key := range keys {
					parts = append(parts, fmt.Sprintf("%s=$%d", key, i+1))
				}
				args = append(args, id)
				tag, err := tx.Exec(ctx, "UPDATE "+table+" SET "+strings.Join(parts, ",")+fmt.Sprintf(" WHERE id=$%d", len(args)), args...)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 0 {
					return pgx.ErrNoRows
				}
			}
			item, err := txJSON(ctx, tx, "SELECT to_jsonb(t) FROM "+table+" t WHERE id=$1", id)
			if err != nil {
				return err
			}
			result = item
			return audit(ctx, tx, a, name+".saved", id, values)
		})
		if err != nil {
			replyError(w, r, err)
			return
		}
		platform.JSON(w, 200, result)
	}
}

// validateOperation 校验话术正文和纯文本匹配规则，不开放任意代码或正则执行。
func validateOperation(name string, values map[string]any, create bool) error {
	required := []string{"title", "content"}
	if name == "keyword-rules" {
		required = []string{"name", "pattern", "reply"}
		if create {
			if _, ok := values["match_mode"]; !ok {
				values["match_mode"] = "contains"
			}
		}
	}
	for _, key := range required {
		value, ok := values[key]
		if !ok && !create {
			continue
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" || len([]rune(text)) > 2000 {
			return platform.Fail(400, "invalid_content", "请填写不超过2000字的有效标题和正文")
		}
	}
	if value, ok := values["scope"]; ok && !inStrings(fmt.Sprint(value), "public", "personal") {
		return platform.Fail(400, "invalid_scope", "话术范围应为个人或公共")
	}
	if value, ok := values["match_mode"]; ok && !inStrings(fmt.Sprint(value), "exact", "contains") {
		return platform.Fail(400, "invalid_match_mode", "匹配模式只支持完全或包含")
	}
	if value, ok := values["cooldown_seconds"]; ok {
		n, yes := value.(float64)
		if !yes || n < 0 || n > 86400 || n != float64(int(n)) {
			return platform.Fail(400, "invalid_cooldown", "冷却时间应为0至86400秒整数")
		}
	}
	return nil
}

// audit 保存不含认证凭证的接待运营变更。
func audit(ctx context.Context, tx pgx.Tx, a actor, action, target string, payload any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	_, err := tx.Exec(ctx, "INSERT INTO chat_audit_logs(tenant_id,actor_id,action,target_id,payload) VALUES($1,NULLIF($2,'')::uuid,$3,NULLIF($4,'')::uuid,$5)", a.TenantID, actorID(a), action, target, jsonValue(payload))
	return err
}
