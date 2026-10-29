// 文件作用说明：记录压测分位数和当前goodchat容器资源，达到预算即取消当前阶段，不改容器或平台容量基线。
package main

import (
	"context"
	"encoding/json"
	"math"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type measurements struct {
	mu               sync.Mutex
	times            map[string][]float64
	success, failure int
	statuses         map[int]int
}
type resource struct {
	At            time.Time `json:"at"`
	CPU           float64   `json:"cpu_percent_one_core_100"`
	Memory        float64   `json:"memory_bytes"`
	GeneratorHeap uint64    `json:"generator_heap_bytes"`
}
type safety struct {
	mu      sync.Mutex
	Samples []resource `json:"samples"`
	Stopped string     `json:"stopped"`
}

// newMeasurements 创建只存延迟和状态码的统计集合，不保存业务内容。
func newMeasurements() *measurements {
	return &measurements{times: map[string][]float64{}, statuses: map[int]int{}}
}

// record 合并并发样本；接收端延迟由其他记录点另行采样。
func (m *measurements) record(kind string, d time.Duration, ok bool, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.times[kind] = append(m.times[kind], float64(d.Microseconds())/1000)
	if kind == "http" {
		m.statuses[status]++
		if ok {
			m.success++
		} else {
			m.failure++
		}
	}
}

// percentile 使用最近秩定义，少量样本保留真实最大值而非插值出未观察延迟。
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	i := int(math.Ceil(p*float64(len(v)))) - 1
	if i < 0 {
		i = 0
	}
	return v[i]
}

// summary 返回可审计样本数和p50/p95/p99，单位均为毫秒。
func (m *measurements) summary() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	kinds := map[string]any{}
	for k, v := range m.times {
		kinds[k] = map[string]any{"samples": len(v), "p50_ms": percentile(v, .5), "p95_ms": percentile(v, .95), "p99_ms": percentile(v, .99)}
	}
	return map[string]any{"latency": kinds, "http_success": m.success, "http_failure": m.failure, "http_statuses": m.statuses}
}

// memoryBytes 解析docker stats显示的当前用量，未识别格式返回0供监控记录故障。
func memoryBytes(text string) float64 {
	field := strings.TrimSpace(strings.Split(text, "/")[0])
	for _, unit := range []struct {
		name   string
		factor float64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"B", 1}} {
		if strings.HasSuffix(field, unit.name) {
			v, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(field, unit.name)), 64)
			return v * unit.factor
		}
	}
	return 0
}

// monitor 每三秒只读当前项目容器资源，连续CPU超预算或单次内存超预算时取消升阶。
func monitor(ctx context.Context, cancel context.CancelFunc, m *measurements, s *safety) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	hot := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probe, stop := context.WithTimeout(ctx, 2500*time.Millisecond)
			raw, e := exec.CommandContext(probe, "docker", "stats", "--no-stream", "--format", "{{json .}}", "goodchat").Output()
			stop()
			if e != nil {
				continue
			}
			var value struct {
				CPU    string `json:"CPUPerc"`
				Memory string `json:"MemUsage"`
			}
			if json.Unmarshal(raw, &value) != nil {
				continue
			}
			cpu, _ := strconv.ParseFloat(strings.TrimSuffix(value.CPU, "%"), 64)
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			sample := resource{time.Now().UTC(), cpu, memoryBytes(value.Memory), mem.HeapAlloc}
			s.mu.Lock()
			s.Samples = append(s.Samples, sample)
			if cpu > 400 {
				hot++
			} else {
				hot = 0
			}
			reason := ""
			if hot >= 3 {
				reason = "container_cpu_over_400_percent_3_samples"
			}
			if sample.Memory > 1.2*(1<<30) {
				reason = "container_memory_over_1_2_GiB"
			}
			if mem.HeapAlloc > 384<<20 {
				reason = "generator_heap_over_384_MiB"
			}
			m.mu.Lock()
			if m.success+m.failure >= 20 && float64(m.failure)/float64(m.success+m.failure) > .02 {
				reason = "http_error_rate_over_2_percent"
			}
			if len(m.times["message"]) >= 20 && percentile(m.times["message"], .95) > 2000 {
				reason = "message_p95_over_2000_ms"
			}
			m.mu.Unlock()
			if reason != "" {
				s.Stopped = reason
			}
			s.mu.Unlock()
			if reason != "" {
				cancel()
				return
			}
		}
	}
}
