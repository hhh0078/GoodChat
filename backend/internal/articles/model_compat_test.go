// 文件作用说明：覆盖文章模型嵌套用量、非思考参数与截断输出的兼容边界。
package articles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestArticleModelCompatibility 验证供应商附加用量不会破坏有效文章，截断结果不会发布或原地重试。
func TestArticleModelCompatibility(t *testing.T) {
	for _, finish := range []string{"stop", "length"} {
		t.Run(finish, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var input map[string]any
				if json.NewDecoder(r.Body).Decode(&input) != nil || input["enable_thinking"] != false || r.URL.Path != "/v1/chat/completions" {
					t.Error("模型参数或路径错误")
				}
				draft := Draft{Language: "zh-CN", Title: "客服如何准备课程咨询知识库", Slug: "course-support-knowledge", Summary: "整理课程咨询资料并建立知识库。", Markdown: strings.Repeat("## 准备课程资料\n\n在知识库中添加已核实的课程信息。\n\n", 12), Keywords: []string{"课程咨询"}}
				body, _ := json.Marshal(draft)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": finish, "message": map[string]any{"content": string(body)}}}, "usage": map[string]any{"prompt_tokens": 123, "completion_tokens": 456, "total_tokens": 579, "completion_tokens_details": map[string]any{"reasoning_tokens": 0}, "prompt_tokens_details": map[string]any{"cached_tokens": 0}}})
			}))
			defer server.Close()
			s := New(nil, nil, platform.Config{})
			s.Model = func(context.Context) (platform.TextModel, error) {
				return platform.TextModel{BaseURL: server.URL, Model: "qwen-test", MaxOutput: 4096, Extras: map[string]any{"enable_thinking": false}}, nil
			}
			draft, usage, err := s.callModel(context.Background(), "测试", "生成文章", "zh-CN")
			if finish == "length" {
				code, _ := safeJobError(err)
				if code != "article_ai_output_limit" || calls != 1 {
					t.Fatalf("截断处理异常：%s，调用%d次", code, calls)
				}
			} else if err != nil || draft.Title == "" || usage["total_tokens"] != 579 || calls != 1 {
				t.Fatalf("嵌套用量处理异常：%v，调用%d次", err, calls)
			}
		})
	}
}
