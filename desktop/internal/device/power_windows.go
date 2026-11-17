// 文件作用说明：以Windows电源请求句柄保持显示器与系统运行，避免线程切换导致保护失效。
package device

import (
	"fmt"
	"golang.org/x/sys/windows"
	"runtime"
	"unsafe"
)

var kernel = windows.NewLazySystemDLL("kernel32.dll")
var createPower = kernel.NewProc("PowerCreateRequest")
var setPower = kernel.NewProc("PowerSetRequest")
var clearPower = kernel.NewProc("PowerClearRequest")

// startPower 同时申请显示器和系统保护，不使用有线程归属限制的执行状态API。
func startPower() (func() error, error) {
	reason, err := windows.UTF16PtrFromString("GoodChat 接待工作台保持常亮与消息接收")
	if err != nil {
		return nil, err
	}
	context := struct {
		Version uint32
		Flags   uint32
		Reason  *uint16
		Unused  [2]uintptr
	}{Version: 0, Flags: 1, Reason: reason}
	handle, _, callErr := createPower.Call(uintptr(unsafe.Pointer(&context)))
	runtime.KeepAlive(reason)
	runtime.KeepAlive(context)
	if handle == 0 || handle == ^uintptr(0) {
		return nil, fmt.Errorf("创建电源请求失败：%v", callErr)
	}
	for _, kind := range []uintptr{0, 1} {
		ok, _, err := setPower.Call(handle, kind)
		if ok == 0 {
			if kind == 1 {
				clearPower.Call(handle, 0)
			}
			windows.CloseHandle(windows.Handle(handle))
			return nil, fmt.Errorf("系统未允许常亮请求：%v", err)
		}
	}
	return func() error {
		a, _, _ := clearPower.Call(handle, 0)
		b, _, _ := clearPower.Call(handle, 1)
		err := windows.CloseHandle(windows.Handle(handle))
		if a == 0 || b == 0 {
			return fmt.Errorf("释放系统电源请求失败")
		}
		return err
	}, nil
}

// replaceFile 用系统替换操作覆盖已存在的设置文件。
func replaceFile(source, destination string) error {
	a, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	b, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(a, b, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
