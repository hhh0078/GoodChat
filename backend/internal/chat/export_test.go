// 文件作用说明：验证CSV公式单元格、导出布尔选项和时区时间格式的安全边界。
package chat

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestCSVFormulaCells 验证公式前缀、空白绕过和控制字符均转为文本，正常中文和多行文本保持完整。
func TestCSVFormulaCells(t *testing.T) {
	for _, value := range []string{"=1+1", "+8613800000000", "-2+3", "@SUM(A1)", "  =1", "\u3000@SUM(A1)", "\t内容", "\r=1", "\n+1", "\x00=1", "\x01=1"} {
		if safe := csvSafeCell(value); safe != "'"+value {
			t.Fatalf("危险单元格未中和: %q -> %q", value, safe)
		}
	}
	for _, value := range []string{"正常中文", "正文\n下一行", "2026-10-26T00:30:00+08:00", ""} {
		if csvSafeCell(value) != value {
			t.Fatalf("正常文本被更改: %q", value)
		}
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 10, 25, 16, 30, 0, 0, time.UTC)
	if value := csvValue(stamp, location); value != "2026-10-26T00:30:00+08:00" {
		t.Fatal(value)
	}
}

// TestExportOptionsAreExplicit 验证内部数据选项只接受true或false，拒绝容易误读的空格和数字。
func TestExportOptionsAreExplicit(t *testing.T) {
	for _, query := range []string{"include_internal=1", "include_internal=yes", "include_internal=True", "include_internal=%20true"} {
		r := httptest.NewRequest("GET", "/export?"+query, nil)
		if _, err := exportFlag(r, "include_internal"); err == nil {
			t.Fatalf("无效选项被允许: %s", query)
		}
	}
}
