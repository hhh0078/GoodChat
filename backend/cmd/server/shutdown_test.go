// 文件作用说明：通过真实HTTP在途请求验证宽限关闭会等待响应完成，监听停止不能被当作所有请求结束。
package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestShutdownWaitsForInflightResponse 验证收到关闭信号后，已经进入的请求仍能完整返回。
func TestShutdownWaitsForInflightResponse(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "完整响应")
	}))
	defer server.Close()
	response := make(chan string, 1)
	go func() {
		r, err := http.Get(server.URL)
		if err != nil {
			response <- "error"
			return
		}
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		response <- string(body)
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	done := shutdownHTTP(ctx, server.Config)
	cancel()
	select {
	case <-done:
		close(release)
		t.Fatal("在途请求未完成就报告关闭结束")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case result := <-response:
		if result != "完整响应" {
			t.Fatal("请求被截断")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("请求未完成")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("关闭没有完成")
	}
}
