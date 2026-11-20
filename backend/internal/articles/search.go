// 文件作用说明：向百度和IndexNow提交公开文章，向Google提交站点地图；凭据只发送给固定官方端点。
package articles

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// articleURL 构造已隔离语言的公开文章规范地址。
func (s *Service) articleURL(a Article) string {
	return strings.TrimRight(s.cfg.PublicURL, "/") + "/" + a.Language + "/articles/" + a.Slug
}

// enqueueSearch 为启用的平台建立持久任务，文章修订幂等，Google每天最多自动提交一次目录。
func (s *Service) enqueueSearch(ctx context.Context, tx pgx.Tx, a Article) error {
	c, e := readSettings(tx.QueryRow(ctx, "SELECT to_jsonb(c) FROM site_article_settings c WHERE id"))
	if e != nil {
		return e
	}
	for p, on := range map[string]bool{"baidu": c.Baidu, "indexnow": c.IndexNow, "google": c.Google} {
		if !on {
			continue
		}
		key := fmt.Sprintf("search:%s:%s:%d", p, a.ID, a.Version)
		if p == "google" {
			key = "sitemap:" + s.now().UTC().Format("2006-01-02")
		}
		if e = enqueue(ctx, tx, "submit", a.ID, p, key, "", map[string]bool{}); e != nil {
			return e
		}
	}
	return nil
}

// searchRequest 限制响应体并抹去含令牌的网络错误，不接受重定向或任意站点地址。
func (s *Service) searchRequest(ctx context.Context, method, address, contentType string, body []byte, bearer string) (int, []byte, error) {
	req, e := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if e != nil {
		return 0, nil, taskFail("search_request_failed", "搜索平台请求无效")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, e := s.client.Do(req)
	if e != nil {
		return 0, nil, taskFail("search_request_failed", "搜索平台连接失败，请检查网络后重试")
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 65537))
	if e != nil || len(raw) > 65536 {
		return res.StatusCode, nil, taskFail("search_response_failed", "搜索平台响应异常")
	}
	return res.StatusCode, raw, nil
}

// parseGoogleCredentials 校验服务账号RSA密钥，不信任凭据文件提供的外部token地址。
func parseGoogleCredentials(value string) (string, *rsa.PrivateKey, error) {
	var c struct {
		Type  string `json:"type"`
		Email string `json:"client_email"`
		Key   string `json:"private_key"`
	}
	if e := json.Unmarshal([]byte(value), &c); e != nil || c.Type != "service_account" || !strings.Contains(c.Email, "@") {
		return "", nil, fmt.Errorf("invalid service account")
	}
	block, _ := pem.Decode([]byte(c.Key))
	if block == nil {
		return "", nil, fmt.Errorf("invalid private key")
	}
	parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	var key *rsa.PrivateKey
	if e == nil {
		key, _ = parsed.(*rsa.PrivateKey)
	} else {
		key, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if key == nil || key.N.BitLen() < 2048 {
		return "", nil, fmt.Errorf("invalid RSA key")
	}
	return c.Email, key, nil
}

// googleToken 仅向Google官方令牌服务交换一小时的站点管理访问令牌。
func (s *Service) googleToken(ctx context.Context, value string) (string, error) {
	email, key, e := parseGoogleCredentials(value)
	if e != nil {
		return "", taskFail("google_credentials_invalid", "Google服务账号凭据无效")
	}
	now := s.now()
	claims, _ := json.Marshal(map[string]any{"iss": email, "scope": "https://www.googleapis.com/auth/webmasters", "aud": "https://oauth2.googleapis.com/token", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	part := head + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(part))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if e != nil {
		return "", e
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {part + "." + base64.RawURLEncoding.EncodeToString(sig)}}
	status, raw, e := s.searchRequest(ctx, "POST", "https://oauth2.googleapis.com/token", "application/x-www-form-urlencoded", []byte(form.Encode()), "")
	if e != nil {
		return "", e
	}
	var out struct {
		Token string `json:"access_token"`
	}
	if status != 200 || json.Unmarshal(raw, &out) != nil || out.Token == "" {
		return "", taskFail("google_auth_failed", "Google授权失败，请检查服务账号及站点权限")
	}
	return out.Token, nil
}

// submitSearch 提交真实公开地址；成功代表平台接收请求，并不承诺搜索引擎收录。
func (s *Service) submitSearch(ctx context.Context, j job) (map[string]any, error) {
	c, e := readSettings(s.pool.QueryRow(ctx, "SELECT to_jsonb(c) FROM site_article_settings c WHERE id"))
	if e != nil {
		return nil, e
	}
	on := map[string]bool{"baidu": c.Baidu, "indexnow": c.IndexNow, "google": c.Google}[j.Provider]
	if !on {
		return nil, taskFail("article_schedule_disabled", "搜索平台已停用")
	}
	urls := []string{}
	if j.Provider != "google" {
		items, e := s.queryArticles(ctx, "SELECT "+listProjection+" FROM site_articles a WHERE status='published' AND ($1='' OR id=NULLIF($1,'')::uuid)", j.ArticleID)
		if e != nil {
			return nil, e
		}
		for _, a := range items {
			urls = append(urls, s.articleURL(a))
		}
	}
	submitted, status := 0, 0
	if j.Provider == "google" {
		value, e := s.secret(c.GoogleCipher, "google", true)
		if e != nil {
			return nil, e
		}
		token, e := s.googleToken(ctx, value)
		if e != nil {
			return nil, e
		}
		property, e := s.googleProperty(c.GoogleSite)
		if e != nil {
			return nil, e
		}
		endpoint := "https://www.googleapis.com/webmasters/v3/sites/" + url.PathEscape(property) + "/sitemaps/" + url.PathEscape(strings.TrimRight(s.cfg.PublicURL, "/")+"/sitemap.xml")
		status, _, e = s.searchRequest(ctx, "PUT", endpoint, "", nil, token)
		if e != nil {
			return nil, e
		}
		if status < 200 || status >= 300 {
			return nil, taskFail("google_submit_failed", "Google站点地图提交失败，请检查站点授权")
		}
		submitted = 1
	} else {
		purpose, cipherText := "baidu", c.BaiduCipher
		if j.Provider == "indexnow" {
			purpose, cipherText = "indexnow", c.IndexCipher
		}
		key, e := s.secret(cipherText, purpose, true)
		if e != nil {
			return nil, e
		}
		for start := 0; start < len(urls); start += 1000 {
			end := start + 1000
			if end > len(urls) {
				end = len(urls)
			}
			chunk := urls[start:end]
			var raw []byte
			if j.Provider == "baidu" {
				q := url.Values{"site": {strings.TrimRight(s.cfg.PublicURL, "/")}, "token": {key}}
				// 百度资源平台公布的普通收录接口为HTTP，该主机不提供可用的HTTPS接口。
				status, raw, e = s.searchRequest(ctx, "POST", "http://data.zz.baidu.com/urls?"+q.Encode(), "text/plain", []byte(strings.Join(chunk, "\n")), "")
				if e != nil {
					return nil, e
				}
				var result struct {
					Success int `json:"success"`
					Error   int `json:"error"`
				}
				if status != 200 || json.Unmarshal(raw, &result) != nil || result.Error != 0 || result.Success != len(chunk) {
					return nil, taskFail("baidu_submit_failed", "百度未接收全部链接，请检查站点和推送配额")
				}
			} else {
				base, _ := url.Parse(s.cfg.PublicURL)
				body, _ := json.Marshal(map[string]any{"host": base.Hostname(), "key": key, "keyLocation": strings.TrimRight(s.cfg.PublicURL, "/") + "/indexnow-key.txt", "urlList": chunk})
				status, _, e = s.searchRequest(ctx, "POST", "https://api.indexnow.org/indexnow", "application/json", body, "")
				if e != nil {
					return nil, e
				}
				if status != 200 && status != 202 {
					return nil, taskFail("indexnow_submit_failed", "IndexNow提交失败，请检查所有权验证键")
				}
			}
			submitted += len(chunk)
		}
	}
	return map[string]any{"provider": j.Provider, "submitted": submitted, "http_status": status, "message": "submitted_not_indexed"}, nil
}
