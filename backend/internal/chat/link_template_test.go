// 文件作用说明：验证显式解除模板绑定的后端规范化，避免遗留页面或版本编号。
package chat

import (
	"context"
	"testing"
)

// TestClearLinkTemplateRestoresDefault 验证空绑定始终清除旧固定版本并恢复默认页面。
func TestClearLinkTemplateRestoresDefault(t *testing.T) {
	for _, empty := range []any{nil, "", "   "} {
		values := map[string]any{"page_id": empty, "binding_mode": "PIN_RELEASE", "pinned_release_id": "old"}
		if err := prepareLinkTemplateBinding(context.Background(), nil, "", values); err != nil {
			t.Fatal(err)
		}
		if values["page_id"] != nil || values["pinned_release_id"] != nil || values["binding_mode"] != "FOLLOW_PAGE" {
			t.Fatal("清空模板后不能残留旧绑定")
		}
	}
}

// TestInvalidLinkTemplateMode 验证错误更新方式在访问数据库之前被拒绝。
func TestInvalidLinkTemplateMode(t *testing.T) {
	if err := prepareLinkTemplateBinding(context.Background(), nil, "", map[string]any{"page_id": "page", "binding_mode": "unknown"}); err == nil {
		t.Fatal("非法模板更新方式不能被接受")
	}
}
