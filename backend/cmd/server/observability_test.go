// 文件作用说明：验证访问日志不泄漏令牌和查询，并且新增观测包装仍支持刷新与WebSocket升级。
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestAccessLogRedaction 验证路由只记录模板，错误追踪编号一致，Cookie、查询和正文均不进入日志。
func TestAccessLogRedaction(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /private/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); _, _ = w.Write([]byte("saved")) })
	request := httptest.NewRequest("POST", "http://local/private/secret-path?otp=123456&key=secret-query", strings.NewReader("secret-body"))
	request.Header.Set("Authorization", "Bearer secret-auth")
	request.Header.Set("Cookie", "session=secret-cookie")
	response := httptest.NewRecorder()
	observeHTTP(middleware(platform.Config{PublicURL: "http://local"}, mux), logger).ServeHTTP(response, request)
	for _, secret := range []string{"secret-path", "secret-query", "123456", "secret-body", "secret-auth", "secret-cookie"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("敏感内容进入访问日志")
		}
	}
	var fields map[string]any
	if json.Unmarshal(output.Bytes(), &fields) != nil {
		t.Fatal("访问日志不是JSON")
	}
	if fields["status"] != float64(201) || fields["bytes"] != float64(5) || fields["route"] != "POST /private/{id}" || fields["request_id"] != response.Header().Get("X-Request-ID") {
		t.Fatalf("日志口径不一致:%v", fields)
	}
}

// TestObservedWriterSupportsFlushAndUpgrade 验证控制器刷新和真实TCP升级不被状态记录器破坏。
func TestObservedWriterSupportsFlushAndUpgrade(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapped := &observedResponse{ResponseWriter: recorder}
	if err := http.NewResponseController(wrapped).Flush(); err != nil || !recorder.Flushed {
		t.Fatal("刷新能力丢失", err)
	}
	server := httptest.NewServer(observeHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, buffer, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
		_ = buffer.Flush()
	}), slog.New(slog.NewJSONHandler(io.Discard, nil))))
	defer server.Close()
	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _ = io.WriteString(connection, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")
	raw, err := io.ReadAll(connection)
	if err != nil || !strings.Contains(string(raw), "101 Switching Protocols") {
		t.Fatal("真实升级失败", err)
	}
}
