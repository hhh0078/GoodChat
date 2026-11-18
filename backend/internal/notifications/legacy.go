// 文件作用说明：为既有离线留言队列复用统一模板与SMTP，不迁移或重复发送原有任务。
package notifications

import (
	"context"
	"regexp"
	"supportdesk/backend/internal/localization"
)

var leadID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// SendLegacyLead 从原队列安全元数据读取留言编号，沿用原有受理、失败与不确定分类。
func (s *Service) SendLegacyLead(ctx context.Context, recipient, subject, body string) (string, string) {
	locale, zone := "en", "UTC"
	for _, candidate := range localization.Locales {
		if subject == localization.Text(candidate, "GoodChat有新的待处理留言") {
			locale = candidate
			break
		}
	}
	var userLocale, userZone string
	if e := s.pool.QueryRow(ctx, "SELECT preferred_locale,display_timezone FROM users WHERE email=$1", recipient).Scan(&userLocale, &userZone); e == nil {
		if localization.Normalize(userLocale) != "" {
			locale = userLocale
		}
		if localization.ValidTimezone(userZone) {
			zone = userZone
		}
	}
	mail, e := s.RenderMail(ctx, "lead", locale, zone, map[string]string{"reference": leadID.FindString(body)})
	if e != nil {
		return "failed", "template"
	}
	if s.send == nil {
		return "failed", "configuration"
	}
	return s.send(ctx, recipient, mail)
}
