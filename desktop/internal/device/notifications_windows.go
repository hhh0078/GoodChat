// 文件作用说明：读取Windows ToastNotifier真实开关与策略状态，补齐SDK恒返回已授权的缺口。
package device

import (
	"fmt"
	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
	"runtime"
	"syscall"
	"unsafe"
)

type managerTable struct {
	ole.IInspectableVtbl
	Create       uintptr
	CreateWithID uintptr
	Template     uintptr
}
type notifierTable struct {
	ole.IInspectableVtbl
	Show    uintptr
	Hide    uintptr
	Setting uintptr
}

// CheckNotifications 在固定线程读取当前应用通知状态，拒绝把系统或组策略禁用视为已授权。
func CheckNotifications(_ func() (bool, error)) (Notification, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	com := windows.NewLazySystemDLL("combase.dll")
	hr, _, _ := com.NewProc("RoInitialize").Call(1)
	if int32(hr) < 0 && uint32(hr) != 0x80010106 {
		return Notification{}, fmt.Errorf("通知组件初始化失败：%x", hr)
	}
	if int32(hr) >= 0 {
		defer com.NewProc("RoUninitialize").Call()
	}
	factory, err := ole.RoGetActivationFactory("Windows.UI.Notifications.ToastNotificationManager", ole.NewGUID("50AC103F-D235-4598-BBEF-98FE4D1A3AD4"))
	if err != nil {
		return Notification{}, err
	}
	if factory == nil {
		return Notification{}, fmt.Errorf("系统通知管理器不可用")
	}
	defer factory.Release()
	app, err := ole.NewHString("GoodChat")
	if err != nil {
		return Notification{}, err
	}
	defer ole.DeleteHString(app)
	var notifier *ole.IInspectable
	manager := (*managerTable)(unsafe.Pointer(factory.RawVTable))
	hr, _, _ = syscall.SyscallN(manager.CreateWithID, uintptr(unsafe.Pointer(factory)), uintptr(app), uintptr(unsafe.Pointer(&notifier)))
	if int32(hr) < 0 || notifier == nil {
		return Notification{}, fmt.Errorf("读取应用通知入口失败：%x", hr)
	}
	defer notifier.Release()
	var setting uint32
	table := (*notifierTable)(unsafe.Pointer(notifier.RawVTable))
	hr, _, _ = syscall.SyscallN(table.Setting, uintptr(unsafe.Pointer(notifier)), uintptr(unsafe.Pointer(&setting)))
	if int32(hr) < 0 {
		return Notification{}, fmt.Errorf("读取系统通知开关失败：%x", hr)
	}
	if setting == 0 {
		return Notification{Allowed: true, Status: "allowed", Description: "系统已允许GoodChat发送通知；专注模式仍可能暂时静默"}, nil
	}
	return notificationSetting(setting), nil
}

// notificationSetting 将系统枚举转换为用户可操作的说明。
func notificationSetting(setting uint32) Notification {
	message := map[uint32]string{1: "已在系统中关闭GoodChat通知", 2: "已关闭当前Windows用户的系统通知", 3: "通知已被管理员组策略禁用", 4: "系统尚未允许此应用发送通知"}[setting]
	if message == "" {
		message = "当前系统通知状态无法确认"
	}
	return Notification{Status: "not_allowed", Description: message}
}
