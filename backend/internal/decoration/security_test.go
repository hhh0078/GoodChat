// 文件作用说明：验证供应商元数据脱敏、AI任务撤权和浏览器实际执行的CSP交集边界。
package decoration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"supportdesk/backend/internal/platform"
)

// TestProviderUsageRedactsUnknownMetadata 只保留整数token数量，避免供应商额外秘密或正文进入租户任务响应。
func TestProviderUsageRedactsUnknownMetadata(t *testing.T) {
	usage, reported := safeProviderUsage(json.RawMessage(`{"total_tokens":20,"prompt_tokens":-1,"output_tokens":1.5,"api_key":"provider-secret","prompt":"private-echo","prompt_tokens_details":{"cached_tokens":2,"secret":"hidden"}}`))
	if !reported || string(usage) != `{"total_tokens":20}` {
		t.Fatalf("应只保留已知非负整数计数，收到%s", usage)
	}
	for _, raw := range []string{`null`, `[]`, `{"api_key":"secret"}`, `{"total_tokens":9223372036854775808}`, `{"total_tokens":-10}`} {
		if _, reported := safeProviderUsage(json.RawMessage(raw)); reported {
			t.Fatal("未知或非法用量不能当作已报告计费")
		}
	}
}

// TestPostgresAIRevocationStopsApply 验证排队撤权不调用模型，调用中撤权则保留候选并阻止草稿写入。
func TestPostgresAIRevocationStopsApply(t *testing.T) {
	for _, phase := range []string{"queued", "running"} {
		t.Run(phase, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			var once sync.Once
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				once.Do(func() { close(started) })
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(defaultDesign("schema", "撤权后的模型候选"))}}}, "usage": map[string]any{"total_tokens": 80, "api_key": "must-not-leak"}})
			}))
			defer provider.Close()
			f := newIntegrationFixture(t, platform.Config{AIEndpoint: provider.URL, AIKey: "test-only-key", AIModel: "local-fake-provider"})
			job := f.mustRequest("POST", "/tenant/ai-design-jobs", f.member, map[string]any{"name": "撤权保护", "prompt": "创建绿色客服咨询页面", "render_mode": "schema"})
			id := job["id"].(string)
			tenant, claimed, err := f.service.claimAIJob(context.Background())
			if err != nil || claimed != id {
				t.Fatal("无法认领测试任务")
			}
			revoke := func() {
				if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.member); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "queued" {
				revoke()
				if err := f.service.processAIJob(context.Background(), tenant, id); err != nil {
					t.Fatal(err)
				}
			} else {
				done := make(chan error, 1)
				go func() { done <- f.service.processAIJob(context.Background(), tenant, id) }()
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("供应商未被调用")
				}
				revoke()
				close(release)
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("撤权后任务未完成保存")
				}
			}
			result := f.mustRequest("GET", "/tenant/ai-design-jobs/"+id, f.member, nil)
			if result["status"] != "failed" || result["error_code"] != "ai_permission_changed" || result["applied"] != false {
				t.Fatalf("撤权后必须拒绝应用任务：%v", result)
			}
			if phase == "queued" && calls.Load() != 0 {
				t.Fatal("排队期间撤权后不应调用模型")
			}
			if phase == "running" && (!containsString(result["result"], "撤权后的模型候选") || containsString(result["usage"], "must-not-leak")) {
				t.Fatal("应保留已校验候选和纯计数用量")
			}
		})
	}
}

// TestBrowserCSPIntersection 通过显式开启的真实浏览器验证nonce不能授权外部脚本；普通测试不启动浏览器。
func TestBrowserCSPIntersection(t *testing.T) {
	if os.Getenv("GOODCHAT_BROWSER_CSP_TEST") != "1" {
		t.Skip("需要显式开启并由浏览器打开测试地址")
	}
	var external atomic.Int32
	done := make(chan map[string]string, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/external.js" {
			external.Add(1)
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = fmt.Fprint(w, "window.externalRan=true")
			return
		}
		if r.URL.Path == "/done" {
			values := map[string]string{}
			for _, key := range []string{"sdk", "code", "unapproved", "external"} {
				values[key] = r.URL.Query().Get(key)
			}
			select {
			case done <- values:
			default:
			}
			_, _ = fmt.Fprint(w, "浏览器安全验证完成")
			return
		}
		setRenderPolicy(w, "browser-test-nonce-123456", server.URL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>GoodChat CSP本地测试</title><script>window.unapprovedRan=true</script><script nonce="browser-test-nonce-123456">window.sdkRan=true</script><script nonce="browser-test-nonce-123456">window.codeRan=true;const script=document.createElement('script');script.nonce=document.currentScript.nonce;script.src='%s/external.js';document.head.append(script);setTimeout(()=>{location.href='/done?sdk='+Boolean(window.sdkRan)+'&code='+Boolean(window.codeRan)+'&unapproved='+Boolean(window.unapprovedRan)+'&external='+Boolean(window.externalRan)},500)</script>`, server.URL)
	}))
	defer server.Close()
	t.Log("浏览器CSP验证地址：", server.URL)
	select {
	case flags := <-done:
		if external.Load() != 0 || flags["sdk"] != "true" || flags["code"] != "true" || flags["unapproved"] != "false" || flags["external"] != "false" {
			t.Fatalf("CSP实际执行不符合预期：%v，外部请求%d", flags, external.Load())
		}
	case <-time.After(45 * time.Second):
		t.Fatal("未收到浏览器验证，需手动打开日志中的本地测试地址")
	}
}

// TestProviderTimeoutDoesNotRevealSecrets 确认供应商超时使用固定业务错误，不能暴露服务端密钥或内部端点。
func TestProviderTimeoutDoesNotRevealSecrets(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer provider.Close()
	s, _ := New(nil, nil, platform.Config{PublicURL: "http://127.0.0.1:13000", CodeOrigin: "http://localhost:18081", AIEndpoint: provider.URL, AIKey: "timeout-private-key", AIModel: "test"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := s.callProvider(ctx, "schema", "测试超时需求", "test", defaultDesign("schema", "测试"))
	if err == nil || strings.Contains(err.Error(), "timeout-private-key") || strings.Contains(err.Error(), provider.URL) || result.Billing != "uncertain" {
		t.Fatal("供应商超时必须固定脱敏错误并保留费用待核对状态")
	}
}
