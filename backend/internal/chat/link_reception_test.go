// 文件作用说明：验证链接接待方式对既有池算法的最小兼容规则。
package chat

import "testing"

// TestLinkAlgorithm 验证继承保持原算法、手领覆盖自动池、自动入口可使用原人工池。
func TestLinkAlgorithm(t *testing.T) {
	for _, item := range []struct{ pool, mode, want string }{
		{"manual", "inherit", "manual"},
		{"weighted_round_robin", "inherit", "weighted_round_robin"},
		{"round_robin", "manual", "manual"},
		{"manual", "auto", "least_load"},
		{"least_ratio", "auto", "least_ratio"},
	} {
		if got := linkAlgorithm(item.pool, item.mode); got != item.want {
			t.Errorf("池%s入口%s实际%s，预期%s", item.pool, item.mode, got, item.want)
		}
	}
}
