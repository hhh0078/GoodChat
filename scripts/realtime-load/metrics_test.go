// 文件作用说明：回归负载工具分位数、Docker内存换算、身份格式和脱敏错误，不替代真实阶段验收。
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPercentiles 验证尾延迟使用真实观测的最近秩而非均值。
func TestPercentiles(t *testing.T) {
	v := []float64{1, 2, 3, 4, 100}
	if percentile(v, .5) != 3 || percentile(v, .95) != 100 || percentile(nil, .99) != 0 {
		t.Fatal("分位数口径错误")
	}
}

// TestMemoryUnits 验证二进制和十进制Docker用量不混淆。
func TestMemoryUnits(t *testing.T) {
	for text, want := range map[string]float64{"1.2GiB / 14GiB": 1.2 * (1 << 30), "384MiB / 2GiB": 384 << 20, "800kB / 2GB": 800000} {
		if math.Abs(memoryBytes(text)-want) > 1 {
			t.Fatal("内存换算错误")
		}
	}
}

// TestIdentityAndSafeErrors 真实应用Cookie需要43字符Base64URL，异常不能回显DSN。
func TestIdentityAndSafeErrors(t *testing.T) {
	cookie := secret("")
	raw, e := base64.RawURLEncoding.DecodeString(cookie)
	if e != nil || len(cookie) != 43 || len(raw) != 32 {
		t.Fatal("Cookie格式无效")
	}
	if !strings.HasPrefix(secret("v_"), "v_") {
		t.Fatal("访客格式无效")
	}
	if safeError(errors.New("postgres://secret")) != "*errors.errorString" {
		t.Fatal("错误未脱敏")
	}
	if safeError(&pgconn.PgError{Code: "23505", Detail: "secret"}) != "sqlstate_23505" {
		t.Fatal("SQL错误未脱敏")
	}
}

// TestMalformedSuccessCount 验证200但损坏JSON不是成功，读取/解码失败必须计入错误率。
func TestMalformedSuccessCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("broken-json")) }))
	defer server.Close()
	m := newMeasurements()
	a := apiClient{base: server.URL, http: server.Client(), metrics: m}
	if a.request(context.Background(), "GET", "/probe", identity{}, nil, nil) == nil {
		t.Fatal("应拒绝损坏响应")
	}
	if m.success != 0 || m.failure != 1 || m.statuses[200] != 1 {
		t.Fatal("损坏成功响应未正确计数")
	}
}
