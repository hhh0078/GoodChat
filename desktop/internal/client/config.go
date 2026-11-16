// Package client 管理桌面服务器配置、原生网络连接与持久消息队列。
package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config 保存非敏感的服务器选择，认证信息只进入系统凭证库。
type Config struct {
	Server string `json:"server"`
}

// Startup 返回安装包元数据与本机选择，每次启动都由用户确认服务器。
type Startup struct {
	Version       string `json:"version"`
	DefaultServer string `json:"default_server"`
	Server        string `json:"server"`
}

// NormalizeServer 只允许根地址和安全传输，本机调试可使用HTTP。
func NormalizeServer(value string) (string, error) {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(value, "\r\n\t\\") {
		return "", errors.New("请输入完整服务器地址，例如 https://goodchat.58it.cn")
	}
	if u.Path != "" && u.Path != "/" || u.RawPath != "" {
		return "", errors.New("服务器地址只填写协议、域名和端口，不包含页面路径")
	}
	ip := net.ParseIP(u.Hostname())
	local := strings.EqualFold(u.Hostname(), "localhost") || ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", errors.New("服务器必须使用HTTPS；HTTP仅供本机调试使用")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = ""
	return u.String(), nil
}

// scopeHash 将服务器、账号与租户组成不可混用的本地分区键。
func scopeHash(parts ...string) string {
	raw, _ := json.Marshal(parts)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// writeConfig 以临时文件替换保存服务器地址，避免断电写坏配置。
func writeConfig(dir, server string) error {
	raw, err := json.Marshal(Config{Server: server})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
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
	return replaceFile(f.Name(), filepath.Join(dir, "settings.json"))
}
