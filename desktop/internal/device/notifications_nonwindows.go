//go:build !windows

// 文件作用说明：在macOS复用Wails系统通知中心的真实授权查询。
package device

// CheckNotifications 不弹出申请窗口，仅查询当前系统授权。
func CheckNotifications(check func() (bool, error)) (Notification, error) {
	allowed, err := check()
	if err != nil {
		return Notification{Status: "unknown", Description: "暂时无法读取系统通知权限"}, err
	}
	if allowed {
		return Notification{Allowed: true, Status: "allowed", Description: "系统已允许GoodChat发送通知"}, nil
	}
	return Notification{Status: "not_allowed", Description: "尚未授权或已在系统中关闭，请申请权限或打开系统设置"}, nil
}
