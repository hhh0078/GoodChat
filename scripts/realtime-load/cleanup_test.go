// 文件作用说明：用最小真实HTTP测试确保动态枚举覆盖新增咨询、路径正确且清理绝不忽略错误。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCleanupEnumeratesBrowserConversations 验证不依赖初始访客数组，动态枚举两批并使用canonical动作路径。
func TestCleanupEnumeratesBrowserConversations(t *testing.T) {
	pending := []string{"browser-one", "browser-two"}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/assignments/close") {
			t.Error("清理动作路径错误")
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/conversations/"), "/assignments/close")
		fmt.Fprintf(w, `{"data":{"id":%q,"status":"closed"}}`, id)
	}))
	defer server.Close()
	a := &apiClient{base: server.URL, http: server.Client(), metrics: newMeasurements()}
	lists := 0
	n, e := closePendingConversations(context.Background(), a, identity{}, func(context.Context) ([]string, error) {
		lists++
		if lists == 1 {
			return pending, nil
		}
		return nil, nil
	})
	if e != nil || n != 2 || calls != 2 || lists != 2 {
		t.Fatal("未正确动态枚举并关闭浏览器咨询")
	}
}

// TestCleanupRejectsHTTPFailure 验证第一个关闭失败即返回真实错误，不继续或伪称清理完成。
func TestCleanupRejectsHTTPFailure(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		w.Write([]byte(`{"code":"test_close_failed"}`))
	}))
	defer server.Close()
	a := &apiClient{base: server.URL, http: server.Client(), metrics: newMeasurements()}
	n, e := closePendingConversations(context.Background(), a, identity{}, func(context.Context) ([]string, error) { return []string{"browser-one", "browser-two"}, nil })
	var failure apiFailure
	if n != 0 || calls != 1 || !errors.As(e, &failure) || failure.Status != 500 {
		t.Fatal("关闭错误被忽略")
	}
}

// TestCleanupRejectsUnconfirmed200 验证200但状态未结束也不可撤销身份或停用租户。
func TestCleanupRejectsUnconfirmed200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"id":"one","status":"active"}}`))
	}))
	defer server.Close()
	a := &apiClient{base: server.URL, http: server.Client(), metrics: newMeasurements()}
	n, e := closePendingConversations(context.Background(), a, identity{}, func(context.Context) ([]string, error) { return []string{"one"}, nil })
	if n != 0 || e == nil {
		t.Fatal("未确认的200被错误当作完成")
	}
}
