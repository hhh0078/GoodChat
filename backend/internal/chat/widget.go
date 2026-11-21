// 文件作用说明：管理链接级悬浮窗设置并输出无需登录、可跨站加载的独立客服脚本。
package chat

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

//go:embed widget.js
var widgetScript string

// widgetSettings 只接受展示字段，访客身份与接待权限仍由原有聊天接口控制。
type widgetSettings struct {
	Enabled          bool   `json:"enabled"`
	Label            string `json:"label"`
	LogoURL          string `json:"logo_url"`
	Color            string `json:"color"`
	Side             string `json:"side"`
	HorizontalOffset int    `json:"horizontal_offset"`
	BottomOffset     int    `json:"bottom_offset"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	ShowDelay        int    `json:"show_delay_seconds"`
	AutoOpenDelay    int    `json:"auto_open_delay_seconds"`
	ScheduleEnabled  bool   `json:"schedule_enabled"`
	Start            string `json:"start"`
	End              string `json:"end"`
	Weekdays         []int  `json:"weekdays"`
}

// defaultWidgetSettings 为既有链接提供可直接嵌入的钴蓝悬浮按钮，默认不自动弹窗。
func defaultWidgetSettings() widgetSettings {
	return widgetSettings{Enabled: true, Label: "在线客服", Color: "#2459DB", Side: "right", HorizontalOffset: 24, BottomOffset: 24, Width: 400, Height: 640, AutoOpenDelay: -1, Start: "00:00", End: "23:59", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}}
}

// validateWidgetSettings 限制尺寸、文本和地址，不允许把脚本或CSS注入宿主页面。
func validateWidgetSettings(in widgetSettings) error {
	valid := in.Label != "" && len([]rune(in.Label)) <= 40 && regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(in.Color) && inStrings(in.Side, "left", "right")
	valid = valid && in.HorizontalOffset >= 8 && in.HorizontalOffset <= 200 && in.BottomOffset >= 8 && in.BottomOffset <= 200 && in.Width >= 320 && in.Width <= 800 && in.Height >= 400 && in.Height <= 1000
	valid = valid && in.ShowDelay >= 0 && in.ShowDelay <= 3600 && in.AutoOpenDelay >= -1 && in.AutoOpenDelay <= 3600 && len(in.Weekdays) > 0 && len(in.Weekdays) <= 7
	seen := map[int]bool{}
	for _, day := range in.Weekdays {
		valid = valid && day >= 0 && day <= 6 && !seen[day]
		seen[day] = true
	}
	for _, value := range []string{in.Start, in.End} {
		_, err := time.Parse("15:04", value)
		valid = valid && err == nil && len(value) == 5
	}
	if in.LogoURL != "" {
		u, err := url.Parse(in.LogoURL)
		valid = valid && err == nil && len(in.LogoURL) <= 2048
		if err == nil {
			valid = valid && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
		}
	}
	if !valid {
		return platform.Fail(400, "widget_invalid", "请检查悬浮窗配置")
	}
	return nil
}

// linkWidget 以租户认证和配置版本约束读取保存，不修改原有路由与装修配置。
func (s *Service) linkWidget(w http.ResponseWriter, r *http.Request) {
	if !uuidPattern.MatchString(r.PathValue("id")) {
		returnError(w, r, "invalid_id", "资源编号格式不正确")
		return
	}
	a := memberActor(r)
	in := struct {
		widgetSettings
		Version int64 `json:"version"`
	}{widgetSettings: defaultWidgetSettings()}
	if r.Method == http.MethodPut {
		if err := platform.Decode(w, r, &in); err != nil {
			replyError(w, r, err)
			return
		}
		in.Label = strings.TrimSpace(in.Label)
		if err := validateWidgetSettings(in.widgetSettings); err != nil {
			replyError(w, r, err)
			return
		}
	}
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := tenantActive(r.Context(), tx, a.TenantID); err != nil {
			return err
		}
		if r.Method == http.MethodPut {
			tag, err := tx.Exec(r.Context(), "UPDATE access_links SET widget_settings=$2,widget_version=widget_version+1 WHERE id=$1 AND widget_version=$3", r.PathValue("id"), jsonValue(in.widgetSettings), in.Version)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return platform.Fail(409, "widget_version_conflict", "内容已更新，请刷新后重试")
			}
		}
		var raw []byte
		if err := tx.QueryRow(r.Context(), "SELECT widget_settings,widget_version FROM access_links WHERE id=$1", r.PathValue("id")).Scan(&raw, &in.Version); err != nil {
			return err
		}
		return json.Unmarshal(raw, &in.widgetSettings)
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, in)
}

// publicWidget 每次加载读取最新设置，禁用、过期或冻结的入口不再创建悬浮窗。
func (s *Service) publicWidget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	key := strings.TrimSuffix(r.PathValue("entry_script"), ".js")
	tenant, err := s.entryTenant(r.Context(), key)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	config := struct {
		widgetSettings
		Origin   string `json:"origin"`
		EntryKey string `json:"entry_key"`
		Timezone string `json:"timezone"`
	}{widgetSettings: defaultWidgetSettings(), Origin: platform.PublicURL(r.Context(), s.cfg.PublicURL), EntryKey: key}
	err = platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		if err := tenantActive(r.Context(), tx, tenant); err != nil {
			return err
		}
		var raw []byte
		if err := tx.QueryRow(r.Context(), "SELECT l.widget_settings,t.timezone FROM access_links l JOIN tenants t ON t.id=l.tenant_id WHERE l.entry_key=$1 AND l.status='active' AND (l.expires_at IS NULL OR l.expires_at>now())", key).Scan(&raw, &config.Timezone); err != nil {
			return err
		}
		return json.Unmarshal(raw, &config.widgetSettings)
	})
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if !config.Enabled {
		_, _ = w.Write([]byte("/* GoodChat widget disabled. */"))
		return
	}
	raw, _ := json.Marshal(config)
	_, _ = w.Write([]byte("(function(){const config=" + string(raw) + ";\n" + widgetScript + "\n})();"))
}
