// 文件作用说明：验证常亮请求的幂等、持久化回滚、退出释放与恢复失败边界。
package device

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestPowerLifecycle 开关与重启不会重复占用系统资源，退出仍保留下次恢复的用户偏好。
func TestPowerLifecycle(t *testing.T) {
	dir := t.TempDir()
	starts, stops := 0, 0
	start := func() (func() error, error) { starts++; return func() error { stops++; return nil }, nil }
	p := newPower(dir, start)
	if err := p.Set(true); err != nil {
		t.Fatal(err)
	}
	if err := p.Set(true); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatal("重复开启创建了额外请求")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if stops != 1 {
		t.Fatal("退出没有释放")
	}
	if err := p.Set(true); err == nil {
		t.Fatal("退出后仍能申请")
	}
	q := newPower(dir, start)
	if on, _ := q.State(); !on || starts != 2 {
		t.Fatal("没有恢复用户选择")
	}
	if err := q.Set(false); err != nil {
		t.Fatal(err)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if stops != 2 {
		t.Fatal("关闭后退出又释放了一次")
	}
	r := newPower(dir, start)
	defer r.Close()
	if on, _ := r.State(); on {
		t.Fatal("关闭的选择没有持久化")
	}
}

// TestPowerSaveFailure 磁盘保存失败必须释放新请求并保留原状态。
func TestPowerSaveFailure(t *testing.T) {
	dir := t.TempDir()
	stops := 0
	p := newPower(dir, func() (func() error, error) { return func() error { stops++; return nil }, nil })
	if err := os.Mkdir(filepath.Join(dir, "device-settings.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := p.Set(true); err == nil {
		t.Fatal("不能假报保存成功")
	}
	if on, _ := p.State(); on || stops != 1 {
		t.Fatal("持久化失败后泄露电源请求")
	}
}

// TestPowerRestoreFailure 系统拒绝恢复常亮时，返回真实关闭与提示而非阻止聊天启动。
func TestPowerRestoreFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "device-settings.json"), []byte(`{"keep_awake":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := newPower(dir, func() (func() error, error) { return nil, errors.New("system denied") })
	if on, problem := p.State(); on || problem == "" {
		t.Fatal("恢复失败应显示真实状态")
	}
}
