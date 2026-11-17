// 文件作用说明：验证压测基线按安全系数向下取整、在线余量封底及资源规格变化失效，不伪造未测容量。
package monitoring

import (
	"strings"
	"testing"

	"supportdesk/backend/internal/localization"
)

// TestResourceTranslationPreservesKernelData 验证九语采样说明不会修改控制组原路径、读数或共享采样。
func TestResourceTranslationPreservesKernelData(t *testing.T) {
	for _, locale := range localization.Locales {
		original := Resources{Scope: "application_cgroup", CgroupPath: "/services/客户进程", CPUCores: 2}
		original.ScopeDetail = "当前进程控制组：" + original.CgroupPath + "，计量包含同组进程"
		translated := localizedResources(locale, original)
		if translated.CgroupPath != original.CgroupPath || translated.CPUCores != 2 || translated.Scope != original.Scope || !strings.Contains(translated.ScopeDetail, original.CgroupPath) {
			t.Fatalf("%s 翻译修改了原始资源数据", locale)
		}
		if locale != "zh-CN" && translated.ScopeDetail == original.ScopeDetail {
			t.Fatalf("%s 未翻译系统说明", locale)
		}
		if original.ScopeDetail != "当前进程控制组：/services/客户进程，计量包含同组进程" {
			t.Fatal("翻译污染了共享原始采样")
		}
	}
}

// TestCapacityUsesMeasuredCeilingAndResourceFingerprint 验证人工实测边界、去重在线扣除和规格变化。
func TestCapacityUsesMeasuredCeilingAndResourceFingerprint(t *testing.T) {
	limit := uint64(2 * 1024 * 1024 * 1024)
	resources := Resources{Scope: "application_cgroup", CPUCores: 2, MemoryLimit: &limit}
	stored := &baseline{TestedPeople: 1001, SafetyFactor: 0.7, Spec: specification(resources)}
	stored.Fingerprint = specFingerprint(stored.Spec)
	uncalibrated := evaluateCapacity(resources, nil, 3)
	if uncalibrated.Status != "uncalibrated" || uncalibrated.Additional != nil {
		t.Fatal("没有真实基线时不应出现预测人数")
	}
	current := evaluateCapacity(resources, stored, 123)
	if current.Status != "calibrated" || current.SafeOnline == nil || *current.SafeOnline != 700 || current.Additional == nil || *current.Additional != 577 {
		t.Fatal("容量未按实测人数乘系数向下取整后扣除在线人数")
	}
	busy := evaluateCapacity(resources, stored, 900)
	if busy.Additional == nil || *busy.Additional != 0 {
		t.Fatal("在线超过安全容量时余量不能为负")
	}
	changed := resources
	changed.CPUCores = 4
	stale := evaluateCapacity(changed, stored, 3)
	if stale.Status != "stale" || stale.Additional != nil {
		t.Fatal("CPU规格变化后不能复用旧压测")
	}
	newLimit := limit * 2
	changed = resources
	changed.MemoryLimit = &newLimit
	if evaluateCapacity(changed, stored, 3).Status != "stale" {
		t.Fatal("内存上限变化后不能复用旧基线")
	}
	changed = resources
	changed.CgroupPath = "/another.service"
	if evaluateCapacity(changed, stored, 3).Status != "stale" {
		t.Fatal("控制组归属变化后不能复用旧基线")
	}
	percent := 99.0
	changed = resources
	changed.CPUPercent = &percent
	changed.MemoryPercent = &percent
	if evaluateCapacity(changed, stored, 3).Status != "calibrated" {
		t.Fatal("正常负载变化不应让硬件基线失效")
	}
}

// TestUnlimitedMemoryCapacityTracksVisibleSpecification 验证未设内存配额时可见内存变化也会使基线失效。
func TestUnlimitedMemoryCapacityTracksVisibleSpecification(t *testing.T) {
	visible := uint64(8 * 1024 * 1024 * 1024)
	resources := Resources{Scope: "application_cgroup", CPUCores: 2, VisibleMemory: &visible}
	stored := &baseline{TestedPeople: 100, SafetyFactor: 0.7, Spec: specification(resources)}
	stored.Fingerprint = specFingerprint(stored.Spec)
	changed := resources
	more := visible * 2
	changed.VisibleMemory = &more
	if evaluateCapacity(changed, stored, 0).Status != "stale" {
		t.Fatal("无独立配额时环境可见内存变化未被识别")
	}
}
