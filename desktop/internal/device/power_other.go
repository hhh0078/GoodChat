//go:build !darwin && !windows

// 文件作用说明：非目标平台明确提示电源功能不可用，不报告虚假的已开启状态。
package device

import "errors"

// startPower 保留跨平台编译入口，但不把不支持的平台当作成功。
func startPower() (func() error, error) {
	return nil, errors.New("当前系统暂不支持屏幕常亮")
}

// replaceFile 复用非Windows文件系统的原子替换。
func replaceFile(source, destination string) error { return renameFile(source, destination) }
