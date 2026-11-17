// 文件作用说明：读取和保存已认证账号的个人语言、显示时区，不修改企业业务时区。
package auth

import (
	"net/http"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
)

// userPreferences 返回个人偏好；更新字段严格验证且只作用于当前认证账号。
func (s *Service) userPreferences(w http.ResponseWriter, r *http.Request) error {
	principal := platform.Current(r)
	if r.Method == http.MethodPatch {
		var input struct {
			Locale   *string `json:"locale"`
			Timezone *string `json:"timezone"`
		}
		if err := platform.Decode(w, r, &input); err != nil {
			return err
		}
		if input.Locale != nil {
			if value := localization.Normalize(*input.Locale); value != "" {
				input.Locale = &value
			} else {
				return platform.Fail(400, "invalid_locale", "请选择支持的语言")
			}
		}
		if input.Timezone != nil && !localization.ValidTimezone(*input.Timezone) {
			return platform.Fail(400, "invalid_timezone", "请使用明确的IANA时区，例如Asia/Shanghai")
		}
		if _, err := s.pool.Exec(r.Context(), "UPDATE users SET preferred_locale=coalesce($1,preferred_locale),display_timezone=coalesce($2,display_timezone) WHERE id=$3", input.Locale, input.Timezone, principal.UserID); err != nil {
			return err
		}
	}
	var locale, timezone string
	if err := s.pool.QueryRow(r.Context(), "SELECT preferred_locale,display_timezone FROM users WHERE id=$1", principal.UserID).Scan(&locale, &timezone); err != nil {
		return err
	}
	platform.JSON(w, 200, map[string]any{"locale": locale, "timezone": timezone})
	return nil
}

// registrationTimezone 在创建企业时采用已经选择的设备时区，旧客户端回退上海时区。
func registrationTimezone(r *http.Request) string {
	value := r.Header.Get("X-GoodChat-Timezone")
	if localization.ValidTimezone(value) {
		return value
	}
	return "Asia/Shanghai"
}
