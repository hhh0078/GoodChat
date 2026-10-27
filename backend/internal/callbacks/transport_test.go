// 文件作用说明：验证进入回调公网限制、禁止跳转、签名和密钥保护，不向外部地址发送测试请求。
package callbacks

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestPublicIPBlocksInternal 验证私网和保留地址不能作为正式回调目标。
func TestPublicIPBlocksInternal(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fc00::1", "::ffff:127.0.0.1"} {
		if publicIP(net.ParseIP(address)) {
			t.Errorf("应拒绝地址%s", address)
		}
	}
	if !publicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("普通公网地址被误拒绝")
	}
}

// TestCallbackDoesNotFollowRedirect 验证开发测试请求也不会跟随回调服务器的跳转。
func TestCallbackDoesNotFollowRedirect(t *testing.T) {
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls++ }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer redirect.Close()
	s := New(nil, nil, platform.Config{CallbackAllowLocal: true})
	response, err := s.client().Get(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 302 || destinationCalls != 0 {
		t.Fatal("回调不应跟随跳转")
	}
}

// TestCallbackURLProductionRejectsLocal 验证生产模式不能通过开发开关访问本机。
func TestCallbackURLProductionRejectsLocal(t *testing.T) {
	s := New(nil, nil, platform.Config{Production: true, CallbackAllowLocal: true})
	if s.validateURL(context.Background(), "https://127.0.0.1/test") == nil {
		t.Fatal("生产环境不应允许本机")
	}
	if s.validateURL(context.Background(), "http://example.com") == nil {
		t.Fatal("生产回调不应使用HTTP")
	}
}

// TestSignatureAndEncryption 验证回调正文变更会改变签名，错误密钥不能解密。
func TestSignatureAndEncryption(t *testing.T) {
	if signature("secret", "123", []byte("one")) == signature("secret", "123", []byte("two")) {
		t.Fatal("签名未覆盖正文")
	}
	s := New(nil, nil, platform.Config{AuthSecret: "test-key-not-for-production"})
	encrypted, err := s.encrypt("callback-secret")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.decrypt(encrypted)
	if err != nil || plain != "callback-secret" {
		t.Fatal("密钥加解密失败")
	}
	other := New(nil, nil, platform.Config{AuthSecret: "different-key"})
	if _, err = other.decrypt(encrypted); err == nil {
		t.Fatal("不同服务密钥不应解密成功")
	}
}
