// 文件作用说明：验证控制组CPU差分、内存未知值、配额单位及采样历史上限，不依赖宿主机当前负载。
package monitoring

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeKernelFixture 保存测试控制组计量文件，只写入当前测试临时目录。
func writeKernelFixture(t *testing.T, dir, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestCgroupCPUAndMemoryMeasurement 验证半核配额按微秒CPU差分归一化，内存max不被误当零上限。
func TestCgroupCPUAndMemoryMeasurement(t *testing.T) {
	dir := t.TempDir()
	root := dir
	proc := t.TempDir()
	group := "/system.slice/goodchat.service"
	writeCgroupLocation(t, dir, proc, group, "/")
	dir = filepath.Join(dir, strings.TrimPrefix(group, "/"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	writeKernelFixture(t, dir, "cpu.stat", "usage_usec 100000\nnr_throttled 3\n")
	writeKernelFixture(t, dir, "cpu.max", "50000 100000\n")
	writeKernelFixture(t, dir, "cpuset.cpus.effective", "0-3\n")
	writeKernelFixture(t, dir, "memory.current", "256\n")
	writeKernelFixture(t, dir, "memory.max", "1024\n")
	s := &Sampler{started: at}
	s.record(readResources(root, proc, at))
	first, _, _ := s.Snapshot()
	if first.Scope != "application_cgroup" || first.CPUPercent != nil || first.CPUCores != 0.5 || first.MemoryPercent == nil || *first.MemoryPercent != 25 {
		t.Fatal("首次采样或控制组计量范围错误")
	}
	writeKernelFixture(t, dir, "cpu.stat", "usage_usec 300000\nnr_throttled 4\n")
	s.record(readResources(root, proc, at.Add(time.Second)))
	next, _, _ := s.Snapshot()
	if next.CPUPercent == nil || math.Abs(*next.CPUPercent-40) > 0.0001 {
		t.Fatal("CPU微秒差分未按配额正确换算")
	}
	writeKernelFixture(t, dir, "memory.max", "max\n")
	writeKernelFixture(t, dir, "cpu.stat", "usage_usec 5\n")
	s.record(readResources(root, proc, at.Add(2*time.Second)))
	reset, _, _ := s.Snapshot()
	if reset.CPUPercent != nil || reset.MemoryLimit != nil || reset.MemoryPercent != nil {
		t.Fatal("重置或无限配额不应伪装为实际负载")
	}
}

// TestMonitorHistoryAndKernelParsing 验证采样历史有界、无效CPU集合被拒绝。
func TestMonitorHistoryAndKernelParsing(t *testing.T) {
	s := &Sampler{started: time.Now()}
	for i := 0; i < 130; i++ {
		s.record(sample{Resources: Resources{At: time.Now().Add(time.Duration(i) * time.Second), Scope: "test", CPUCores: 1}, usage: uint64(i) * 1000, cpuKnown: true})
	}
	_, history, _ := s.Snapshot()
	if len(history) != 120 {
		t.Fatal("监控历史未限制在两分钟")
	}
	for raw, want := range map[string]int{"0-3,6,8-9": 7, "": 0, "3-1": 0, "bad": 0, "0-1000001": 0} {
		if cpuCount(raw) != want {
			t.Fatalf("CPU集合解析错误：%q", raw)
		}
	}
}
