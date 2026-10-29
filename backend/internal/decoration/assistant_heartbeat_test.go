// 文件作用说明：以短间隔心跳和受控阻塞模型验证长图片调用续租、终态保护及取消，不实际等待十分钟。
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
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestPostgresAssistantHeartbeatRenewsAndStops 验证短租约持续续期，另一个工作器不误判中断，任务终态则停止续租并取消调用。
func TestPostgresAssistantHeartbeatRenewsAndStops(t *testing.T) {
	f := newAssistantFixture(t)
	_, job := assistantSubmitFixture(t, f, "")
	ctx := context.Background()
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE assistant_jobs SET status='running',lease_until=now()-interval '1 second' WHERE id=$1", job)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.service.assistantHeartbeat(callCtx, f.tenant, job, 10*time.Millisecond, 2*time.Second, cancel)
	}()
	deadline := time.Now().Add(2 * time.Second)
	renewed := false
	for time.Now().Before(deadline) {
		if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT lease_until>now()+interval '1 second' AND EXISTS(SELECT 1 FROM assistant_queue WHERE job_id=$1 AND available_at>now()+interval '1 second') FROM assistant_jobs WHERE id=$1", job).Scan(&renewed)
		}); err != nil {
			t.Fatal(err)
		}
		if renewed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !renewed {
		t.Fatal("图片调用期间应同时续任务与调度租约")
	}
	if err := f.service.processAssistantJob(ctx, f.tenant, job); err != nil {
		t.Fatal(err)
	}
	if err := platform.TenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, "SELECT status FROM assistant_jobs WHERE id=$1", job).Scan(&status); err != nil {
			return err
		}
		if status != "running" {
			t.Fatal("另一工作器不能误判仍有心跳的调用")
		}
		_, err := tx.Exec(ctx, "UPDATE assistant_jobs SET status='failed',error_code='test_terminal',completed_at=now() WHERE id=$1", job)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("终态应停止心跳")
	}
	if callCtx.Err() == nil {
		t.Fatal("终态应取消仍在等待的供应商请求")
	}
}

// TestPostgresAssistantLateSourceCannotAdvance 验证文本调用返回前已失败的任务只保存用量，不重新创建图片任务或覆盖终态。
func TestPostgresAssistantLateSourceCannotAdvance(t *testing.T) {
	f := newAssistantFixture(t)
	_, job := assistantSubmitFixture(t, f, "")
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), "UPDATE assistant_jobs SET status='failed',error_code='test_terminal',error_message='已经终止',completed_at=now() WHERE id=$1", job)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return assistantModelResponse(assistantFileOutput(t, true)), nil
	})}
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var status, code string
		var source json.RawMessage
		var images int
		if err := tx.QueryRow(context.Background(), "SELECT status,error_code,source_result,(SELECT count(*) FROM assistant_image_tasks WHERE job_id=$1) FROM assistant_jobs WHERE id=$1", job).Scan(&status, &code, &source, &images); err != nil {
			return err
		}
		if status != "failed" || code != "test_terminal" || len(source) != 0 || images != 0 {
			t.Fatal("迟到文本不能推进已经终止的任务")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPostgresAssistantLateImageCannotStartNext 验证首张图片返回时任务已失败，不会继续调用下一张图片。
func TestPostgresAssistantLateImageCannotStartNext(t *testing.T) {
	f := newAssistantFixture(t)
	_, job := assistantSubmitFixture(t, f, "")
	var imageCalls atomic.Int32
	var picture bytes.Buffer
	_ = png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 16, 16)))
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "chat/completions") {
			output := assistantFileOutput(t, true)
			output.ImageTasks = append(output.ImageTasks, assistantImageTask{ID: "second", Path: "static/second.jpg", Prompt: "第二张绿色服务插画", Width: 1024, Height: 1024})
			return assistantModelResponse(output), nil
		}
		imageCalls.Add(1)
		if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), "UPDATE assistant_jobs SET status='failed',error_code='test_terminal',completed_at=now() WHERE id=$1", job)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(picture.Bytes())}}, "usage": map[string]int{"total_tokens": 10}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	if err := f.service.processAssistantJob(context.Background(), f.tenant, job); err != nil {
		t.Fatal(err)
	}
	if imageCalls.Load() != 1 {
		t.Fatal("终态任务不得继续调用下一张图片")
	}
	result := f.mustRequest("GET", "/tenant/assistant/jobs/"+job, f.member, nil)
	if result["status"] != "failed" || result["error_code"] != "test_terminal" {
		t.Fatal("迟到图片不能改变既有终态")
	}
}

// TestPostgresAssistantCancelWaitingImage 验证取消会中止仍在等待的图片请求，并使处理器与心跳一起结束。
func TestPostgresAssistantCancelWaitingImage(t *testing.T) {
	f := newAssistantFixture(t)
	thread, job := assistantSubmitFixture(t, f, "")
	started := make(chan struct{})
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "chat/completions") {
			return assistantModelResponse(assistantFileOutput(t, true)), nil
		}
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	done := make(chan error, 1)
	go func() { done <- f.service.processAssistantJob(context.Background(), f.tenant, job) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("图片请求未开始")
	}
	f.mustRequest("POST", "/tenant/assistant/threads/"+thread+"/cancel", f.member, map[string]any{})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消未中止等待中的图片请求")
	}
	if _, exists := f.service.assistantCancels.Load(job); exists {
		t.Fatal("任务结束后应移除取消句柄并停止心跳")
	}
}

// TestPostgresAssistantRetrySeparatesLateAttempt 验证取消后立即重试使用新编号，旧供应商迟到响应和旧取消句柄清理均不能污染新任务。
func TestPostgresAssistantRetrySeparatesLateAttempt(t *testing.T) {
	f := newAssistantFixture(t)
	thread, oldJob := assistantSubmitFixture(t, f, "")
	oldStarted, newStarted := make(chan struct{}), make(chan struct{})
	oldRelease, newRelease := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	f.service.assistantClient = &http.Client{Transport: assistantRoundTripper(func(r *http.Request) (*http.Response, error) {
		attempt := calls.Add(1)
		if attempt == 1 {
			close(oldStarted)
			select {
			case <-oldRelease:
			case <-time.After(3 * time.Second):
				return nil, context.DeadlineExceeded
			}
			return assistantModelResponse(assistantFileOutput(t, true)), nil
		}
		close(newStarted)
		select {
		case <-newRelease:
		case <-time.After(3 * time.Second):
			return nil, context.DeadlineExceeded
		}
		return assistantModelResponse(assistantOutput{SchemaVersion: 1, Type: "text", Message: "新一轮正常完成"}), nil
	})}
	oldDone := make(chan error, 1)
	go func() { oldDone <- f.service.processAssistantJob(context.Background(), f.tenant, oldJob) }()
	select {
	case <-oldStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("旧请求未开始")
	}
	f.mustRequest("POST", "/tenant/assistant/threads/"+thread+"/cancel", f.member, map[string]any{})
	retried := f.mustRequest("POST", "/tenant/assistant/jobs/"+oldJob+"/retry", f.member, map[string]any{})
	newJob := retried["id"].(string)
	if newJob == oldJob {
		t.Fatal("重试不能复用旧编号")
	}
	newDone := make(chan error, 1)
	go func() { newDone <- f.service.processAssistantJob(context.Background(), f.tenant, newJob) }()
	select {
	case <-newStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("新请求未开始")
	}
	close(oldRelease)
	select {
	case err := <-oldDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("旧请求未结束")
	}
	if _, exists := f.service.assistantCancels.Load(newJob); !exists {
		t.Fatal("旧请求清理不能删除新一轮的取消句柄")
	}
	close(newRelease)
	select {
	case err := <-newDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("新请求未完成")
	}
	oldResult := f.mustRequest("GET", "/tenant/assistant/jobs/"+oldJob, f.member, nil)
	newResult := f.mustRequest("GET", "/tenant/assistant/jobs/"+newJob, f.member, nil)
	if oldResult["status"] != "cancelled" || newResult["status"] != "succeeded" || newResult["design"] != nil || calls.Load() != 2 {
		t.Fatal("旧响应不能覆盖新任务状态或推进旧图片任务")
	}
	if err := platform.TenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM assistant_image_tasks WHERE job_id=$1", oldJob).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatal("已取消旧响应不得新增图片任务")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
