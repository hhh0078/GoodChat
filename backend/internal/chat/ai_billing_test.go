// 文件作用说明：验证微元计费精度、工具白名单和跨午夜业务时段，不调用真实供应商。
package chat

import (
	"testing"
	"time"
)

// TestAIAmountAndToolBoundaries 防止浮点计费、负用量、溢出及非法话术动作。
func TestAIAmountAndToolBoundaries(t *testing.T) {
	for _, tc := range []struct{ i, o, a, b, want int64 }{{2000, 500, 1000000, 4000000, 4000}, {1, 0, 1, 0, 1}, {0, 0, 0, 0, 0}, {10, 10, 100000, 300000, 4}, {1, 1, 300000, 600000, 1}} {
		v, e := aiAmount(tc.i, tc.o, tc.a, tc.b)
		if e != nil || v != tc.want {
			t.Fatalf("微元计费错误: %d %v", v, e)
		}
	}
	if _, e := aiAmount(-1, 0, 1, 1); e == nil {
		t.Fatal("负用量未被拒绝")
	}
	if _, e := aiAmount(1<<62, 0, 1<<62, 0); e == nil {
		t.Fatal("溢出未被拒绝")
	}
	if e := validateAIPlan(aiPlan{Items: []aiReplyItem{{Type: "script", ScriptID: "other", Version: 1}}}, []aiScript{{ID: "allowed", Version: 1}}); e == nil {
		t.Fatal("未绑定话术被允许")
	}
	if e := aiDecode(`{"items":[],"tenant_id":"fake"}`, &aiPlan{}); e == nil {
		t.Fatal("未知工具字段被接受")
	}
	if e := aiDecode(`{} {}`, &aiPlan{}); e == nil {
		t.Fatal("多JSON值未被拒绝")
	}
	settings := aiBotSettings{Start: "22:00", End: "02:00", Weekdays: []int{1}}
	monday := time.Date(2026, 11, 16, 23, 0, 0, 0, time.UTC)
	if !aiWithinHours(settings, monday) || !aiWithinHours(settings, monday.Add(2*time.Hour)) || aiWithinHours(settings, monday.Add(4*time.Hour)) {
		t.Fatal("跨午夜业务日错误")
	}
	if aiPublicIP([]byte{127, 0, 0, 1}) || aiPublicIP([]byte{169, 254, 169, 254}) || aiPublicIP([]byte{100, 64, 0, 1}) {
		t.Fatal("私网模型地址未被拒绝")
	}
}
