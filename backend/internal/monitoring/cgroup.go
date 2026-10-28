// 文件作用说明：按当前进程归属与挂载信息定位cgroup v2，拒绝把无法证明边界的整机根控制组当作应用。
package monitoring

import (
	"os"
	"path/filepath"
	"strings"
)

// knownContainer 仅依据运行环境的容器标记确认私有根控制组，不猜测普通Linux的根控制组范围。
func knownContainer() bool {
	for _, path := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// kernelMountPath 还原mountinfo规定的路径转义，方便比较带空格的真实挂载点。
func kernelMountPath(raw string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(raw)
}

// safeAbsolute 要求内核路径为规范绝对路径，损坏或越界信息不参与资源定位。
func safeAbsolute(path string) bool { return filepath.IsAbs(path) && filepath.Clean(path) == path }

// currentCgroup 结合self/cgroup与mountinfo定位当前进程的v2子组，无法确认时返回Go进程回退说明。
// root、proc 为系统挂载根，container 标识只用于兼容容器私有cgroup namespace中的根路径。
func currentCgroup(root, proc string, container bool) (string, string, string) {
	raw, err := os.ReadFile(filepath.Join(proc, "self", "cgroup"))
	if err != nil {
		return "", "", "未取得当前进程的控制组归属，仅统计Go进程"
	}
	group := ""
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[0] == "0" && parts[1] == "" {
			if group != "" {
				return "", "", "控制组归属信息不明确，仅统计Go进程"
			}
			group = parts[2]
		}
	}
	if !safeAbsolute(group) {
		return "", "", "未取得有效的cgroup v2归属，仅统计Go进程"
	}
	if group == "/" && !container {
		return "", "", "当前进程位于可见根控制组，无法确认应用边界，仅统计Go进程"
	}
	raw, err = os.ReadFile(filepath.Join(proc, "self", "mountinfo"))
	if err != nil {
		return "", "", "未确认cgroup v2挂载范围，仅统计Go进程"
	}
	root = filepath.Clean(root)
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		after := strings.Fields(parts[1])
		before := strings.Fields(parts[0])
		if len(after) < 1 || after[0] != "cgroup2" || len(before) < 5 {
			continue
		}
		mountRoot, mountPoint := kernelMountPath(before[3]), kernelMountPath(before[4])
		if !safeAbsolute(mountRoot) || !safeAbsolute(mountPoint) || !(mountPoint == root || strings.HasPrefix(mountPoint, root+string(filepath.Separator))) {
			continue
		}
		if group == "/" && container {
			return mountPoint, group, "当前容器私有根控制组，计量包含同组进程"
		}
		relative, err := filepath.Rel(mountRoot, group)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		return filepath.Join(mountPoint, relative), group, "当前进程控制组：" + group + "，计量包含同组进程"
	}
	return "", "", "无法确认当前进程的控制组挂载，仅统计Go进程"
}
