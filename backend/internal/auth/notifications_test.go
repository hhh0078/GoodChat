// 文件作用说明：用本地SMTP验证通知复用受理分类并拒绝地址和标题注入，不向真实收件人发送邮件。
package auth

import (
	"context"
	"net"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestNotificationDeliveryBoundaries 验证DATA确认、拒绝和确认丢失保持真实分类，输入注入不进入传输层。
func TestNotificationDeliveryBoundaries(t *testing.T) {
	for mode, want := range map[string]string{"accept": "accepted", "reject": "failed", "unknown": "unknown"} {
		t.Run(mode, func(t *testing.T) {
			smtp := startSMTP(t, mode)
			host, port, _ := net.SplitHostPort(smtp.address)
			service := &Service{mailer: &smtpMailer{cfg: platform.Config{SMTPHost: host, SMTPPort: port, SMTPFrom: "notice@goodchat.test", SMTPAllowPlain: true}}}
			status, _ := service.SendNotification(context.Background(), "admin@goodchat.test", "新的待处理留言", "请登录工作台查看，邮件不附客户隐私资料。")
			if status != want {
				t.Fatalf("通知传输分类:%s want%s", status, want)
			}
		})
	}
	service := &Service{}
	for _, input := range [][2]string{{"Name <admin@goodchat.test>", "通知"}, {"admin@goodchat.test", "通知\r\nBcc: other@goodchat.test"}} {
		status, kind := service.SendNotification(context.Background(), input[0], input[1], "正文")
		if status != "failed" || kind != "invalid_notification" {
			t.Fatal("输入注入未拒绝")
		}
	}
}
