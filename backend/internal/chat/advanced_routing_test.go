// 文件作用说明：独立验证加权积分、可选技能优先和轮询游标边界，真实容量与租户隔离由集成测试覆盖。
package chat

import (
	"context"
	"testing"
)

// TestAdvancedRoutingCandidateOrder 覆盖初始权重、已补偿积分、可选技能先于权重以及轮询游标回绕。
func TestAdvancedRoutingCandidateOrder(t *testing.T) {
	cases := []struct {
		name, algorithm, want string
		cursor                int64
		items                 []routeCandidate
	}{
		{"初始权重", "weighted_round_robin", "b", 0, []routeCandidate{{ID: "a", Weight: 1}, {ID: "b", Weight: 3}}},
		{"积分补偿", "weighted_round_robin", "a", 0, []routeCandidate{{ID: "a", Weight: 1, Score: 2}, {ID: "b", Weight: 3, Score: -2}}},
		{"技能排序优先", "weighted_round_robin", "a", 0, []routeCandidate{{ID: "a", Weight: 1, Optional: 2}, {ID: "b", Weight: 1000, Optional: 1}}},
		{"积分相同时稳定排序", "weighted_round_robin", "a", 0, []routeCandidate{{ID: "b", Weight: 2}, {ID: "a", Weight: 2}}},
		{"轮询游标回绕", "round_robin", "b", 5, []routeCandidate{{ID: "a", Weight: 1}, {ID: "b", Weight: 1}}},
		{"空候选安全排队", "weighted_round_robin", "", 0, nil},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			ordered, reason, err := preferredCandidates(context.Background(), nil, routeState{Algorithm: item.algorithm, Cursor: item.cursor}, item.items)
			if err != nil || reason != "algorithm" {
				t.Fatalf("候选排序错误: reason=%s err=%v", reason, err)
			}
			if item.want == "" {
				if len(ordered) != 0 {
					t.Fatal("空资格集合不能生成候选")
				}
			} else if len(ordered) == 0 || ordered[0].ID != item.want {
				t.Fatalf("候选优先级错误: %+v", ordered)
			}
		})
	}
}
