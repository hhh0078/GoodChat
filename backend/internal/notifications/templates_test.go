// 文件作用说明：验证九语言模板转义、受控链接和跨午夜及夏令时调度。
package notifications

import (
	"strings"
	"supportdesk/backend/internal/localization"
	"testing"
	"time"
)

// TestQuietHours 验证跨午夜、非跨午夜与夏令时切换后的本地静默结束时刻。
func TestQuietHours(t *testing.T) {
	for _, c := range []struct {
		at, zone, want string
		start, end     int
	}{{"2026-11-18T14:30:00Z", "Asia/Shanghai", "2026-11-19T00:00:00Z", 22, 8}, {"2026-03-08T06:30:00Z", "America/New_York", "2026-03-08T12:00:00Z", 22, 8}, {"2026-11-01T05:30:00Z", "America/New_York", "2026-11-01T13:00:00Z", 22, 8}, {"2026-11-18T09:30:00Z", "UTC", "2026-11-18T17:00:00Z", 9, 17}} {
		at, _ := time.Parse(time.RFC3339, c.at)
		if got := NextAllowed(at, c.zone, c.start, c.end).UTC().Format(time.RFC3339); got != c.want {
			t.Fatal(c.zone, got, c.want)
		}
	}
}

// TestNineLanguageTemplates 验证全场景九语言有译文、HTML转义、按钮携带明确语言与时区。
func TestNineLanguageTemplates(t *testing.T) {
	for _, locale := range localization.Locales {
		for _, d := range Definitions {
			m, e := Render("https://goodchat.example", locale, "Asia/Shanghai", d, map[string]string{"code": "123456", "token": "preview", "tenant": "<img src=x onerror=alert(1)>"}, localization.Text(locale, d.Subject), localization.Text(locale, d.Body), localization.Text(locale, d.Button), -1)
			if e != nil {
				t.Fatal(d.Kind, e)
			}
			if strings.Contains(m.HTML, "<img src=x") {
				t.Fatal("业务变量未转义")
			}
			if d.Path != "" && (!strings.Contains(m.HTML, "timezone=Asia%2FShanghai") || !strings.Contains(m.HTML, "lang="+locale)) {
				t.Fatal("按钮偏好缺失")
			}
			if strings.HasPrefix(d.Kind, "code_") && !strings.Contains(m.Body, "123456") {
				t.Fatal("验证码丢失")
			}
			if locale != "zh-CN" && localization.Text(locale, d.Subject) == d.Subject {
				t.Fatal("缺少译文", locale, d.Kind)
			}
		}
	}
}

// TestInvalidTemplates 验证邮件头注入、未知变量及危险站点地址被拒绝。
func TestInvalidTemplates(t *testing.T) {
	for _, v := range [][3]string{{"x\r\nBcc: x", "body", ""}, {"title", "{token}", ""}, {"title", "{bad", ""}, {"", "body", ""}} {
		if Validate(v[0], v[1], v[2]) == nil {
			t.Fatal("非法模板未拒绝")
		}
	}
	d, _ := Find("test")
	if _, e := Render("javascript:alert(1)", "en", "UTC", d, nil, "title", "body", "button", 0); e == nil {
		t.Fatal("危险站点未拒绝")
	}
}
