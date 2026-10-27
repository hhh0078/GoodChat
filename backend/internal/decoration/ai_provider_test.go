// 文件作用说明：使用本地假模型服务验证兼容协议、失败和安全校验，不代表真实供应商业务验收。
package decoration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestProviderProtocolAndUsage 使用假供应商验证真实HTTP协议、服务端鉴权和用量记录。
func TestProviderProtocolAndUsage(t *testing.T) {
	design := defaultDesign("schema", "本地假模型结果")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-only-key" {
			t.Error("模型路径或服务端鉴权不符")
		}
		var request struct {
			Model    string              `json:"model"`
			Messages []map[string]string `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Model != "test-model" || len(request.Messages) != 2 {
			t.Error("模型请求结构不符")
		}
		if strings.Contains(request.Messages[1]["content"], "test-only-key") {
			t.Error("密钥不得进入模型提示词")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(design)}}}, "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 150, "total_tokens": 200}})
	}))
	defer server.Close()
	service, err := New(nil, nil, platform.Config{PublicURL: "http://127.0.0.1:13000", CodeOrigin: "http://localhost:18081", AIEndpoint: server.URL, AIKey: "test-only-key", AIModel: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.callProvider(context.Background(), "schema", "创建一个简洁咨询页面", "test-model", defaultDesign("schema", "初稿"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Billing != "reported" || !json.Valid(result.Usage) || !strings.Contains(string(result.Design), "本地假模型结果") {
		t.Fatal("应保留校验后的候选设计和供应商用量")
	}
}

// TestProviderRejectsBadAndRedirectedResponses 覆盖供应商错误、无效设计与重定向密钥泄露边界。
func TestProviderRejectsBadAndRedirectedResponses(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.WriteHeader(500) }))
	defer target.Close()
	for _, mode := range []string{"error", "throttle", "unsafe", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "error":
					http.Error(w, "sensitive-provider-detail", 401)
				case "throttle":
					http.Error(w, "sensitive-provider-detail", 429)
				case "unsafe":
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"html":"<iframe src='http://127.0.0.1'></iframe>","css":"","javascript":"GoodChat.getState()"}`}}}, "usage": map[string]int{"total_tokens": 50}})
				case "redirect":
					http.Redirect(w, r, target.URL, 307)
				}
			}))
			defer server.Close()
			service, _ := New(nil, nil, platform.Config{PublicURL: "http://127.0.0.1:13000", CodeOrigin: "http://localhost:18081", AIEndpoint: server.URL, AIKey: "test-key", AIModel: "test-model"})
			result, err := service.callProvider(context.Background(), "code", "测试需求", "test-model", defaultDesign("code", "test"))
			if err == nil {
				t.Fatal("供应商异常必须真实失败")
			}
			if mode == "error" || mode == "throttle" {
				var typed *platform.APIError
				expected := "ai_provider_auth_failed"
				if mode == "throttle" {
					expected = "ai_provider_rate_limited"
				}
				if !errors.As(err, &typed) || typed.Code != expected {
					t.Fatal("供应商授权与额度失败需要明确区分")
				}
			}
			if strings.Contains(err.Error(), "sensitive-provider-detail") {
				t.Fatal("供应商内部详情不得回传给租户")
			}
			if len(result.Design) > 0 {
				t.Fatal("不安全设计不得成为可发布结果")
			}
		})
	}
	if targetCalls != 0 {
		t.Fatal("模型请求不得跟随重定向发送密钥")
	}
}

// TestUnconfiguredAIUnavailable 检查缺少模型配置时直接503且不创建假成功。
func TestUnconfiguredAIUnavailable(t *testing.T) {
	service, _ := New(nil, nil, platform.Config{PublicURL: "http://127.0.0.1:13000", CodeOrigin: "http://localhost:18081"})
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/tenant/ai-design-jobs", strings.NewReader(`{"name":"test","prompt":"test prompt","render_mode":"schema"}`))
	service.createAIJob(response, request)
	if response.Code != 503 || !strings.Contains(response.Body.String(), "ai_unavailable") {
		t.Fatal("缺少真实配置必须明确不可用")
	}
}
