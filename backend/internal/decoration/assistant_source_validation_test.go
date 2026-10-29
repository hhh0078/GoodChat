// 文件作用说明：验证真实生图前的静态文件预检、有界文本修复、实际用量累计及取消和供应商失败边界。
package decoration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// assistantBrokenFileOutput 在合法聊天节点外加入禁止的form和SVG，模拟实际模型生成中的结构错误。
func assistantBrokenFileOutput(t *testing.T) assistantOutput {
	output := assistantFileOutput(t, true)
	output.Files[0].Content = strings.Replace(output.Files[0].Content, "<body>", `<body><form><input name="x"></form><svg viewBox="0 0 10 10"><path d="M0 0h10v10z"></path></svg>`, 1)
	return output
}

// assistantImageFixtureResponse 返回受控的真实PNG字节，供完整静态打包流程验证。
func assistantImageFixtureResponse() *http.Response {
	var buffer bytes.Buffer
	_ = png.Encode(&buffer, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	body, _ := json.Marshal(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(buffer.Bytes())}}, "usage": map[string]int{"total_tokens": 10}})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}
}

// TestPostgresAssistantRepairsBeforeImage 验证首次结构错误被具体反馈并修复后才生图，图片只调用一次且文本用量正确累计。
func TestPostgresAssistantRepairsBeforeImage(t *testing.T) {
	f := newAssistantFixture(t)
	_, job := assistantSubmitFixture(t, f, "")
	var textCalls, imageCalls atomic.Int32
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "chat/completions") {
			attempt := textCalls.Add(1)
			if attempt == 1 {
				return assistantModelResponse(assistantBrokenFileOutput(t)), nil
			}
			raw, _ := io.ReadAll(r.Body)
			var request struct {
				Messages []struct {
					Role    string `json:"role"`
					Content any    `json:"content"`
				} `json:"messages"`
			}
			if json.Unmarshal(raw, &request) != nil || len(request.Messages) < 2 {
				t.Fatal("修复请求应保留完整候选及校验原因")
			}
			previous, _ := request.Messages[len(request.Messages)-2].Content.(string)
			reason, _ := request.Messages[len(request.Messages)-1].Content.(string)
			var candidate assistantOutput
			if json.Unmarshal([]byte(previous), &candidate) != nil || len(candidate.Files) == 0 || !strings.Contains(candidate.Files[0].Content, "<form>") || !strings.Contains(reason, "校验错误") {
				t.Error("必须将完整候选和具体校验错误交给模型修复")
			}
			return assistantModelResponse(assistantFileOutput(t, true)), nil
		}
		imageCalls.Add(1)
		return assistantImageFixtureResponse(), nil
	})}
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	result := f.mustRequest("GET", "/tenant/assistant/jobs/"+job, f.member, nil)
	usage := result["usage"].(map[string]any)
	if result["status"] != "succeeded" || textCalls.Load() != 2 || imageCalls.Load() != 1 || usage["total_tokens"] != float64(40) {
		t.Fatalf("预检修复或费用累计错误：status=%v text=%d image=%d usage=%v", result["status"], textCalls.Load(), imageCalls.Load(), usage)
	}
}

// TestPostgresAssistantRepairLimitPreservesCandidate 验证三次文件校验均失败后保存最后候选和具体错误，不执行任何生图。
func TestPostgresAssistantRepairLimitPreservesCandidate(t *testing.T) {
	f := newAssistantFixture(t)
	_, job := assistantSubmitFixture(t, f, "")
	var calls atomic.Int32
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "chat/completions") {
			t.Error("静态候选不合法时不能调用生图")
		}
		return assistantModelResponse(assistantBrokenFileOutput(t)), nil
	})}
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	result := f.mustRequest("GET", "/tenant/assistant/jobs/"+job, f.member, nil)
	usage := result["usage"].(map[string]any)
	if result["status"] != "failed" || result["error_code"] != "invalid_design" || result["phase"] != "source" || calls.Load() != 3 || usage["total_tokens"] != float64(60) {
		t.Fatal("静态修复应最多调用三次并保留具体失败")
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var candidate json.RawMessage
		var images, templates int
		if err := tx.QueryRow(context.Background(), "SELECT source_result,(SELECT count(*) FROM assistant_image_tasks WHERE job_id=$1),(SELECT count(*) FROM tenant_templates) FROM assistant_jobs WHERE id=$1", job).Scan(&candidate, &images, &templates); err != nil {
			return err
		}
		if !strings.Contains(string(candidate), "form") || images != 0 || templates != 0 {
			t.Fatal("失败候选应持久保存，不能生成图片任务或覆盖模板")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPostgresAssistantCancelledCandidateStopsRepair 验证首个候选返回前已取消时，不继续文本修复或生图。
func TestPostgresAssistantCancelledCandidateStopsRepair(t *testing.T) {
	f := newAssistantFixture(t)
	thread, job := assistantSubmitFixture(t, f, "")
	var calls atomic.Int32
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		f.mustRequest("POST", "/tenant/assistant/threads/"+thread+"/cancel", f.member, map[string]any{})
		return assistantModelResponse(assistantBrokenFileOutput(t)), nil
	})}
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	result := f.mustRequest("GET", "/tenant/assistant/jobs/"+job, f.member, nil)
	if result["status"] != "cancelled" || calls.Load() != 1 {
		t.Fatal("取消后不能继续自动修复")
	}
}

// TestPostgresAssistantProviderFailureNotRepaired 验证授权和不确定网络失败都不会被当作文件结构问题自动重复调用。
func TestPostgresAssistantProviderFailureNotRepaired(t *testing.T) {
	for _, kind := range []string{"auth", "network"} {
		t.Run(kind, func(t *testing.T) {
			f := newAssistantFixture(t)
			_, job := assistantSubmitFixture(t, f, "")
			var calls atomic.Int32
			f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if kind == "network" {
					return nil, context.DeadlineExceeded
				}
				return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("private-provider-detail")), Header: make(http.Header)}, nil
			})}
			if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
				t.Fatal(err)
			}
			result := f.mustRequest("GET", "/tenant/assistant/jobs/"+job, f.member, nil)
			if result["status"] != "failed" || calls.Load() != 1 || result["billing_status"] != "uncertain" {
				t.Fatal("供应商失败不能自动重复调用")
			}
		})
	}
}
