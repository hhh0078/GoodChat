// 文件作用说明：验证公开上下文、同步游标和附件类型的真实信任边界。
package chat

import (
	"strings"
	"testing"
)

// TestContextLimits 验证中文特殊字符、零值和过深过大载荷的行为。
func TestContextLimits(t *testing.T) {
	valid := map[string]any{"姓名": "张三&李四+%#", "数量": float64(0), "已支付": false, "空值": nil}
	if err := validateContext(valid); err != nil {
		t.Fatalf("合法上下文被拒绝：%v", err)
	}
	if err := validateContext(map[string]any{"长文本": strings.Repeat("字", 2001)}); err == nil {
		t.Fatal("超长文本应拒绝")
	}
	var nested any = "正文"
	for i := 0; i < 7; i++ {
		nested = map[string]any{"子": nested}
	}
	if err := validateContext(nested.(map[string]any)); err == nil {
		t.Fatal("过深对象应拒绝")
	}
	many := map[string]any{}
	for i := 0; i < 101; i++ {
		many[strings.Repeat("a", i+1)] = true
	}
	if err := validateContext(many); err == nil {
		t.Fatal("超多字段应拒绝")
	}
}

// TestCursorScopeAndTampering 验证游标不能跨访客、租户或会话复用。
func TestCursorScopeAndTampering(t *testing.T) {
	s := &Service{cursorKey: []byte("01234567890123456789012345678901")}
	a := actor{TenantID: "tenant1", ContactID: "contact1"}
	token := s.encodeCursor(a, "conversation1", 73)
	seq, err := s.decodeCursor(a, "conversation1", token)
	if err != nil || seq != 73 {
		t.Fatalf("合法游标恢复失败：%d %v", seq, err)
	}
	for _, test := range []struct {
		a                   actor
		conversation, token string
	}{{actor{TenantID: "tenant2", ContactID: "contact1"}, "conversation1", token}, {actor{TenantID: "tenant1", ContactID: "contact2"}, "conversation1", token}, {a, "conversation2", token}, {a, "conversation1", token + "A"}} {
		if _, err := s.decodeCursor(test.a, test.conversation, test.token); err == nil {
			t.Fatal("越权或篡改游标必须拒绝")
		}
	}
}

// TestTrackingRemovesCredentials 验证来源记录不会保存URL中的手机号和认证秘密。
func TestTrackingRemovesCredentials(t *testing.T) {
	result := cleanTracking(map[string]any{"landing_url": "https://shop.example/item?phone=123&token=secret&utm_source=广告#private", "referrer": "javascript:alert(1)", "phone": "123", "campaign_id": "A"})
	url, _ := result["landing_url"].(string)
	if strings.Contains(url, "secret") || strings.Contains(url, "phone") || strings.Contains(url, "private") || !strings.Contains(url, "utm_source") {
		t.Fatalf("URL清理不正确：%s", url)
	}
	if _, ok := result["phone"]; ok {
		t.Fatal("来源不接受任意资料字段")
	}
	if _, ok := result["referrer"]; ok {
		t.Fatal("脚本URL应忽略")
	}
}

// TestAttachmentTrustBoundary 验证可执行内容不能通过通用二进制伪装进入允许列表。
func TestAttachmentTrustBoundary(t *testing.T) {
	for _, kind := range []string{"text/html; charset=utf-8", "image/svg+xml", "application/octet-stream", "application/x-msdownload"} {
		if allowedMedia(kind) {
			t.Fatalf("危险媒体不应允许：%s", kind)
		}
	}
	for _, kind := range []string{"image/png", "audio/webm", "application/pdf", "application/zip"} {
		if !allowedMedia(kind) {
			t.Fatalf("安全支持类型被拒绝：%s", kind)
		}
	}
}

// TestUUIDGeneration 验证持久业务编号格式和随机性。
func TestUUIDGeneration(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newID()
		if !uuidPattern.MatchString(id) || seen[id] {
			t.Fatal("业务编号无效或重复")
		}
		seen[id] = true
	}
}
