// Package monitoring 采样应用运行控制组或Go进程资源，明确计量范围，不推断物理宿主机或未压测容量。
package monitoring

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Resources 保存采样时实际可读取的资源，未知指标使用空值而不是假零。
type Resources struct {
	At            time.Time `json:"at"`
	Scope         string    `json:"scope"`
	ScopeDetail   string    `json:"scope_detail"`
	CgroupPath    string    `json:"cgroup_path,omitempty"`
	CPUPercent    *float64  `json:"cpu_percent"`
	CPUCores      float64   `json:"cpu_cores"`
	MemoryUsed    *uint64   `json:"memory_used_bytes"`
	MemoryLimit   *uint64   `json:"memory_limit_bytes"`
	MemoryPercent *float64  `json:"memory_percent"`
	VisibleMemory *uint64   `json:"visible_memory_bytes"`
	GoHeap        uint64    `json:"go_heap_bytes"`
	Goroutines    int       `json:"goroutines"`
	CPUThrottled  *uint64   `json:"cpu_throttled_periods"`
}

// sample 保存计算CPU差分所需的单调累计用量，不通过API公开内部采样状态。
type sample struct {
	Resources
	usage    uint64
	cpuKnown bool
}

// Sampler 维护单实例最近两分钟的一秒采样，HTTP读取不启动独立采样循环。
type Sampler struct {
	mu      sync.Mutex
	latest  sample
	history []Resources
	started time.Time
}

// NewSampler 初始化应用资源采样器，首次CPU因无差分样本而保持未知。
func NewSampler() *Sampler {
	s := &Sampler{started: time.Now()}
	s.record(readResources("/sys/fs/cgroup", "/proc", time.Now()))
	return s
}

// Start 在应用生命周期内定期采样，退出时释放时钟，不改变系统资源配置。
func (s *Sampler) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.record(readResources("/sys/fs/cgroup", "/proc", time.Now()))
		}
	}
}

// Snapshot 返回资源与历史的独立副本，避免HTTP编码与采样并发修改同一切片。
func (s *Sampler) Snapshot() (Resources, []Resources, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latest.Resources, append([]Resources{}, s.history...), time.Since(s.started).Seconds()
}

// record 只在同范围且累计CPU未重置时计算差分，保留真实采样空洞。
func (s *Sampler) record(next sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.latest
	duration := next.At.Sub(previous.At).Seconds()
	if next.cpuKnown && previous.cpuKnown && next.Scope == previous.Scope && next.CgroupPath == previous.CgroupPath && next.usage >= previous.usage && duration > 0 && next.CPUCores > 0 {
		value := math.Min(100, math.Max(0, float64(next.usage-previous.usage)/1_000_000/duration/next.CPUCores*100))
		next.CPUPercent = &value
	}
	s.latest = next
	s.history = append(s.history, next.Resources)
	if len(s.history) > 120 {
		s.history = append([]Resources{}, s.history[len(s.history)-120:]...)
	}
}

// readResources 优先定位当前进程的cgroup v2并计量同组进程，归属或指标无法确认时仅报告Go进程。
func readResources(cgroup, proc string, at time.Time) sample {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	result := sample{Resources: Resources{At: at, Scope: "go_process", CPUCores: float64(runtime.NumCPU()), GoHeap: memory.HeapAlloc, Goroutines: runtime.NumGoroutine()}}
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) == nil {
		result.usage = uint64(usage.Utime.Sec+usage.Stime.Sec)*1_000_000 + uint64(usage.Utime.Usec+usage.Stime.Usec)
		result.cpuKnown = true
	}
	if data, err := os.ReadFile(filepath.Join(proc, "self", "statm")); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 1 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				value := pages * uint64(os.Getpagesize())
				result.MemoryUsed = &value
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(proc, "meminfo")); err == nil {
		values := keyValues(string(data))
		if value, ok := values["MemTotal:"]; ok {
			value *= 1024
			result.VisibleMemory = &value
		}
	}
	directory, group, detail := currentCgroup(cgroup, proc, knownContainer())
	result.ScopeDetail = detail
	if directory == "" {
		return result
	}
	data, err := os.ReadFile(filepath.Join(directory, "cpu.stat"))
	if err == nil {
		values := keyValues(string(data))
		if used, ok := values["usage_usec"]; ok {
			result.Scope = "application_cgroup"
			result.CgroupPath = group
			result.usage = used
			result.cpuKnown = true
			result.MemoryUsed = nil
			if throttled, ok := values["nr_throttled"]; ok {
				result.CPUThrottled = &throttled
			}
			if raw, err := os.ReadFile(filepath.Join(directory, "cpu.max")); err == nil {
				fields := strings.Fields(string(raw))
				if len(fields) == 2 && fields[0] != "max" {
					quota, qErr := strconv.ParseFloat(fields[0], 64)
					period, pErr := strconv.ParseFloat(fields[1], 64)
					if qErr == nil && pErr == nil && quota > 0 && period > 0 {
						result.CPUCores = math.Min(result.CPUCores, quota/period)
					}
				}
			}
			if raw, err := os.ReadFile(filepath.Join(directory, "cpuset.cpus.effective")); err == nil {
				if count := cpuCount(string(raw)); count > 0 {
					result.CPUCores = math.Min(result.CPUCores, float64(count))
				}
			}
			result.MemoryUsed = readUint(filepath.Join(directory, "memory.current"))
			result.MemoryLimit = readUint(filepath.Join(directory, "memory.max"))
		}
	}
	if result.Scope == "go_process" {
		result.ScopeDetail = "当前进程控制组指标不可读取，仅统计Go进程"
	}
	if result.MemoryUsed != nil && result.MemoryLimit != nil && *result.MemoryLimit > 0 {
		value := math.Min(100, float64(*result.MemoryUsed)/float64(*result.MemoryLimit)*100)
		result.MemoryPercent = &value
	}
	return result
}

// readUint 读取内核无符号计数，max及不可读内容表示未配置或未知。
func readUint(path string) *uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

// keyValues 解析内核以键值行输出的累计计量，忽略单位及不认识的行。
func keyValues(raw string) map[string]uint64 {
	result := map[string]uint64{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			result[fields[0]] = value
		}
	}
	return result
}

// cpuCount 解析有效CPU集合，不接受反向或过大范围，避免损坏数据导致无界循环。
func cpuCount(raw string) int {
	count := 0
	for _, part := range strings.Split(strings.TrimSpace(raw), ",") {
		ends := strings.Split(part, "-")
		first, err := strconv.Atoi(ends[0])
		if err != nil || first < 0 {
			return 0
		}
		last := first
		if len(ends) == 2 {
			last, err = strconv.Atoi(ends[1])
			if err != nil {
				return 0
			}
		} else if len(ends) != 1 {
			return 0
		}
		if last < first || last > 1_000_000 {
			return 0
		}
		count += last - first + 1
	}
	return count
}
