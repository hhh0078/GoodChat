// 文件作用说明：每次选择服务器后核对原生程序版本，未完成检查或必须更新时禁止业务登录和消息处理。
package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
)

// Update 返回当前进程版本和目标平台下载，安装后必须重启程序才会改变当前版本。
type Update struct {
	Current  string `json:"current_version"`
	Required bool   `json:"required"`
	Download string `json:"download_url"`
	Release  *struct {
		Version     string `json:"version"`
		Description string `json:"description"`
	} `json:"release"`
}

// newer 比较三段数字版本，拒绝畸形的服务器响应。
func newer(next, current string) (bool, error) {
	var a, b [3]int
	for index, value := range []string{next, current} {
		parts := strings.Split(value, ".")
		if len(parts) != 3 {
			return false, errors.New("版本检查返回了无效版本")
		}
		for i, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || n > 65535 {
				return false, errors.New("版本检查返回了无效版本")
			}
			if index == 0 {
				a[i] = n
			} else {
				b[i] = n
			}
		}
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i], nil
		}
	}
	return false, nil
}

// CheckUpdate 从选定服务器读取最新版本，不使用离线缓存绕过强制更新。
func (c *Client) CheckUpdate() (Update, error) {
	s, err := c.current()
	if err != nil {
		return Update{}, err
	}
	target := "mac"
	if runtime.GOOS == "windows" {
		target = "win"
	}
	result, err := c.request(s, http.MethodGet, "/api/v1/public/client-releases/latest?platform="+target+"&current_version="+url.QueryEscape(c.version), "", nil, "")
	if err != nil {
		return Update{}, errors.New("无法检查最新版本，请检查服务器地址或网络后重试")
	}
	if result.Status != 200 {
		return Update{}, errors.New("版本检查未完成，请重试或联系管理员")
	}
	var envelope struct {
		Data *Update `json:"data"`
	}
	if json.Unmarshal(result.Body, &envelope) != nil || envelope.Data == nil {
		return Update{}, errors.New("服务器不支持客户端版本检查，请联系管理员")
	}
	value := *envelope.Data
	value.Current = c.version
	if value.Release != nil {
		above, err := newer(value.Release.Version, c.version)
		if err != nil {
			return Update{}, err
		}
		threshold, err := newer(value.Release.Version, "0.0.1")
		if err != nil {
			return Update{}, err
		}
		value.Required = above && threshold
		if value.Required {
			download, err := url.Parse(value.Download)
			if err != nil || download.Scheme != "https" || download.Host == "" || download.User != nil {
				return Update{}, errors.New("管理员尚未配置有效的更新下载地址")
			}
		}
	} else {
		value.Required = false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return Update{}, errors.New("服务器已切换，请重新检查版本")
	}
	s.updateChecked = true
	s.updateRequired = value.Required
	return value, nil
}

// compatible 在原生边界执行更新门禁，不能仅隐藏页面按钮。
func compatible(s *session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.updateChecked {
		return errors.New("请先完成最新版本检查")
	}
	if s.updateRequired {
		return errors.New("当前版本需要强制更新，请安装新版后重新打开程序")
	}
	return nil
}
