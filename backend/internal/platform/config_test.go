// 文件作用说明：验证AI每日提交限制配置，防止非法或零值变成无上限。
package platform

import "testing"

// TestAIDailyLimitParsing 验证允许的非负整数和明确拒绝的非法配置。
func TestAIDailyLimitParsing(t *testing.T) {
	for _, value := range []string{"0", "1", "20", "100000"} {
		if _, err := aiDailyLimit(value); err != nil {
			t.Errorf("合法上限%s被拒绝", value)
		}
	}
	for _, value := range []string{"-1", "unlimited", "1.5", "100001", ""} {
		if _, err := aiDailyLimit(value); err == nil {
			t.Errorf("非法上限%s被接受", value)
		}
	}
	t.Setenv("SUPPORT_AI_DAILY_LIMIT", "")
	limit, err := aiDailyLimit(env("SUPPORT_AI_DAILY_LIMIT", "20"))
	if err != nil || limit != 20 {
		t.Fatal("未配置时每日提交次数应默认20")
	}
}
