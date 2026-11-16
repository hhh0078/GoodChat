// 文件作用说明：使用Unix原子重命名替换桌面非敏感配置。
//go:build !windows

package client

import "os"

// replaceFile 原子替换已经完整写入的配置文件。
func replaceFile(source, target string) error { return os.Rename(source, target) }
