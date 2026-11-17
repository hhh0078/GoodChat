// Package device 提供本机电源和通知能力，不修改服务器或系统永久设置。
package device

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Power 保存用户的常亮选择和当前进程持有的电源请求。
type Power struct {
	mu        sync.Mutex
	dir       string
	start     func() (func() error, error)
	release   func() error
	lastError string
	closed    bool
}

// NewPower 恢复本机选择；可选功能恢复失败时不阻塞聊天启动。
func NewPower(dir string) *Power { return newPower(dir, startPower) }

// newPower 将系统调用作为依赖，便于验证持久化失败和资源释放。
func newPower(dir string, start func() (func() error, error)) *Power {
	p := &Power{dir: dir, start: start}
	raw, err := os.ReadFile(filepath.Join(dir, "device-settings.json"))
	if os.IsNotExist(err) {
		return p
	}
	var prefs struct {
		KeepAwake bool `json:"keep_awake"`
	}
	if err != nil || json.Unmarshal(raw, &prefs) != nil {
		p.lastError = "无法读取本机常亮设置，请重新设置"
		return p
	}
	if prefs.KeepAwake {
		p.release, err = p.start()
		if err != nil {
			p.lastError = "无法恢复常亮状态：" + err.Error()
		}
	}
	return p
}

// State 返回实际持有状态，不把保存的偏好冒充当前已生效。
func (p *Power) State() (bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.release != nil, p.lastError
}

// Set 仅在系统请求与本机保存都成功时确认开启，重复调用不会叠加请求。
func (p *Power) Set(enabled bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("程序正在退出")
	}
	if enabled == (p.release != nil) && p.lastError == "" {
		return nil
	}
	var release func() error
	if enabled && p.release == nil {
		var err error
		release, err = p.start()
		if err != nil {
			return err
		}
	}
	if err := p.save(enabled); err != nil {
		if release != nil {
			_ = release()
		}
		return errors.New("本机设置保存失败，原状态未改变")
	}
	if enabled {
		if release != nil {
			p.release = release
		}
	} else if p.release != nil {
		err := p.release()
		p.release = nil
		if err != nil {
			p.lastError = "电源请求释放失败：" + err.Error()
			return err
		}
	}
	p.lastError = ""
	return nil
}

// save 以临时文件替换本机偏好，不与服务器选择或系统凭证共用文件。
func (p *Power) save(enabled bool) error {
	if err := os.MkdirAll(p.dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(p.dir, ".device-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	raw, _ := json.Marshal(map[string]bool{"keep_awake": enabled})
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceFile(f.Name(), filepath.Join(p.dir, "device-settings.json"))
}

// Close 退出时释放常亮请求，保留用户选择供下次启动恢复。
func (p *Power) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.release == nil {
		return nil
	}
	err := p.release()
	p.release = nil
	return err
}
