// 文件作用说明：验证邮箱边界、Argon2id密码、随机凭证及请求来源保护，防止身份输入被错误接受。
package auth

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestEmailAndPassword 验证规范化、强度、随机盐和错误密码拒绝。
func TestEmailAndPassword(t *testing.T) {
	email, err := normalizeEmail("  USER@Example.com  ")
	if err != nil || email != "user@example.com" {
		t.Fatal("邮箱规范化失败")
	}
	for _, input := range []string{"Name <user@example.com>", "a@localhost", "a@example.com\r\nBcc:evil@example.com", "not-email"} {
		if _, err := normalizeEmail(input); err == nil {
			t.Fatal("非法邮箱被接受")
		}
	}
	for _, password := range []string{"short", "        ", strings.Repeat("中", 129)} {
		if validatePassword(password) == nil {
			t.Fatal("非法密码被接受")
		}
	}
	first, err := hashPassword("安全 password 123")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword("安全 password 123")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !verifyPassword(first, "安全 password 123") || verifyPassword(first, "wrong password") {
		t.Fatal("密码随机盐或校验失败")
	}
	for _, invalid := range []string{"plain-text", strings.Replace(first, "m=65536", "m=4294967295", 1), strings.Replace(first, "t=3", "t=0", 1), strings.Replace(first, "v=19", "v=10", 1)} {
		if verifyPassword(invalid, "安全 password 123") {
			t.Fatal("非法密码摘要被接受")
		}
	}
}

// TestRandomIdentifiers 验证令牌格式及验证码HMAC的用途和世代隔离。
func TestRandomIdentifiers(t *testing.T) {
	id, err := newID()
	if err != nil || !validID(id) {
		t.Fatal("UUID生成失败")
	}
	for _, bad := range []string{"", id + "x", "zzzzzzzz-0000-0000-0000-000000000000"} {
		if validID(bad) {
			t.Fatal("非法UUID被接受")
		}
	}
	for index := 0; index < 20; index++ {
		code, err := newCode()
		if err != nil || len(code) != 6 {
			t.Fatal("六位验证码生成失败")
		}
		for _, digit := range code {
			if digit < '0' || digit > '9' {
				t.Fatal("验证码含非数字")
			}
		}
	}
	secret := []byte(strings.Repeat("k", 32))
	base := keyedDigest(secret, "otp", "generation-a", "a@example.com", "register", "123456")
	for _, different := range [][]byte{keyedDigest(secret, "otp", "generation-a", "a@example.com", "login", "123456"), keyedDigest(secret, "otp", "generation-b", "a@example.com", "register", "123456"), keyedDigest([]byte(strings.Repeat("x", 32)), "otp", "generation-a", "a@example.com", "register", "123456")} {
		if string(base) == string(different) {
			t.Fatal("验证码摘要未隔离用途、世代或服务端密钥")
		}
	}
}

// TestOriginAndSecret 验证来源检查及开发密钥跨重启持久化。
func TestOriginAndSecret(t *testing.T) {
	cfg := platform.Config{PublicURL: "https://goodchat.example", DataDir: t.TempDir()}
	service := &Service{cfg: cfg}
	for _, origin := range []string{"https://evil.example", "null", "https://goodchat.example/path", "https://goodchat.example@evil.example", "http://goodchat.example"} {
		request := httptest.NewRequest("POST", "/", nil)
		request.Header.Set("Origin", origin)
		if service.checkOrigin(request) == nil {
			t.Fatal("跨站来源被允许")
		}
	}
	request := httptest.NewRequest("POST", "/", nil)
	request.Header.Set("Origin", cfg.PublicURL)
	if err := service.checkOrigin(request); err != nil {
		t.Fatal(err)
	}
	first, err := loadAuthSecret(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadAuthSecret(cfg)
	if err != nil || string(first) != string(second) {
		t.Fatal("认证密钥未正确持久化")
	}
	if _, err := loadAuthSecret(platform.Config{Production: true, AuthSecret: "short"}); err == nil {
		t.Fatal("生产短密钥被接受")
	}
}

// TestDeliveryClassification 验证最终确认丢失与明确拒收不会混淆。
func TestDeliveryClassification(t *testing.T) {
	cases := []struct {
		err          error
		final        bool
		status, kind string
	}{{io.EOF, true, "unknown", "confirmation_lost"}, {&textproto.Error{Code: 451, Msg: "try later"}, true, "failed", "temporary"}, {&textproto.Error{Code: 550, Msg: "rejected"}, true, "failed", "permanent"}, {context.DeadlineExceeded, false, "failed", "temporary"}, {errors.New("failure"), false, "failed", "transport"}}
	for _, item := range cases {
		got := classifyDelivery(item.err, item.final)
		if got.Status != item.status || got.FailureKind != item.kind {
			t.Fatal("邮件错误分类不正确")
		}
	}
}
