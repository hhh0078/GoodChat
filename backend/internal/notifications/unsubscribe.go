// 文件作用说明：提供仅能取消摘要或产品更新的签名退订，支持邮件客户端一键POST且不能关闭安全通知。
package notifications

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// unsubscribeClaims 不包含邮箱、密码或登录权限，只表示一种非安全邮件订阅。
type unsubscribeClaims struct {
	User     string `json:"u"`
	Category string `json:"c"`
	Expires  int64  `json:"e"`
}

// signingKey 复用本站已配置认证密钥，派生用途隔离签名，不生成可登录凭证。
func (s *Service) signingKey() ([]byte, error) {
	secret := []byte(s.cfg.AuthSecret)
	if len(secret) < 32 && s.cfg.DataDir != "" {
		secret, _ = os.ReadFile(filepath.Join(s.cfg.DataDir, "auth-secret"))
	}
	if len(secret) < 32 {
		return nil, fmt.Errorf("退订签名配置不可用")
	}
	key := sha256.Sum256(append([]byte("goodchat-unsubscribe-v1:"), secret...))
	return key[:], nil
}

// unsubscribeToken 生成一年内有效的用途限定签名，不授予账号或租户访问权限。
func (s *Service) unsubscribeToken(user, category string) (string, error) {
	if category != "digest" && category != "updates" {
		return "", fmt.Errorf("退订类别无效")
	}
	key, e := s.signingKey()
	if e != nil {
		return "", e
	}
	raw, _ := json.Marshal(unsubscribeClaims{user, category, time.Now().AddDate(1, 0, 0).Unix()})
	payload := base64.RawURLEncoding.EncodeToString(raw)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}

// verifyUnsubscribe 校验签名、用途和期限，不接受修改账号或安全邮件订阅的请求。
func (s *Service) verifyUnsubscribe(token string) (unsubscribeClaims, error) {
	var claims unsubscribeClaims
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(token) > 1500 {
		return claims, fmt.Errorf("退订链接无效或已过期")
	}
	key, e := s.signingKey()
	if e != nil {
		return claims, e
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return claims, fmt.Errorf("退订链接无效或已过期")
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte(parts[0]))
	if !hmac.Equal(signature, h.Sum(nil)) {
		return claims, fmt.Errorf("退订链接无效或已过期")
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil || json.Unmarshal(raw, &claims) != nil || claims.User == "" || claims.Expires < time.Now().Unix() || (claims.Category != "digest" && claims.Category != "updates") {
		return unsubscribeClaims{}, fmt.Errorf("退订链接无效或已过期")
	}
	return claims, nil
}

// unsubscribeURL 只构造本站退订地址，正文使用确认页，邮件头使用无需登录的一键POST入口。
func (s *Service) unsubscribeURL(user, category, path, locale, zone string) (string, error) {
	token, e := s.unsubscribeToken(user, category)
	if e != nil {
		return "", e
	}
	u, e := url.Parse(s.cfg.PublicURL)
	if e != nil {
		return "", e
	}
	u.Path = path
	u.RawQuery = url.Values{"token": {token}, "lang": {locale}, "timezone": {zone}}.Encode()
	u.Fragment = ""
	return u.String(), nil
}

// unsubscribe GET仅验证显示确认，不因邮件安全扫描访问就退订；POST才执行幂等取消。
func (s *Service) unsubscribe(w http.ResponseWriter, r *http.Request) error {
	claims, e := s.verifyUnsubscribe(r.URL.Query().Get("token"))
	if e != nil {
		return platform.Fail(400, "invalid_unsubscribe", e.Error())
	}
	if r.Method == http.MethodPost {
		e = s.systemTx(r.Context(), func(tx pgx.Tx) error {
			column := "digest_enabled"
			if claims.Category == "updates" {
				column = "updates_enabled"
			}
			_, e := tx.Exec(r.Context(), "UPDATE mail_notification_preferences SET "+column+"=false,updated_at=now() WHERE user_id=$1", claims.User)
			return e
		})
		if e != nil {
			return e
		}
	}
	platform.JSON(w, 200, map[string]any{"category": claims.Category, "unsubscribed": r.Method == http.MethodPost})
	return nil
}
