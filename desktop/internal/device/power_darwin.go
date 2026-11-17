// 文件作用说明：用macOS IOKit请求阻止空闲息屏和空闲睡眠，进程结束后由系统回收。
package device

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/pwr_mgt/IOPMLib.h>

// goodchat_hold 同时申请显示器与系统空闲保护，失败时释放已申请部分。
static IOReturn goodchat_hold(IOPMAssertionID *display, IOPMAssertionID *system) {
    IOReturn result = IOPMAssertionCreateWithName(kIOPMAssertionTypePreventUserIdleDisplaySleep, kIOPMAssertionLevelOn, CFSTR("GoodChat keeps the reception screen awake"), display);
    if (result != kIOReturnSuccess) return result;
    result = IOPMAssertionCreateWithName(kIOPMAssertionTypePreventUserIdleSystemSleep, kIOPMAssertionLevelOn, CFSTR("GoodChat keeps message reception awake"), system);
    if (result != kIOReturnSuccess) IOPMAssertionRelease(*display);
    return result;
}
*/
import "C"
import "fmt"

// startPower 返回仅释放本次两个断言的闭包，不模拟用户输入或覆盖锁屏设置。
func startPower() (func() error, error) {
	var display, system C.IOPMAssertionID
	if code := C.goodchat_hold(&display, &system); code != 0 {
		return nil, fmt.Errorf("系统未允许常亮请求（%d）", int32(code))
	}
	return func() error {
		a := C.IOPMAssertionRelease(display)
		b := C.IOPMAssertionRelease(system)
		if a != 0 || b != 0 {
			return fmt.Errorf("释放电源断言失败（%d/%d）", int32(a), int32(b))
		}
		return nil
	}, nil
}

// replaceFile 使用同文件系统原子替换，避免退出或断电破坏设置。
func replaceFile(source, destination string) error { return renameFile(source, destination) }
