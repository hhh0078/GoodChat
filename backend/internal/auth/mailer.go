// 文件作用说明：提供GoodChat认证邮件模板、受TLS保护的SMTP发送及真实受理结果分类。
package auth

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"supportdesk/backend/internal/platform"
)

// deliveryResult 区分受理、确定失败和DATA结果不确定，避免不安全的自动重发。
type deliveryResult struct {
	Status      string
	FailureKind string
}

// smtpMailer 封装本项目SMTP配置，测试替身不代表真实邮箱收件。
type smtpMailer struct {
	mu      sync.RWMutex
	cfg     platform.Config
	timeout time.Duration
}

// validate 检查邮件配置，禁止生产明文连接或凭证不完整的假发信。
func (m *smtpMailer) validate() error {
	return validateMailConfig(m.snapshot())
}

// validateMailConfig 在发送和平台配置写入边界验证TLS及发件地址。
func validateMailConfig(cfg platform.Config) error {
	if cfg.SMTPHost == "" || cfg.SMTPPort == "" || cfg.SMTPFrom == "" {
		return platform.Fail(503, "mail_unavailable", "邮件服务尚未配置，请联系平台管理员")
	}
	port, err := strconv.Atoi(cfg.SMTPPort)
	if err != nil || port < 1 || port > 65535 || strings.ContainsAny(cfg.SMTPHost, "\r\n/ ") {
		return platform.Fail(503, "mail_unavailable", "邮件服务配置无效")
	}
	from, err := mail.ParseAddress(cfg.SMTPFrom)
	if err != nil || from.Address == "" || strings.ContainsAny(cfg.SMTPFrom, "\r\n") || (cfg.SMTPUser == "") != (cfg.SMTPPassword == "") {
		return platform.Fail(503, "mail_unavailable", "邮件服务配置不完整")
	}
	if cfg.Production && cfg.SMTPAllowPlain {
		return platform.Fail(503, "mail_tls_required", "生产邮件服务必须使用TLS")
	}
	return nil
}

// send 在同一截止时间内完成建连、TLS和SMTP；只有DATA确认才算已受理。
func (m *smtpMailer) send(ctx context.Context, recipient, subject, body string) deliveryResult {
	return m.sendRich(ctx, recipient, subject, body, "")
}

// sendRich 共用原有TLS和SMTP确认边界，仅接收后端渲染的HTML。
func (m *smtpMailer) sendRich(ctx context.Context, recipient, subject, body, rich string) deliveryResult {
	return m.sendRichHeaders(ctx, recipient, subject, body, rich, "")
}

// sendRichHeaders 为订阅邮件加入一键退订头，其余邮件保持现有SMTP受理边界。
func (m *smtpMailer) sendRichHeaders(ctx context.Context, recipient, subject, body, rich, unsubscribe string) deliveryResult {
	cfg := m.snapshot()
	if err := validateMailConfig(cfg); err != nil {
		return deliveryResult{"failed", "configuration"}
	}
	timeout := m.timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.SMTPHost, cfg.SMTPPort))
	if err != nil {
		return classifyDelivery(err, false)
	}
	defer conn.Close()
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.SMTPHost}
	implicitTLS := cfg.SMTPPort == "465"
	if implicitTLS {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return classifyDelivery(err, false)
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return classifyDelivery(err, false)
	}
	defer client.Close()
	if !implicitTLS {
		if offered, _ := client.Extension("STARTTLS"); offered {
			if err := client.StartTLS(tlsConfig); err != nil {
				return classifyDelivery(err, false)
			}
		} else if cfg.Production || !cfg.SMTPAllowPlain {
			return deliveryResult{"failed", "tls_required"}
		}
	}
	if cfg.SMTPUser != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPHost)); err != nil {
			return deliveryResult{"failed", "configuration"}
		}
	}
	from, _ := mail.ParseAddress(cfg.SMTPFrom)
	if err := client.Mail(from.Address); err != nil {
		return classifyDelivery(err, false)
	}
	if err := client.Rcpt(recipient); err != nil {
		result := classifyDelivery(err, false)
		if result.FailureKind == "permanent" {
			result.FailureKind = "recipient_permanent"
		}
		return result
	}
	writer, err := client.Data()
	if err != nil {
		return classifyDelivery(err, false)
	}
	if _, err := io.WriteString(writer, buildMail(from.Address, recipient, subject, body, rich, unsubscribe)); err != nil {
		return classifyDelivery(err, false)
	}
	if err := writer.Close(); err != nil {
		return classifyDelivery(err, true)
	}
	// DATA已明确返回250，QUIT失败不能改写受理结果或触发重发。
	_ = client.Quit()
	return deliveryResult{"accepted", ""}
}

// classifyDelivery 将明确SMTP拒绝、暂时失败和最终确认丢失分开记录。
func classifyDelivery(err error, awaitingFinal bool) deliveryResult {
	var protocol *textproto.Error
	if errors.As(err, &protocol) {
		if protocol.Code >= 400 && protocol.Code < 500 {
			return deliveryResult{"failed", "temporary"}
		}
		if protocol.Code >= 500 && protocol.Code < 600 {
			return deliveryResult{"failed", "permanent"}
		}
	}
	if awaitingFinal {
		return deliveryResult{"unknown", "confirmation_lost"}
	}
	var network net.Error
	if errors.As(err, &network) || errors.Is(err, io.EOF) || errors.Is(err, context.DeadlineExceeded) {
		return deliveryResult{"failed", "temporary"}
	}
	return deliveryResult{"failed", "transport"}
}

// buildMail 组装纯文本与HTML两种正文，品牌固定为本项目而不复制参考凭证。
func buildMail(from, to, subject, body, rich, unsubscribe string) string {
	id, _ := platform.Token()
	if id == "" {
		id = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	domain := strings.Split(from, "@")
	host := "goodchat.local"
	if len(domain) == 2 {
		host = domain[1]
	}
	extra := fmt.Sprintf("Date: %s\r\nMessage-ID: <%s@%s>\r\n", time.Now().UTC().Format(time.RFC1123Z), id, host)
	if unsubscribe != "" {
		extra += "List-Unsubscribe: <" + unsubscribe + ">\r\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n"
	}

	boundary := "goodchat-auth-multipart"
	plain := mailBase64(body)
	if rich == "" {
		rich = "<!doctype html><html><body style=\"font-family:system-ui;color:#182338;background:#f4f7fb;padding:24px\"><h1>GoodChat</h1><div style=\"white-space:pre-wrap\">" + html.EscapeString(body) + "</div></body></html>"
	}
	return extra + fmt.Sprintf("From: GoodChat <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=%q\r\n\r\n--%s\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--%s\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--%s--\r\n", from, to, mime.BEncoding.Encode("UTF-8", subject), boundary, boundary, plain, boundary, mailBase64(rich), boundary)
}

// mailBase64 将MIME编码按76字符换行，避免长模板超过SMTP单行长度限制。
func mailBase64(value string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	var out strings.Builder
	for len(encoded) > 76 {
		out.WriteString(encoded[:76])
		out.WriteString("\r\n")
		encoded = encoded[76:]
	}
	out.WriteString(encoded)
	return out.String()
}
