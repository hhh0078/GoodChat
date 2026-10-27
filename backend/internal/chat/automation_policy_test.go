// 文件作用说明：验证营业跨日与时区边界、评价期限和欢迎策略摘要，不使用外部通知替身作为真实验收。
package chat

import (
	"testing"
	"time"
)

// TestAutomationBusinessHoursBoundaries 验证本地起始包含、结束排除、跨午夜和周末回绕。
func TestAutomationBusinessHoursBoundaries(t *testing.T) {
	config := defaultAutomationConfig()
	config.BusinessHoursEnabled = true
	config.Timezone = "Asia/Shanghai"
	config.WeeklyHours = []businessWindow{{Day: 6, Start: "22:00", End: "02:00"}, {Day: 1, Start: "09:00", End: "18:00"}}
	for _, item := range []struct {
		value string
		open  bool
	}{{"2026-10-31T21:59:00+08:00", false}, {"2026-10-31T22:00:00+08:00", true}, {"2026-11-01T01:59:59+08:00", true}, {"2026-11-01T02:00:00+08:00", false}, {"2026-11-02T01:00:00Z", true}, {"2026-11-02T10:00:00Z", false}} {
		point, err := time.Parse(time.RFC3339, item.value)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := businessAt(config, point)
		if err != nil || actual != item.open {
			t.Fatalf("营业边界%s=%v，期望%v，错误%v", item.value, actual, item.open, err)
		}
	}
	config.BusinessHoursEnabled = false
	if open, err := businessAt(config, time.Now()); err != nil || !open {
		t.Fatal("关闭营业场景限制时应按全天营业处理")
	}
}

// TestAutomationConfigValidationAndPolicyHash 验证错误时间和标签被拒，评价策略变更不撤销欢迎任务。
func TestAutomationConfigValidationAndPolicyHash(t *testing.T) {
	config := defaultAutomationConfig()
	config.Version = 1
	config.Timezone = "Asia/Shanghai"
	if err := validateAutomationConfig(config); err != nil {
		t.Fatal(err)
	}
	before := welcomePolicyHash(config)
	config.EvaluationAllowEdit = true
	if before != welcomePolicyHash(config) {
		t.Fatal("单独修改评价策略不能取消欢迎任务")
	}
	config.StopWelcomeOnAgent = false
	if before == welcomePolicyHash(config) {
		t.Fatal("欢迎接入策略必须改变欢迎任务有效摘要")
	}
	config.WeeklyHours = []businessWindow{{Day: 1, Start: "24:00", End: "18:00"}}
	if validateAutomationConfig(config) == nil {
		t.Fatal("越界时分不能保存")
	}
	config.WeeklyHours = []businessWindow{}
	config.EvaluationTags = []string{"重复", "重复"}
	if validateAutomationConfig(config) == nil {
		t.Fatal("重复评价标签不能保存")
	}
}

// TestEvaluationEditDeadlineKeepsSubmissionBoundary 验证晚提交的评价改评也不能越过初评截止日。
func TestEvaluationEditDeadlineKeepsSubmissionBoundary(t *testing.T) {
	config := defaultAutomationConfig()
	closed := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	created := closed.Add(167 * time.Hour)
	if expected := closed.Add(168 * time.Hour); !evaluationDeadline(config, closed, created).Equal(expected) {
		t.Fatal("改评期限不能超过咨询结束后的初评窗口")
	}
	created = closed.Add(time.Hour)
	if !evaluationDeadline(config, closed, created).Equal(closed.Add(25 * time.Hour)) {
		t.Fatal("通常改评期限应从首次评价时刻计算")
	}
}
