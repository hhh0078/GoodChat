// 文件作用说明：向后台通知队列提供与身份邮件相同的SMTP传输和真实受理分类，不开放公共任意发信接口。
package auth

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"supportdesk/backend/internal/notifications"
	"unicode/utf8"
)

// SendNotification 仅供受信任的后台队列发送租户主动配置的通知，返回受理、失败或结果不确定及安全分类。
func (s *Service) SendNotification(ctx context.Context, recipient, subject, body string) (string, string) {
	email, err := normalizeEmail(recipient)
	if err != nil || strings.ContainsAny(subject, "\r\n") || strings.TrimSpace(subject) == "" || utf8.RuneCountInString(subject) > 120 || utf8.RuneCountInString(body) > 10000 {
		return "failed", "invalid_notification"
	}
	if s.mailer == nil {
		return "failed", "configuration"
	}
	result := s.mailer.send(ctx, email, subject, body)
	return result.Status, result.FailureKind
}

// SendRenderedNotification 复用SMTP传输，仅接收后端安全渲染邮件，不开放公共发信接口。
func (s *Service) SendRenderedNotification(ctx context.Context, recipient string, mail notifications.Mail) (string, string) {
	email, err := normalizeEmail(recipient)
	if err != nil || strings.ContainsAny(mail.Subject+mail.UnsubscribeURL, "\r\n") || utf8.RuneCountInString(mail.Subject) > 120 || len(mail.Body) > 60000 {
		return "failed", "invalid_notification"
	}
	if s.mailer == nil {
		return "failed", "configuration"
	}
	result := s.mailer.sendRichHeaders(ctx, email, mail.Subject, mail.Body, mail.HTML, mail.UnsubscribeURL)
	return result.Status, result.FailureKind
}

// queueNotice 将账号通知与业务写入同一事务，未装配时保留独立模块行为。
func (s *Service) queueNotice(ctx context.Context, tx pgx.Tx, e notifications.Event) error {
	if s.Notify == nil {
		return nil
	}
	return s.Notify(ctx, tx, e)
}
