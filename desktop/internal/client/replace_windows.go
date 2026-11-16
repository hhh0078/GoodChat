// 文件作用说明：在Windows上原子替换已存在的桌面配置文件。
//go:build windows

package client

import "golang.org/x/sys/windows"

// replaceFile 使用系统替换语义，避免Windows重命名不能覆盖旧文件。
func replaceFile(source, target string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
