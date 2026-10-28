// 文件作用说明：用临时目录中的真实proc与控制组文件验证子组定位、容器私有根和不明范围回退。
package monitoring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCgroupLocation 构造与Linux相同格式的proc归属和mountinfo，所有路径限制在测试临时目录。
func writeCgroupLocation(t *testing.T, root, proc, group, mountRoot string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(proc, "self"), 0700); err != nil {
		t.Fatal(err)
	}
	encode := strings.NewReplacer(`\`, `\134`, " ", `\040`, "\t", `\011`)
	writeKernelFixture(t, filepath.Join(proc, "self"), "cgroup", "0::"+group+"\n")
	writeKernelFixture(t, filepath.Join(proc, "self"), "mountinfo", fmt.Sprintf("25 24 0:30 %s %s rw,nosuid,nodev - cgroup2 cgroup rw\n", encode.Replace(mountRoot), encode.Replace(root)))
}

// TestCgroupLocationUsesCurrentSubgroup 验证普通systemd进程读取实际服务子组而非整机挂载根。
func TestCgroupLocationUsesCurrentSubgroup(t *testing.T) {
	root, proc := t.TempDir(), t.TempDir()
	group := "/system.slice/goodchat.service"
	writeCgroupLocation(t, root, proc, group, "/")
	subgroup := filepath.Join(root, strings.TrimPrefix(group, "/"))
	if err := os.MkdirAll(subgroup, 0700); err != nil {
		t.Fatal(err)
	}
	writeKernelFixture(t, root, "cpu.stat", "usage_usec 9000000\n")
	writeKernelFixture(t, root, "memory.current", "9999999\n")
	writeKernelFixture(t, subgroup, "cpu.stat", "usage_usec 100000\n")
	writeKernelFixture(t, subgroup, "cpu.max", "50000 100000\n")
	writeKernelFixture(t, subgroup, "memory.current", "256\n")
	writeKernelFixture(t, subgroup, "memory.max", "1024\n")
	directory, path, _ := currentCgroup(root, proc, false)
	if directory != subgroup || path != group {
		t.Fatal("未定位到systemd实际子组")
	}
	result := readResources(root, proc, time.Now())
	if result.Scope != "application_cgroup" || result.CgroupPath != group || result.MemoryUsed == nil || *result.MemoryUsed != 256 || result.usage != 100000 {
		t.Fatal("子组采样误用了根控制组负载")
	}
}

// TestCgroupMountRootAndPrivateNamespace 验证子树挂载与已知容器私有根，同时拒绝普通主机根范围。
func TestCgroupMountRootAndPrivateNamespace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "control groups")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	proc := t.TempDir()
	writeCgroupLocation(t, root, proc, "/user.slice/user-1000.slice/app.scope", "/user.slice/user-1000.slice")
	directory, _, _ := currentCgroup(root, proc, false)
	if directory != filepath.Join(root, "app.scope") {
		t.Fatal("子树挂载或空格路径解析错误")
	}
	writeCgroupLocation(t, root, proc, "/", "/")
	if directory, _, reason := currentCgroup(root, proc, false); directory != "" || !strings.Contains(reason, "无法确认") {
		t.Fatal("普通Linux根控制组不得标成应用")
	}
	if directory, path, _ := currentCgroup(root, proc, true); directory != root || path != "/" {
		t.Fatal("已知容器私有控制组根应可以读取")
	}
}

// TestCgroupRejectsUnknownOrEscapingMount 验证信息损坏、越界或非v2挂载回退到Go进程范围。
func TestCgroupRejectsUnknownOrEscapingMount(t *testing.T) {
	root, proc := t.TempDir(), t.TempDir()
	for _, group := range []string{"/../outside", "relative", "/other.slice/app.scope"} {
		writeCgroupLocation(t, root, proc, group, "/system.slice")
		if directory, _, _ := currentCgroup(root, proc, false); directory != "" {
			t.Fatalf("不应读取无法确认的归属：%s", group)
		}
	}
	writeCgroupLocation(t, root, proc, "/system.slice/app.scope", "/")
	writeKernelFixture(t, filepath.Join(proc, "self"), "mountinfo", "25 24 0:30 / /unknown rw - ext4 device rw\n")
	result := readResources(root, proc, time.Now())
	if result.Scope != "go_process" || !strings.Contains(result.ScopeDetail, "仅统计Go进程") {
		t.Fatal("无法确认的控制组应明确回退")
	}
}
