// 文件作用说明：使用本地SMTP测试替身验证受理边界、TLS强制、超时与模板，不代表真实邮箱投递。
package auth

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"supportdesk/backend/internal/platform"
)

// smtpFixture 提供可以控制DATA确认与QUIT收尾的本地测试服务器。
type smtpFixture struct {
	address     string
	messages    chan string
	listener    net.Listener
	connections sync.Map
}

// startSMTP 创建测试SMTP，仅绑定本机随机端口并在测试结束关闭连接。
func startSMTP(t *testing.T, mode string) *smtpFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &smtpFixture{address: listener.Addr().String(), messages: make(chan string, 32), listener: listener}
	t.Cleanup(func() {
		listener.Close()
		fixture.connections.Range(func(key, value any) bool { key.(net.Conn).Close(); return true })
	})
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			fixture.connections.Store(connection, true)
			go fixture.serve(connection, mode)
		}
	}()
	return fixture
}

// serve 实现测试需要的SMTP命令并可模拟最终确认丢失。
func (f *smtpFixture) serve(connection net.Conn, mode string) {
	defer connection.Close()
	defer f.connections.Delete(connection)
	if mode == "stall" {
		time.Sleep(time.Second)
		return
	}
	_, _ = io.WriteString(connection, "220 goodchat.test ESMTP\r\n")
	reader := textproto.NewReader(bufio.NewReader(connection))
	for {
		line, err := reader.ReadLine()
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch command {
		case "EHLO", "HELO":
			_, _ = io.WriteString(connection, "250 goodchat.test\r\n")
		case "MAIL", "RCPT":
			_, _ = io.WriteString(connection, "250 OK\r\n")
		case "DATA":
			_, _ = io.WriteString(connection, "354 Send data\r\n")
			data, err := reader.ReadDotBytes()
			if err != nil {
				return
			}
			f.messages <- string(data)
			if mode == "unknown" {
				return
			}
			if mode == "reject" {
				_, _ = io.WriteString(connection, "550 rejected\r\n")
			} else {
				_, _ = io.WriteString(connection, "250 accepted\r\n")
			}
		case "QUIT":
			if mode == "quit-close" {
				return
			}
			_, _ = io.WriteString(connection, "221 bye\r\n")
			return
		default:
			_, _ = io.WriteString(connection, "500 unsupported\r\n")
		}
	}
}

// config 返回仅用于本地测试的SMTP配置，不携带任何真实账号凭证。
func (f *smtpFixture) config() platform.Config {
	host, port, _ := net.SplitHostPort(f.address)
	return platform.Config{SMTPHost: host, SMTPPort: port, SMTPFrom: "no-reply@goodchat.test", SMTPAllowPlain: true, PublicURL: "http://localhost:13000", AuthSecret: strings.Repeat("test-auth-secret-", 3)}
}

// nextBody 从测试服务器捕获的MIME正文提取纯文本，不把验证码写入测试输出。
func (f *smtpFixture) nextBody(t *testing.T) string {
	t.Helper()
	var raw string
	select {
	case raw = <-f.messages:
	case <-time.After(3 * time.Second):
		t.Fatal("未收到测试SMTP邮件")
	}
	message, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal("测试邮件格式错误")
	}
	_, parameters, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal("测试邮件MIME错误")
	}
	part, err := multipart.NewReader(message.Body, parameters["boundary"]).NextPart()
	if err != nil {
		t.Fatal("缺少纯文本邮件正文")
	}
	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
	if err != nil {
		t.Fatal("邮件正文解码失败")
	}
	return string(body)
}

// TestSMTPBoundaries 验证DATA确认、QUIT断开和未知结果的区别。
func TestSMTPBoundaries(t *testing.T) {
	for _, item := range []struct{ mode, want string }{{"accepted", "accepted"}, {"quit-close", "accepted"}, {"unknown", "unknown"}, {"reject", "failed"}} {
		t.Run(item.mode, func(t *testing.T) {
			server := startSMTP(t, item.mode)
			mailer := smtpMailer{cfg: server.config(), timeout: time.Second}
			result := mailer.send(context.Background(), "recipient@example.com", "GoodChat 测试", "仅测试本地邮件传输")
			if result.Status != item.want {
				t.Fatal("SMTP受理状态错误")
			}
			if !strings.Contains(server.nextBody(t), "本地邮件") {
				t.Fatal("纯文本邮件丢失")
			}
		})
	}
}

// TestSMTPRequiresTLSAndDeadline 验证普通服务器不能使生产发信降级为明文。
func TestSMTPRequiresTLSAndDeadline(t *testing.T) {
	server := startSMTP(t, "accepted")
	cfg := server.config()
	cfg.SMTPAllowPlain = false
	cfg.Production = true
	mailer := smtpMailer{cfg: cfg, timeout: time.Second}
	result := mailer.send(context.Background(), "recipient@example.com", "test", "test")
	if result.Status != "failed" || result.FailureKind != "tls_required" {
		t.Fatal("生产SMTP被降级为明文")
	}
	stalled := startSMTP(t, "stall")
	slow := smtpMailer{cfg: stalled.config(), timeout: 50 * time.Millisecond}
	started := time.Now()
	result = slow.send(context.Background(), "recipient@example.com", "test", "test")
	if result.Status != "failed" || time.Since(started) > 500*time.Millisecond {
		t.Fatal("SMTP总体截止时间未生效")
	}
	missing := smtpMailer{cfg: platform.Config{}}
	if missing.validate() == nil {
		t.Fatal("SMTP未配置没有报错")
	}
}
