//go:build !windows

// 文件作用说明：在非Windows系统原子替换本机设备设置文件。
package device

import "os"

// renameFile 不跨文件系统移动配置文件。
func renameFile(source, destination string) error { return os.Rename(source, destination) }
