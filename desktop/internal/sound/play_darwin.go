// 文件作用说明：使用macOS系统音频工具播放固定本机提示音，不启动Shell。
package sound

import "os/exec"

// playFile 启动短提示音并回收子进程，播放故障不影响原生消息接收。
func playFile(path string) error {
	cmd := exec.Command("/usr/bin/afplay", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
