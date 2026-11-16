// 文件作用说明：提供Mac与Windows共用的GoodChat桌面入口、受限桥接、托盘与系统通知。
package main

import (
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"supportdesk/desktop/internal/client"
	"supportdesk/desktop/internal/sound"
)

// version 和 defaultServer 由打包脚本通过链接参数注入，不保存任何密钥。
var version = "0.1.1"
var defaultServer = "https://goodchat.58it.cn"

//go:embed all:frontend/dist
var assets embed.FS

//go:embed icon.png
var icon []byte

// Desktop 只向随包界面开放明确的桌面操作，不暴露任意文件或命令执行。
type Desktop struct {
	client        *client.Client
	app           *application.App
	window        *application.WebviewWindow
	notifications *notifications.NotificationService
}

// Startup 读取版本、打包默认服务器和本机记住的服务器。
func (d *Desktop) Startup() (client.Startup, error) { return d.client.Startup() }

// SelectServer 确认服务器并隔离旧服务器的任务与凭证。
func (d *Desktop) SelectServer(server string) error { return d.client.SelectServer(server) }

// CheckUpdate 在原生层执行启动版本检查，避免页面绕过强制更新。
func (d *Desktop) CheckUpdate() (client.Update, error) { return d.client.CheckUpdate() }

// SetIdentity 核验并切换客服工作空间。
func (d *Desktop) SetIdentity(tenant string) error { return d.client.SetIdentity(tenant) }

// Status 返回原生模块的连接状态和待发数量。
func (d *Desktop) Status() client.State { return d.client.Status() }

// Pending 返回当前身份未确认的持久消息。
func (d *Desktop) Pending() ([]client.Pending, error) { return d.client.Pending() }

// Retry 重试原消息编号，不生成重复消息。
func (d *Desktop) Retry(id string) error { return d.client.Retry(id) }

// Reconnect 立即检查并重连原生消息连接。
func (d *Desktop) Reconnect() { d.client.Reconnect() }

// SendFrame 只转发输入状态，聊天正文必须持久排队。
func (d *Desktop) SendFrame(conversation string, typing bool) bool {
	return d.client.SendFrame(conversation, typing)
}

// EnableNotifications 在用户主动操作时申请操作系统通知权限。
func (d *Desktop) EnableNotifications() (bool, error) {
	return d.notifications.RequestNotificationAuthorization()
}

// OpenExternal 仅在系统浏览器打开HTTP或HTTPS链接，不执行本地协议。
func (d *Desktop) OpenExternal(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("链接地址无效")
	}
	return d.app.Browser.OpenURL(u.String())
}

// main 创建系统WebView与后台消息模块，关闭窗口默认隐藏到托盘。
func main() {
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal("无法读取本机配置目录")
	}
	d := &Desktop{notifications: notifications.New()}
	native, err := client.New(filepath.Join(dir, "GoodChat"), version, defaultServer, nil, func(name string, value any) {
		if d.app != nil {
			d.app.Event.Emit(name, value)
		}
	})
	if err != nil {
		log.Fatal("初始化桌面数据失败：", err)
	}
	d.client = native
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name: "GoodChat", Description: "GoodChat 客服工作台", Icon: icon,
		Services:   []application.Service{application.NewService(d), application.NewService(d.notifications)},
		Assets:     application.AssetOptions{Handler: application.AssetFileServerFS(frontend), Middleware: native.Middleware},
		OnShutdown: native.Close,
		Mac:        application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "cn.58it.goodchat.desktop", OnSecondInstanceLaunch: func(application.SecondInstanceData) {
			if d.window != nil {
				d.show()
			}
		}},
	})
	d.app = app
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "GoodChat · 客服工作台", Name: "main", Width: 1360, Height: 880, MinWidth: 980, MinHeight: 680,
		URL: "/", BackgroundColour: application.NewRGB(244, 247, 252), DevToolsEnabled: false,
		Permissions: map[application.PermissionType]application.Permission{application.PermissionMicrophone: application.PermissionDefault},
	})
	d.window = window
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) { native.SetBackground(true); window.Hide(); e.Cancel() })
	for _, event := range []events.WindowEventType{events.Common.WindowLostFocus, events.Common.WindowMinimise, events.Common.WindowHide} {
		window.OnWindowEvent(event, func(*application.WindowEvent) { native.SetBackground(true) })
	}
	window.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) { native.SetBackground(false) })
	tray := app.SystemTray.New().SetIcon(icon)
	tray.SetTooltip("GoodChat 客服工作台")
	if runtime.GOOS == "darwin" {
		tray.SetLabel("GoodChat")
	}
	menu := app.NewMenu()
	menu.Add("打开工作台").OnClick(func(*application.Context) { d.show() })
	menu.Add("切换服务器").OnClick(func(*application.Context) { d.show(); app.Event.Emit("desktop:select-server", nil) })
	menu.Add("重新连接消息服务").OnClick(func(*application.Context) { native.Reconnect() })
	menu.AddSeparator()
	menu.Add("退出 GoodChat").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	player := sound.New(filepath.Join(dir, "GoodChat"), frontend)
	native.SetNotifier(func() bool {
		allowed, err := d.notifications.CheckNotificationAuthorization()
		return err == nil && allowed
	}, func(body string, system bool, soundID string, volume float64) error {
		var audioError error
		if volume > 0 {
			audioError = player.Play(soundID, volume)
		}
		if system {
			if err := d.notifications.SendNotification(notifications.NotificationOptions{ID: "goodchat-attention", Title: "GoodChat 接待提醒", Body: body, Sound: &notifications.NotificationSound{Silent: true}}); err != nil {
				return err
			}
		}
		return audioError
	})
	d.notifications.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error == nil {
			d.show()
		}
	})
	if err = app.Run(); err != nil && !strings.Contains(err.Error(), "already running") {
		log.Fatal("桌面程序启动失败：", err)
	}
}

// show 恢复工作台并聚焦窗口，后台消息任务不会随窗口重建。
func (d *Desktop) show() { d.client.SetBackground(false); d.window.Show(); d.window.Focus() }
