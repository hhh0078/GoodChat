// 文件作用说明：验证个人偏好必须经过认证与严格校验，且不会改变企业统计时区。
package auth

import (
	"context"
	"testing"
)

// TestPersonalPreferencesKeepTenantTimezone 在隔离身份库验证本人更新、非法输入与企业时区隔离。
func TestPersonalPreferencesKeepTenantTimezone(t *testing.T) {
	f := identityIntegration(t)
	account, session := f.register(t, "i18n-preferences@example.com")
	requireStatus(t, f.call("PATCH", "/api/v1/auth/preferences", map[string]string{"locale": "de", "timezone": "America/New_York"}, session, ""), 200)
	var locale, timezone, tenantZone string
	if err := f.pool.QueryRow(context.Background(), "SELECT preferred_locale,display_timezone FROM users WHERE id=$1", account.User.ID).Scan(&locale, &timezone); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), "SELECT timezone FROM tenants WHERE id=$1", account.Memberships[0].TenantID).Scan(&tenantZone); err != nil {
		t.Fatal(err)
	}
	if locale != "de" || timezone != "America/New_York" || tenantZone != "Asia/Shanghai" {
		t.Fatal("个人设置覆盖了企业时区或未保存")
	}
	requireStatus(t, f.call("PATCH", "/api/v1/auth/preferences", map[string]string{"timezone": "UTC+8"}, session, ""), 400)
	requireStatus(t, f.call("PATCH", "/api/v1/auth/preferences", map[string]string{"locale": "xx"}, session, ""), 400)
	requireStatus(t, f.call("PATCH", "/api/v1/auth/preferences", map[string]string{"locale": "en", "tenant_id": account.Memberships[0].TenantID}, session, ""), 400)
	requireStatus(t, f.call("GET", "/api/v1/auth/preferences", nil, nil, ""), 401)
}
