// 文件作用说明：验证SMTP密码密文落盘、配置恢复、无秘密回显及标准邮件头，不外发邮件。
package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestPrivateSMTPSettings 验证空密码保持原值、修改前备份、权限0600和错误密钥不能解密。
func TestPrivateSMTPSettings(t *testing.T) {
	dir := t.TempDir()
	cfg := platform.Config{DataDir: dir, SMTPHost: "smtp.example.com", SMTPPort: "465", SMTPFrom: "notice@example.com", SMTPUser: "notice@example.com", SMTPPassword: "private-example-password", Production: true}
	s := &Service{cfg: cfg, secret: []byte(strings.Repeat("s", 32)), mailer: &smtpMailer{cfg: cfg}}
	body := `{"host":"smtp.example.com","port":"465","from":"notice@example.com","username":"notice@example.com","password":""}`
	w := httptest.NewRecorder()
	if e := s.mailSettings(w, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(dir, "smtp-config.json"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte(cfg.SMTPPassword)) {
		t.Fatal("密码明文落盘")
	}
	info, e := os.Stat(filepath.Join(dir, "smtp-config.json"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("配置权限错误")
	}
	if _, e := os.Stat(filepath.Join(dir, "smtp-config.previous.json")); e != nil {
		t.Fatal("未保留备份")
	}
	restored, e := applyStoredSMTP(cfg, s.secret)
	if e != nil || restored.SMTPPassword != cfg.SMTPPassword {
		t.Fatal("密码恢复失败", e)
	}
	if _, e := applyStoredSMTP(cfg, []byte(strings.Repeat("x", 32))); e == nil {
		t.Fatal("错误密钥未拒绝")
	}
	encoded, _ := json.Marshal(s.MailStatus())
	if bytes.Contains(encoded, []byte(cfg.SMTPPassword)) || bytes.Contains(encoded, []byte("password_cipher")) {
		t.Fatal("状态泄露秘密")
	}
}

// TestMIMEHeaders 验证Date、Message-ID、品牌发件名与一键退订头符合业务用途。
func TestMIMEHeaders(t *testing.T) {
	text := buildMail("notice@example.com", "owner@example.com", "Test", "Plain", "<p>HTML</p>", "https://goodchat.example/api/v1/public/mail-unsubscribe?token=test")
	for _, header := range []string{"Date: ", "Message-ID: <", "From: GoodChat <notice@example.com>", "List-Unsubscribe: <https://goodchat.example/", "List-Unsubscribe-Post: List-Unsubscribe=One-Click", "multipart/alternative"} {
		if !strings.Contains(text, header) {
			t.Fatal("缺少标准邮件头", header)
		}
	}
	if strings.Contains(buildMail("notice@example.com", "owner@example.com", "Code", "Code", "", ""), "List-Unsubscribe:") {
		t.Fatal("安全邮件不应有退订头")
	}
}
