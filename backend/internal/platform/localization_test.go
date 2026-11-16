// 文件作用说明：验证多语言错误保留业务码、请求编号与动态参数，并继续隐藏内部错误。
package platform

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"supportdesk/backend/internal/localization"
	"testing"
)

// TestLocalizedErrorContract 验证九语安全错误均保留稳定的机器判断字段。
func TestLocalizedErrorContract(t *testing.T) {
	for _, locale := range localization.Locales {
		r := httptest.NewRequest("GET", "/?lang="+locale, nil)
		r.Header.Set("X-Request-ID", "i18n-test")
		w := httptest.NewRecorder()
		source := "导出最多%d条数据，请缩小筛选范围；未生成截断文件"
		ErrorJSON(w, r, Failf(409, "export_limit_exceeded", source, 123))
		var payload map[string]any
		if json.Unmarshal(w.Body.Bytes(), &payload) != nil {
			t.Fatal("无效错误JSON")
		}
		if payload["code"] != "export_limit_exceeded" || payload["request_id"] != "i18n-test" || payload["message_key"] != localization.Key(source) || payload["locale"] != locale {
			t.Fatal("错误协议不稳定")
		}
		if !strings.Contains(payload["message"].(string), "123") || strings.Contains(payload["message"].(string), "%!") {
			t.Fatal("动态参数损坏")
		}
	}
	r := httptest.NewRequest("GET", "/?lang=en", nil)
	w := httptest.NewRecorder()
	ErrorJSON(w, r, errors.New("private_database_password"))
	if strings.Contains(w.Body.String(), "private_database_password") {
		t.Fatal("内部信息泄露")
	}
}
