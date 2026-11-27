// 文件作用说明：验证九语言模板转义、受控链接和跨午夜及夏令时调度。
package notifications

import (
	"net/url"
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

// TestKnowledgeGapEmailDetails 验证旧自定义模板仍展示真实问题与原因，客户HTML不能执行，链接直达待补充页。
func TestKnowledgeGapEmailDetails(t *testing.T) {
	d, _ := Find("knowledge_gap")
	mail, err := Render("https://goodchat.example", "zh-CN", "Asia/Shanghai", d, map[string]string{"tenant": "测试团队", "reference": "task-1", "question": "作者的微信号？<script>alert(1)</script>", "reason": "没有授权资料"}, "有问题待补充", "旧模板正文", "查看", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mail.Body, "作者的微信号？") || !strings.Contains(mail.Body, "没有授权资料") || !strings.Contains(mail.Body, "tab=pending") || strings.Contains(mail.HTML, "<script>") {
		t.Fatal("问题详情、转义或站内入口不正确")
	}
}

// TestHandoffConversationLink 验证真实会话深链、旧模板兼容和不可信编号降级，不允许邮件指定租户或外部地址。
func TestHandoffConversationLink(t *testing.T) {
	d, _ := Find("handoff_waiting")
	const conversation = "11a1e08d-91f6-40ac-b173-4feddfdd39b9"
	for _, reference := range []string{conversation, strings.ToUpper(conversation), "", "task-1", conversation + "&tenant=other", "//evil.example"} {
		mail, err := Render("https://goodchat.example", "zh-CN", "Asia/Shanghai", d, map[string]string{"tenant": "GoodHR", "reference": reference, "question": "会封号吗", "reason": "需要人工处理"}, "人工接待提醒", "旧模板正文", "打开工作台", 2)
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(mail.Body, "https://goodchat.example/workspace?")
		if start < 0 {
			t.Fatal("缺少固定站内入口")
		}
		link, err := url.Parse(strings.Split(mail.Body[start:], "\n")[0])
		if err != nil || link.Host != "goodchat.example" || link.Path != "/workspace" {
			t.Fatal("邮件目标异常", err)
		}
		q := link.Query()
		want := ""
		if reference == conversation || reference == strings.ToUpper(conversation) {
			want = conversation
		}
		if q.Get("conversation") != want || q.Get("tenant") != "" || q.Get("tenant_id") != "" || q.Get("lang") != "zh-CN" || q.Get("timezone") != "Asia/Shanghai" {
			t.Fatal("会话定位或偏好参数错误", link)
		}
		if !strings.Contains(mail.Body, "会封号吗") || !strings.Contains(mail.Body, "需要人工处理") {
			t.Fatal("旧模板的问题详情丢失")
		}
	}
}
