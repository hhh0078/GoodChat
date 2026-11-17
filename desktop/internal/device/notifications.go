// 文件作用说明：定义系统通知状态与固定设置入口，禁止前端传入任意系统协议地址。
package device

import "runtime"

// Notification 表示操作系统报告的能力，不代表成员账号已开启业务提醒。
type Notification struct {
	Allowed     bool   `json:"allowed"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

// NotificationSettingsURL 只返回目标操作系统的固定通知设置页。
func NotificationSettingsURL() string {
	if runtime.GOOS == "windows" {
		return "ms-settings:notifications"
	}
	return "x-apple.systempreferences:com.apple.preference.notifications"
}
