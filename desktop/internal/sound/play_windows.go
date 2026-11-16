// 文件作用说明：使用Windows系统API异步播放固定本机提示音，不创建控制台或PowerShell。
package sound

import (
	"errors"
	"golang.org/x/sys/windows"
	"unsafe"
)

var playSound = windows.NewLazySystemDLL("winmm.dll").NewProc("PlaySoundW")

// playFile 通过winmm播放已按成员偏好缩放的PCM，不修改系统全局音量。
func playFile(path string) error {
	value, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, _ := playSound.Call(uintptr(unsafe.Pointer(value)), 0, 0x00020003)
	if result == 0 {
		return errors.New("系统提示音播放失败")
	}
	return nil
}
