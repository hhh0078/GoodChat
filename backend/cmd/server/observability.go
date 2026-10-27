// 文件作用说明：记录脱敏HTTP状态与耗时，保留WebSocket升级能力，不记录路径参数、查询、身份头或业务正文。
package main

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// observedResponse 仅累计实际状态和字节数，原始响应器仍可由ResponseController访问。
type observedResponse struct {
	http.ResponseWriter
	status, bytes int
}

// Unwrap 允许标准控制器取得底层Flusher和HTTP升级能力。
func (w *observedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// FlushError 在刷新前记录隐式200，并保留标准控制器的底层错误。
func (w *observedResponse) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Flush 兼容传统流式处理器的Flusher接口，响应状态仍由统一记录器维护。
func (w *observedResponse) Flush() { _ = w.FlushError() }

// WriteHeader 只记录首次发送的最终状态，保留标准重复响应头处理。
func (w *observedResponse) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != 101 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Write 记录实际写入字节，未显式写头时使用标准200状态。
func (w *observedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	count, err := w.ResponseWriter.Write(data)
	w.bytes += count
	return count, err
}

// Hijack 将WebSocket升级交给标准控制器，升级后日志标记101而不读取帧内容。
func (w *observedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	connection, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.status = http.StatusSwitchingProtocols
	}
	return connection, buffer, err
}

// observeHTTP 只记录路由模板和随机请求编号，避免客户手机号、令牌或查询参数进入访问日志。
func observeHTTP(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		began := time.Now()
		observed := &observedResponse{ResponseWriter: w}
		defer func() {
			status := observed.status
			if status == 0 {
				status = http.StatusOK
			}
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			logger.Info("http_request", "request_id", r.Header.Get("X-Request-ID"), "method", r.Method, "route", route, "status", status, "bytes", observed.bytes, "duration_ms", time.Since(began).Milliseconds())
		}()
		next.ServeHTTP(observed, r)
	})
}
