// 文件作用说明：保存租户营业欢迎场景和评价期限策略，以租户时区解释跨日营业时间。
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

// businessWindow 表示起始星期的一个营业时段，结束时间较小时跨到次日。
type businessWindow struct {
	Day   int    `json:"day"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// automationConfig 保存欢迎场景与评价策略，不改变真实客服在线和会话分配状态。
type automationConfig struct {
	BusinessHoursEnabled      bool             `json:"business_hours_enabled"`
	WeeklyHours               []businessWindow `json:"weekly_hours"`
	StopWelcomeOnAgent        bool             `json:"stop_welcome_on_agent"`
	EvaluationEnabled         bool             `json:"evaluation_enabled"`
	EvaluationWindowHours     int              `json:"evaluation_window_hours"`
	EvaluationAllowEdit       bool             `json:"evaluation_allow_edit"`
	EvaluationEditWindowHours int              `json:"evaluation_edit_window_hours"`
	EvaluationTags            []string         `json:"evaluation_tags"`
	Version                   int              `json:"version,omitempty"`
	Timezone                  string           `json:"timezone,omitempty"`
}

// defaultAutomationConfig 使用明确可调整的首版策略，不以默认值虚构已保存配置。
func defaultAutomationConfig() automationConfig {
	return automationConfig{WeeklyHours: []businessWindow{}, StopWelcomeOnAgent: true, EvaluationEnabled: true, EvaluationWindowHours: 168, EvaluationEditWindowHours: 24, EvaluationTags: []string{"响应及时", "解决问题", "态度友好"}}
}

// loadAutomationConfig 在租户事务内确保默认行存在，并读取当前租户真实时区。
func loadAutomationConfig(ctx context.Context, tx pgx.Tx, tenant string) (automationConfig, error) {
	config := defaultAutomationConfig()
	if _, err := tx.Exec(ctx, "INSERT INTO automation_settings(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", tenant); err != nil {
		return config, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT a.settings,a.version,t.timezone FROM automation_settings a JOIN tenants t ON t.id=a.tenant_id WHERE a.tenant_id=$1", tenant).Scan(&raw, &config.Version, &config.Timezone); err != nil {
		return config, err
	}
	version, timezone := config.Version, config.Timezone
	if err := json.Unmarshal(raw, &config); err != nil {
		return config, err
	}
	config.Version, config.Timezone = version, timezone
	return config, nil
}

// minutesOfDay 严格读取二十四小时制的时分，不接受模糊或越界时间。
func minutesOfDay(value string) (int, bool) {
	if len(value) != 5 || value[2] != ':' {
		return 0, false
	}
	hour, e1 := strconv.Atoi(value[:2])
	minute, e2 := strconv.Atoi(value[3:])
	return hour*60 + minute, e1 == nil && e2 == nil && hour >= 0 && hour < 24 && minute >= 0 && minute < 60
}

// validateAutomationConfig 校验完整策略，禁止无限评价期或无法解释的营业时间。
func validateAutomationConfig(config automationConfig) error {
	if config.Version < 1 || len(config.WeeklyHours) > 28 || config.EvaluationWindowHours < 1 || config.EvaluationWindowHours > 720 || config.EvaluationEditWindowHours < 1 || config.EvaluationEditWindowHours > 168 || len(config.EvaluationTags) > 12 {
		return platform.Fail(400, "invalid_automation_settings", "版本、评价期限或营业时段数量不正确")
	}
	for _, window := range config.WeeklyHours {
		start, a := minutesOfDay(window.Start)
		end, b := minutesOfDay(window.End)
		if window.Day < 0 || window.Day > 6 || !a || !b || start == end {
			return platform.Fail(400, "invalid_business_hours", "营业时间需要星期0至6和有效时分；跨日可将结束设为较早时间")
		}
	}
	seen := map[string]bool{}
	for _, tag := range config.EvaluationTags {
		if tag != strings.TrimSpace(tag) || tag == "" || len([]rune(tag)) > 30 || seen[tag] {
			return platform.Fail(400, "invalid_evaluation_tags", "评价标签不可为空、重复或超过30字")
		}
		seen[tag] = true
	}
	return nil
}

// businessAt 判断本地自然时间是否落在任一营业区间，采用含开始不含结束规则。
func businessAt(config automationConfig, point time.Time) (bool, error) {
	if !config.BusinessHoursEnabled {
		return true, nil
	}
	zone, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return false, err
	}
	local := point.In(zone)
	weekday := int(local.Weekday())
	minute := local.Hour()*60 + local.Minute()
	for _, window := range config.WeeklyHours {
		start, validStart := minutesOfDay(window.Start)
		end, validEnd := minutesOfDay(window.End)
		if !validStart || !validEnd {
			continue
		}
		if start < end && weekday == window.Day && minute >= start && minute < end {
			return true, nil
		}
		if start > end && (weekday == window.Day && minute >= start || weekday == (window.Day+1)%7 && minute < end) {
			return true, nil
		}
	}
	return false, nil
}

// welcomePolicyHash 只追踪影响欢迎任务有效性的策略，改评价设置不会撤销欢迎任务。
func welcomePolicyHash(config automationConfig) string {
	return hash(string(jsonValue(map[string]any{"enabled": config.BusinessHoursEnabled, "hours": config.WeeklyHours, "timezone": config.Timezone, "stop_on_agent": config.StopWelcomeOnAgent})))
}

// automationSettings 以版本保护完整更新场景与评价设置，普通客服没有配置权限。
func (s *Service) automationSettings(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var input automationConfig
	if r.Method == http.MethodPut {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
		if err := validateAutomationConfig(input); err != nil {
			replyError(w, r, err)
			return
		}
	}
	var result automationConfig
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		var err error
		result, err = loadAutomationConfig(r.Context(), tx, a.TenantID)
		if err != nil || r.Method == http.MethodGet {
			return err
		}
		version := input.Version
		input.Version = 0
		input.Timezone = ""
		tag, err := tx.Exec(r.Context(), "UPDATE automation_settings SET settings=$2,version=version+1,updated_at=now() WHERE tenant_id=$1 AND version=$3", a.TenantID, jsonValue(input), version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return platform.Fail(409, "automation_version_conflict", "策略已由其他成员更新，请重新读取后保存")
		}
		if err := audit(r.Context(), tx, a, "automation_settings.updated", "", map[string]any{"version": version + 1}); err != nil {
			return err
		}
		result, err = loadAutomationConfig(r.Context(), tx, a.TenantID)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// registerAutomationRich 注册欢迎规则、场景预览与评价历史，保持现有基础接口兼容。
func (s *Service) registerAutomationRich(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenant/lead-settings", s.auth.RequireAdmin(s.leadSettings))
	mux.HandleFunc("PUT /api/v1/tenant/lead-settings", s.auth.RequireAdmin(s.leadSettings))
	mux.HandleFunc("GET /api/v1/tenant/lead-notifications", s.auth.RequireAdmin(s.leadNotifications))
	mux.HandleFunc("POST /api/v1/tenant/lead-notifications/{id}/retry", s.auth.RequireAdmin(s.leadNotifications))
	mux.HandleFunc("GET /api/v1/tenant/automation-settings", s.auth.RequireAdmin(s.automationSettings))
	mux.HandleFunc("PUT /api/v1/tenant/automation-settings", s.auth.RequireAdmin(s.automationSettings))
	mux.HandleFunc("GET /api/v1/tenant/welcome-rules", s.auth.RequireAdmin(s.welcomeRules))
	mux.HandleFunc("POST /api/v1/tenant/welcome-rules", s.auth.RequireAdmin(s.welcomeRules))
	mux.HandleFunc("PATCH /api/v1/tenant/welcome-rules/{id}", s.auth.RequireAdmin(s.welcomeRules))
	mux.HandleFunc("DELETE /api/v1/tenant/welcome-rules/{id}", s.auth.RequireAdmin(s.welcomeRules))
	mux.HandleFunc("GET /api/v1/tenant/welcome-preview", s.auth.RequireAdmin(s.welcomePreview))
	mux.HandleFunc("GET /api/v1/conversations/{id}/evaluation", s.withActor(s.evaluationState))
	mux.HandleFunc("GET /api/v1/tenant/evaluations/{id}/versions", s.auth.RequireAdmin(s.evaluationVersions))
}
