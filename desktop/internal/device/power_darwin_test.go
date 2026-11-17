// 文件作用说明：显式开启时在真实Mac上核对IOKit断言的申请和释放，不修改系统电源偏好。
package device

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMacPowerAssertions 从pmset读取真实断言，确保开关与退出均清理当前进程的请求。
func TestMacPowerAssertions(t *testing.T) {
	if os.Getenv("GOODCHAT_TEST_POWER") != "1" {
		t.Skip("需要显式开启真实系统电源检查")
	}
	p := NewPower(t.TempDir())
	defer p.Close()
	if err := p.Set(true); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/usr/bin/pmset", "-g", "assertions").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GoodChat keeps the reception screen awake", "GoodChat keeps message reception awake"} {
		if !strings.Contains(string(out), name) {
			t.Fatal("系统未报告断言", name)
		}
	}
	if err = p.Set(false); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command("/usr/bin/pmset", "-g", "assertions").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "GoodChat keeps the reception screen awake") || strings.Contains(string(out), "GoodChat keeps message reception awake") {
		t.Fatal("断言没有释放")
	}
}
