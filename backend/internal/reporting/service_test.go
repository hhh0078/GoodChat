// 文件作用说明：验证报表日期按租户时区解析、夏令时边界与无效范围。
package reporting

import (
	"net/http/httptest"
	"testing"
	"time"
	_ "time/tzdata"
)

// TestDefaultDateRangeIsToday 验证未传日期时使用租户日历当天，不沿用最近七天或浏览器时区。
func TestDefaultDateRangeIsToday(t *testing.T) {
	zone := time.FixedZone("租户时区", 8*60*60)
	before := time.Now().In(zone)
	start, end, err := dateRange(httptest.NewRequest("GET", "/statistics", nil), zone)
	after := time.Now().In(zone)
	if err != nil || start.Hour() != 0 || start.Location() != zone || !end.Equal(start.AddDate(0, 0, 1)) {
		t.Fatalf("默认应为租户当天：%v %v %v", start, end, err)
	}
	if start.Format("2006-01-02") != before.Format("2006-01-02") && start.Format("2006-01-02") != after.Format("2006-01-02") {
		t.Fatal("默认日期不在当前租户当天")
	}
}

// TestDateRangeUsesTenantCalendar 验证含夏令时的日历范围没有被固定24小时截断。
func TestDateRangeUsesTenantCalendar(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/statistics?from=2026-03-08&to=2026-03-08", nil)
	start, end, err := dateRange(r, zone)
	if err != nil || end.Sub(start) != 23*time.Hour {
		t.Fatalf("租户日历范围错误: %v %v %v", start, end, err)
	}
	if start.Format(time.RFC3339) != "2026-03-08T00:00:00-05:00" || end.Format(time.RFC3339) != "2026-03-09T00:00:00-04:00" {
		t.Fatal("夏令时边界错误")
	}
	for _, query := range []string{"from=bad", "from=2026-02-02&to=2026-01-01", "from=2020-01-01&to=2026-01-01"} {
		if _, _, err = dateRange(httptest.NewRequest("GET", "/statistics?"+query, nil), zone); err == nil {
			t.Error("应拒绝无效日期范围")
		}
	}
}
