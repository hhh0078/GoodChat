// 文件作用说明：保存文章计划及搜索平台设置，所有私密凭据按用途加密且读取接口只返回已配置状态。
package articles

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

type settings struct {
	AIEnabled    bool       `json:"ai_enabled"`
	Prompt       string     `json:"ai_prompt"`
	Source       string     `json:"source_language"`
	Targets      []string   `json:"target_languages"`
	Clock        string     `json:"daily_time"`
	Timezone     string     `json:"timezone"`
	NextRun      *time.Time `json:"next_run_at"`
	Baidu        bool       `json:"baidu_enabled"`
	BaiduCipher  string     `json:"baidu_token_cipher"`
	IndexNow     bool       `json:"indexnow_enabled"`
	IndexCipher  string     `json:"indexnow_key_cipher"`
	Google       bool       `json:"google_enabled"`
	GoogleSite   string     `json:"google_site"`
	GoogleCipher string     `json:"google_credentials_cipher"`
	Version      int64      `json:"version"`
}
type settingsInput struct {
	Section           string   `json:"section"`
	Version           int64    `json:"version"`
	AIEnabled         bool     `json:"ai_enabled"`
	Prompt            string   `json:"ai_prompt"`
	Source            string   `json:"source_language"`
	Targets           []string `json:"target_languages"`
	Clock             string   `json:"daily_time"`
	Timezone          string   `json:"timezone"`
	Baidu             bool     `json:"baidu_enabled"`
	BaiduToken        string   `json:"baidu_token"`
	IndexNow          bool     `json:"indexnow_enabled"`
	IndexKey          string   `json:"indexnow_key"`
	Google            bool     `json:"google_enabled"`
	GoogleSite        string   `json:"google_site"`
	GoogleCredentials string   `json:"google_credentials"`
}

var indexKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{8,128}$`)

// readSettings 从唯一设置行读取运行配置，调用方决定是否在事务中加锁。
func readSettings(row pgx.Row) (settings, error) {
	var raw []byte
	var value settings
	err := row.Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	return value, err
}

// secret 为每种搜索平台凭据绑定不同附加认证数据，不接受来源不明或被挪用的密文。
func (s *Service) secret(value, purpose string, decrypt bool) (string, error) {
	if len(s.cfg.AuthSecret) < 32 {
		return "", platform.Fail(503, "article_secret_unavailable", "请先配置服务端安全密钥")
	}
	key := sha256.Sum256([]byte(s.cfg.AuthSecret + "\x00goodchat-article-search-v1"))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if decrypt {
		raw, e := base64.RawStdEncoding.DecodeString(value)
		if e != nil || len(raw) < a.NonceSize() {
			return "", platform.Fail(503, "article_secret_invalid", "搜索平台凭据不可用")
		}
		plain, e := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(purpose))
		return string(plain), e
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(a.Seal(nonce, nonce, []byte(value), []byte(purpose))), nil
}

// nextDaily 使用显式时区计算严格晚于当前时刻的下一次日程，保存设置不会补发过期文章。
func nextDaily(now time.Time, clock, timezone string) (time.Time, error) {
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, platform.Fail(400, "invalid_timezone", "请选择有效时区")
	}
	parsed, err := time.Parse("15:04", clock)
	if err != nil || len(clock) != 5 {
		return time.Time{}, platform.Fail(400, "invalid_schedule", "请填写有效的每日发布时间")
	}
	local := now.In(zone)
	next := time.Date(local.Year(), local.Month(), local.Day(), parsed.Hour(), parsed.Minute(), 0, 0, zone)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

// currentModel 每次执行前重新取得平台仍启用的模型，关闭模型后新任务不能绕过开关。
func (s *Service) currentModel(ctx context.Context) (platform.TextModel, error) {
	if s.Model != nil {
		return s.Model(ctx)
	}
	if s.cfg.AIEndpoint == "" || s.cfg.AIKey == "" || s.cfg.AIModel == "" {
		return platform.TextModel{}, platform.Fail(503, "article_ai_unavailable", "平台尚未配置可用文字模型")
	}
	return platform.TextModel{BaseURL: s.cfg.AIEndpoint, APIKey: s.cfg.AIKey, Model: s.cfg.AIModel, MaxInput: 32000, MaxOutput: 8192}, nil
}

// getSettings 只输出配置状态及公开计划，不回显密文、令牌或Google私钥。
func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) {
	value, err := readSettings(s.pool.QueryRow(r.Context(), "SELECT to_jsonb(c) FROM site_article_settings c WHERE id"))
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	model, e := s.currentModel(r.Context())
	var count int
	err = s.pool.QueryRow(r.Context(), "SELECT count(*) FROM site_articles").Scan(&count)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	respond(w, r, map[string]any{"ai_enabled": value.AIEnabled, "ai_prompt": value.Prompt, "source_language": value.Source, "target_languages": value.Targets, "daily_time": value.Clock, "timezone": value.Timezone, "next_run_at": value.NextRun, "version": value.Version, "ai_configured": e == nil && model.APIKey != "", "model": model.Model, "title_count": count, "baidu_enabled": value.Baidu, "baidu_token_configured": value.BaiduCipher != "", "indexnow_enabled": value.IndexNow, "indexnow_key_configured": value.IndexCipher != "", "indexnow_key_location": strings.TrimRight(s.cfg.PublicURL, "/") + "/indexnow-key.txt", "google_enabled": value.Google, "google_credentials_configured": value.GoogleCipher != "", "google_site": value.GoogleSite, "sitemap_url": strings.TrimRight(s.cfg.PublicURL, "/") + "/sitemap.xml"}, nil)
}

// googleProperty 限制Search Console资源为当前官网，不能借平台凭据向无关网站提交。
func (s *Service) googleProperty(value string) (string, error) {
	base, err := url.Parse(s.cfg.PublicURL)
	if err != nil {
		return "", err
	}
	if value == "" {
		return strings.TrimRight(s.cfg.PublicURL, "/") + "/", nil
	}
	if strings.HasPrefix(value, "sc-domain:") {
		host := strings.TrimPrefix(value, "sc-domain:")
		if host != "" && (base.Hostname() == host || strings.HasSuffix(base.Hostname(), "."+host)) {
			return value, nil
		}
	}
	parsed, e := url.Parse(value)
	if e == nil && parsed.Scheme == base.Scheme && parsed.Host == base.Host && (parsed.Path == "" || parsed.Path == "/") && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" {
		return strings.TrimRight(value, "/") + "/", nil
	}
	return "", platform.Fail(400, "invalid_search_site", "搜索平台站点必须对应当前官网")
}

// saveSettings 分区保存AI计划或搜索凭据，保留未填写密钥并用版本防止两个抽屉互相覆盖。
func (s *Service) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsInput
	if err := platform.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err)
		return
	}
	err := pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		old, e := readSettings(tx.QueryRow(r.Context(), "SELECT to_jsonb(c) FROM site_article_settings c WHERE id FOR UPDATE"))
		if e != nil {
			return e
		}
		if old.Version != in.Version {
			return platform.Fail(409, "version_conflict", "设置已被修改，请刷新后重试")
		}
		if in.Section == "ai" {
			if _, e = languages([]string{in.Source}); e != nil {
				return e
			}
			targets, e := languages(in.Targets)
			if e != nil {
				return e
			}
			if len(in.Prompt) > 16000 {
				return platform.Fail(400, "invalid_prompt", "文章提示词过长")
			}
			next, e := nextDaily(s.now(), in.Clock, in.Timezone)
			if e != nil {
				return e
			}
			var due any
			if in.AIEnabled {
				if strings.TrimSpace(in.Prompt) == "" {
					return platform.Fail(400, "invalid_prompt", "请填写AI文章提示词")
				}
				if _, e = s.currentModel(r.Context()); e != nil {
					return platform.Fail(503, "article_ai_unavailable", "平台尚未配置可用文字模型")
				}
				due = next
			}
			_, e = tx.Exec(r.Context(), `UPDATE site_article_settings SET ai_enabled=$1,ai_prompt=$2,source_language=$3,target_languages=$4,daily_time=$5,timezone=$6,next_run_at=$7,version=version+1,updated_at=now(),updated_by=NULLIF($8,'')::uuid WHERE id`, in.AIEnabled, strings.TrimSpace(in.Prompt), in.Source, targets, in.Clock, in.Timezone, due, platform.Current(r).UserID)
			return e
		}
		if in.Section != "search" {
			return platform.Fail(400, "invalid_settings", "设置类型无效")
		}
		if len(in.BaiduToken) > 256 || strings.ContainsAny(in.BaiduToken, "\r\n") || len(in.GoogleCredentials) > 65536 {
			return platform.Fail(400, "invalid_search_credentials", "请检查搜索平台凭据")
		}
		if in.BaiduToken != "" {
			old.BaiduCipher, e = s.secret(in.BaiduToken, "baidu", false)
			if e != nil {
				return e
			}
		}
		if in.IndexKey != "" {
			if !indexKeyPattern.MatchString(in.IndexKey) {
				return platform.Fail(400, "invalid_indexnow_key", "IndexNow验证键格式无效")
			}
			old.IndexCipher, e = s.secret(in.IndexKey, "indexnow", false)
			if e != nil {
				return e
			}
		}
		if in.GoogleCredentials != "" {
			if _, _, e = parseGoogleCredentials(in.GoogleCredentials); e != nil {
				return platform.Fail(400, "invalid_google_credentials", "Google服务账号凭据格式无效")
			}
			old.GoogleCipher, e = s.secret(in.GoogleCredentials, "google", false)
			if e != nil {
				return e
			}
		}
		if in.Baidu && old.BaiduCipher == "" || in.IndexNow && old.IndexCipher == "" || in.Google && old.GoogleCipher == "" {
			return platform.Fail(400, "missing_search_credentials", "请先填写需要启用的搜索平台凭据")
		}
		property, e := s.googleProperty(in.GoogleSite)
		if e != nil {
			return e
		}
		_, e = tx.Exec(r.Context(), `UPDATE site_article_settings SET baidu_enabled=$1,baidu_token_cipher=$2,indexnow_enabled=$3,indexnow_key_cipher=$4,google_enabled=$5,google_site=$6,google_credentials_cipher=$7,version=version+1,updated_at=now(),updated_by=NULLIF($8,'')::uuid WHERE id`, in.Baidu, old.BaiduCipher, in.IndexNow, old.IndexCipher, in.Google, property, old.GoogleCipher, platform.Current(r).UserID)
		return e
	})
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	s.getSettings(w, r)
}

// indexNowKey 仅公开IndexNow所有权键，不包含后台账号、其他搜索凭据或任何租户数据。
func (s *Service) indexNowKey(w http.ResponseWriter, r *http.Request) {
	value, err := readSettings(s.pool.QueryRow(r.Context(), "SELECT to_jsonb(c) FROM site_article_settings c WHERE id"))
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	if !value.IndexNow || value.IndexCipher == "" {
		http.NotFound(w, r)
		return
	}
	key, err := s.secret(value.IndexCipher, "indexnow", true)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, r, map[string]string{"key": key}, nil)
}
