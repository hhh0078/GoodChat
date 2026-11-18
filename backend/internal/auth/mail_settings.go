// 文件作用说明：提供平台SMTP私有配置，密码加密落盘、保留单份恢复副本且API不回显密码。
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	netmail "net/mail"
	"os"
	"path/filepath"

	"supportdesk/backend/internal/platform"
)

// storedSMTP 仅保存受控SMTP字段，密码密文绑定本站认证密钥。
type storedSMTP struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	From     string `json:"from"`
	Username string `json:"username"`
	Cipher   string `json:"password_cipher"`
}

// smtpCipher 使用认证密钥派生独立AES-GCM密钥，不将密钥与密码一起保存。
func smtpCipher(secret []byte) (cipher.AEAD, error) {
	key := sha256.Sum256(append([]byte("goodchat-smtp-v1:"), secret...))
	block, e := aes.NewCipher(key[:])
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}

// sealSMTP 加密密码，随机nonce确保相同密码重复保存产生不同密文。
func sealSMTP(secret []byte, password string) (string, error) {
	a, e := smtpCipher(secret)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.RawStdEncoding.EncodeToString(a.Seal(nonce, nonce, []byte(password), nil)), nil
}

// openSMTP 验证密文完整性，错误不回显配置或原始密文。
func openSMTP(secret []byte, value string) (string, error) {
	raw, e := base64.RawStdEncoding.DecodeString(value)
	if e != nil {
		return "", fmt.Errorf("邮件私有配置无法解密")
	}
	a, e := smtpCipher(secret)
	if e != nil || len(raw) < a.NonceSize() {
		return "", fmt.Errorf("邮件私有配置无法解密")
	}
	plain, e := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], nil)
	if e != nil {
		return "", fmt.Errorf("邮件私有配置无法解密")
	}
	return string(plain), nil
}

// applyStoredSMTP 只在存在本站私有文件时覆盖启动环境配置，原有环境变量继续兼容。
func applyStoredSMTP(cfg platform.Config, secret []byte) (platform.Config, error) {
	raw, e := os.ReadFile(filepath.Join(cfg.DataDir, "smtp-config.json"))
	if os.IsNotExist(e) {
		return cfg, nil
	}
	if e != nil {
		return cfg, fmt.Errorf("邮件私有配置不可读取")
	}
	var stored storedSMTP
	if json.Unmarshal(raw, &stored) != nil {
		return cfg, fmt.Errorf("邮件私有配置格式无效")
	}
	password, e := openSMTP(secret, stored.Cipher)
	if e != nil {
		return cfg, e
	}
	cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom, cfg.SMTPUser, cfg.SMTPPassword = stored.Host, stored.Port, stored.From, stored.Username, password
	return cfg, nil
}

// snapshot 在读锁下复制SMTP配置，发送中途更新不会产生字段混用。
func (m *smtpMailer) snapshot() platform.Config { m.mu.RLock(); defer m.mu.RUnlock(); return m.cfg }

// MailStatus 提供平台状态与非秘密配置，永不返回密码或密文。
func (s *Service) MailStatus() map[string]any {
	cfg := s.mailer.snapshot()
	from := cfg.SMTPFrom
	if parsed, e := netmail.ParseAddress(from); e == nil {
		from = parsed.Address
	}
	return map[string]any{"host": cfg.SMTPHost, "port": cfg.SMTPPort, "from": from, "username": cfg.SMTPUser, "password_set": cfg.SMTPPassword != "", "configured": validateMailConfig(cfg) == nil, "tls_required": !cfg.SMTPAllowPlain}
}

// mailSettings 限平台管理员读取或更新SMTP，空密码保留已有值，写入前保存可恢复的加密副本。
func (s *Service) mailSettings(w http.ResponseWriter, r *http.Request) error {
	if r.Method == http.MethodGet {
		platform.JSON(w, 200, s.MailStatus())
		return nil
	}
	var in struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		From     string `json:"from"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if e := platform.Decode(w, r, &in); e != nil {
		return e
	}
	if len(in.Password) > 2048 {
		return platform.Fail(400, "mail_config_invalid", "邮件服务配置无效")
	}
	s.mailer.mu.Lock()
	defer s.mailer.mu.Unlock()
	cfg := s.mailer.cfg
	cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom, cfg.SMTPUser = in.Host, in.Port, in.From, in.Username
	if in.Password != "" {
		cfg.SMTPPassword = in.Password
	}
	if cfg.SMTPUser == "" {
		cfg.SMTPPassword = ""
	}
	if e := validateMailConfig(cfg); e != nil {
		return e
	}
	encoded, e := sealSMTP(s.secret, cfg.SMTPPassword)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(storedSMTP{cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom, cfg.SMTPUser, encoded})
	if e != nil {
		return e
	}
	path := filepath.Join(s.cfg.DataDir, "smtp-config.json")
	previous, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		old := s.mailer.cfg
		cipher, e := sealSMTP(s.secret, old.SMTPPassword)
		if e != nil {
			return e
		}
		previous, _ = json.Marshal(storedSMTP{old.SMTPHost, old.SMTPPort, old.SMTPFrom, old.SMTPUser, cipher})
	} else if e != nil {
		return platform.Fail(503, "mail_config_write", "邮件私有配置无法保存")
	}
	if e := os.MkdirAll(s.cfg.DataDir, 0700); e != nil {
		return e
	}
	if e := os.WriteFile(filepath.Join(s.cfg.DataDir, "smtp-config.previous.json"), previous, 0600); e != nil {
		return e
	}
	temp, e := os.CreateTemp(s.cfg.DataDir, ".smtp-config-*")
	if e != nil {
		return e
	}
	name := temp.Name()
	defer os.Remove(name)
	if e := temp.Chmod(0600); e != nil {
		temp.Close()
		return e
	}
	if _, e := temp.Write(raw); e != nil {
		temp.Close()
		return e
	}
	if e := temp.Sync(); e != nil {
		temp.Close()
		return e
	}
	if e := temp.Close(); e != nil {
		return e
	}
	if e := os.Rename(name, path); e != nil {
		return e
	}
	s.mailer.cfg = cfg
	platform.JSON(w, 200, map[string]bool{"saved": true})
	return nil
}
