// 文件作用说明：在本机隔离PostgreSQL模式验证文章HTTP管理、语言发布、后台任务和官方搜索协议。
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
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"supportdesk/backend/internal/platform"
	"testing"
	"time"
)

type articleAuth struct{}

// RequirePlatform 模拟已验证的平台角色边界，拒绝普通账号到达文章写入处理器。
func (articleAuth) RequirePlatform(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Platform") != "yes" {
			platform.ErrorJSON(w, r, platform.Fail(403, "forbidden", "无权限"))
			return
		}
		next(w, r)
	}
}

type articleFixture struct {
	s      *Service
	server *httptest.Server
	db     *pgxpool.Pool
}

// newArticleFixture 仅允许显式的本机专用测试库，为每项测试创建并回收随机模式。
func newArticleFixture(t *testing.T) *articleFixture {
	t.Helper()
	path := os.Getenv("GOODCHAT_TEST_CONFIG")
	if path == "" {
		t.Skip("显式指定隔离测试库后执行")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var c struct {
		Database  string `json:"database_url"`
		Migration string `json:"migration_url"`
	}
	if json.Unmarshal(raw, &c) != nil {
		t.Fatal("配置错误")
	}
	ctx := context.Background()
	cfg, e := pgxpool.ParseConfig(c.Migration)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "goodchat_saas_test_") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("只允许本机独立测试库")
	}
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	schema := "article_test_" + strings.ToLower(rand.Text()[:12])
	q := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+q); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+q+" CASCADE"); admin.Close() })
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(db.Close)
	if _, e = db.Exec(ctx, "CREATE TABLE users(id uuid PRIMARY KEY)"); e != nil {
		t.Fatal(e)
	}
	sql, e := os.ReadFile(filepath.Join("..", "database", "migrations", "045_site_articles.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, string(sql)); e != nil {
		t.Fatal(e)
	}
	s := New(db, articleAuth{}, platform.Config{PublicURL: "https://goodchat.example", AuthSecret: strings.Repeat("s", 40)})
	mux := http.NewServeMux()
	s.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &articleFixture{s, server, db}
}

// request 通过真实HTTP编解码调用接口，并保留状态供权限与冲突断言。
func (f *articleFixture) request(t *testing.T, method, path string, body any, admin bool) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, f.server.URL+"/api/v1/"+path, bytes.NewReader(raw))
	if admin {
		req.Header.Set("X-Test-Platform", "yes")
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var value map[string]any
	if e = json.NewDecoder(res.Body).Decode(&value); e != nil {
		t.Fatal(e)
	}
	if data, ok := value["data"].(map[string]any); ok {
		return res.StatusCode, data
	}
	return res.StatusCode, value
}

// createArticle 创建真实数据库草稿供发布及去重测试。
func (f *articleFixture) createArticle(t *testing.T, title, slug string) map[string]any {
	t.Helper()
	code, data := f.request(t, "POST", "platform/articles", map[string]any{"language": "zh-CN", "title": title, "slug": slug, "body_markdown": "## 流程\n\n**说明**\n\n[x](javascript:alert(1))"}, true)
	if code != 200 {
		t.Fatal(code, data)
	}
	return data["article"].(map[string]any)
}

// TestArticleHTTPVersions 验证权限、草稿不可见、并发冲突、明确翻译语言和公共版本隔离。
func TestArticleHTTPVersions(t *testing.T) {
	f := newArticleFixture(t)
	ctx := context.Background()
	if c, _ := f.request(t, "POST", "platform/articles", map[string]any{}, false); c != 403 {
		t.Fatal(c)
	}
	a := f.createArticle(t, "电商如何配置售后客服", "shop-support")
	id := a["id"].(string)
	if c, _ := f.request(t, "GET", "public/articles/shop-support?language=zh-CN", nil, false); c != 404 {
		t.Fatal(c)
	}
	if c, _ := f.request(t, "POST", "platform/articles", map[string]any{"language": "zh-CN", "title": "电商，如何配置售后客服？", "body_markdown": "hi"}, true); c != 409 {
		t.Fatal(c)
	}
	if c, _ := f.request(t, "POST", "platform/articles/"+id+"/publish", map[string]any{"version": 999}, true); c != 409 {
		t.Fatal(c)
	}
	c, data := f.request(t, "POST", "platform/articles/"+id+"/publish", map[string]any{"version": a["version"], "languages": []string{"en", "en"}}, true)
	if c != 200 {
		t.Fatal(c, data)
	}
	if c, _ = f.request(t, "GET", "public/articles/shop-support?language=en", nil, false); c != 404 {
		t.Fatal(c)
	}
	if c, data = f.request(t, "GET", "public/articles/shop-support?language=zh-CN", nil, false); c != 200 || strings.Contains(data["article"].(map[string]any)["body_html"].(string), "javascript:") {
		t.Fatal(c, data)
	}
	var count int
	_ = f.db.QueryRow(ctx, "SELECT count(*) FROM site_article_jobs WHERE kind='translate'").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	// 模型协议模拟只证明任务契约，不冒充真实模型验收。
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		draft := Draft{Language: "en", Title: "How to set up after-sales customer support", Slug: "shop-support", Markdown: "## Steps\n\nUse a knowledge base.", Keywords: []string{"support"}}
		raw, _ := json.Marshal(draft)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(raw)}}}})
	}))
	defer model.Close()
	f.s.Model = func(context.Context) (platform.TextModel, error) {
		return platform.TextModel{BaseURL: model.URL, Model: "contract", APIKey: "contract"}, nil
	}
	if worked, e := f.s.runOne(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	if c, data = f.request(t, "GET", "public/articles/shop-support?language=en", nil, false); c != 200 || len(data["alternates"].([]any)) != 2 {
		t.Fatal(c, data)
	}
	c, data = f.request(t, "GET", "public/articles?language=en", nil, false)
	if c != 200 || len(data["items"].([]any)) != 1 {
		t.Fatal(c, data)
	}
	c, data = f.request(t, "GET", "platform/articles/"+id, nil, true)
	current := data["article"].(map[string]any)
	edit := map[string]any{}
	for _, key := range []string{"title", "language", "summary", "keywords", "version", "body_markdown"} {
		edit[key] = current[key]
	}
	edit["slug"] = "new-url"
	if c, _ = f.request(t, "PATCH", "platform/articles/"+id, edit, true); c != 409 {
		t.Fatal(c)
	}
	if c, _ = f.request(t, "POST", "platform/articles/"+id+"/archive", map[string]any{"version": current["version"]}, true); c != 200 {
		t.Fatal(c)
	}
	_, data = f.request(t, "GET", "public/articles/sitemap", nil, false)
	if len(data["items"].([]any)) != 1 {
		t.Fatal(data)
	}
	if c, _ = f.request(t, "GET", "public/articles/shop-support?language=zh-CN", nil, false); c != 404 {
		t.Fatal(c)
	}
}

// TestArticlePagination 验证同时间多页无遗漏，语言或状态过滤不能由游标绕过。
func TestArticlePagination(t *testing.T) {
	f := newArticleFixture(t)
	for i := 0; i < 25; i++ {
		f.createArticle(t, fmt.Sprintf("分页文章%d", i), fmt.Sprintf("page-%d", i))
	}
	_, e := f.db.Exec(context.Background(), "UPDATE site_articles SET created_at='2026-01-01'")
	if e != nil {
		t.Fatal(e)
	}
	cursor := ""
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		c, data := f.request(t, "GET", "platform/articles?limit=7&cursor="+cursor, nil, true)
		if c != 200 {
			t.Fatal(c, data)
		}
		for _, v := range data["items"].([]any) {
			id := v.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatal("重复")
			}
			seen[id] = true
		}
		if !data["has_more"].(bool) {
			break
		}
		cursor = data["next_cursor"].(string)
	}
	if len(seen) != 25 {
		t.Fatal(len(seen))
	}
	if c, _ := f.request(t, "GET", "platform/articles?cursor=bad", nil, true); c != 400 {
		t.Fatal(c)
	}
}

// TestDailyArticleJob 验证18点仅认领一次，停机恢复不批量补发，成功任务重启不重复生成。
func TestDailyArticleJob(t *testing.T) {
	f := newArticleFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	f.s.now = func() time.Time { return now }
	_, e := f.db.Exec(ctx, "UPDATE site_article_settings SET ai_enabled=true,ai_prompt='行业场景',next_run_at=$1", now.Add(-time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e = f.s.schedule(ctx); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	_ = f.db.QueryRow(ctx, "SELECT count(*) FROM site_article_jobs").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	var prompt string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		prompt = string(raw)
		draft := Draft{Language: "zh-CN", Title: "珠宝门店如何用知识库回答保养问题", Slug: "jewelry-care", Markdown: "## 示例场景\n\n依据门店知识库回答。", Keywords: []string{}}
		out, _ := json.Marshal(draft)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(out)}}}})
	}))
	defer model.Close()
	f.s.Model = func(context.Context) (platform.TextModel, error) {
		return platform.TextModel{BaseURL: model.URL, APIKey: "test", Model: "test"}, nil
	}
	for i := 0; i < 3; i++ {
		f.createArticle(t, fmt.Sprintf("已有标题%d", i), fmt.Sprintf("existing-%d", i))
	}
	worked, e := f.s.runOne(ctx)
	if e != nil || !worked {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(prompt, fmt.Sprintf("已有标题%d", i)) {
			t.Fatal("遗漏既有标题")
		}
	}
	if worked, e = f.s.runOne(ctx); worked || e != nil {
		t.Fatal(worked, e)
	}
	_ = f.db.QueryRow(ctx, "SELECT count(*) FROM site_articles WHERE status='published'").Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	var next time.Time
	_ = f.db.QueryRow(ctx, "SELECT next_run_at FROM site_article_settings WHERE id").Scan(&next)
	if next.Sub(now) != 24*time.Hour {
		t.Fatal(next)
	}
	// 过期围栏不能发布旧结果。
	var id, token string
	e = f.db.QueryRow(ctx, "INSERT INTO site_article_jobs(kind,dedupe_key,status,lease_until,lease_token) VALUES('generate','stale','running',now()-interval '1 second',gen_random_uuid()) RETURNING id::text,lease_token::text").Scan(&id, &token)
	if e != nil {
		t.Fatal(e)
	}
	e = pgx.BeginFunc(ctx, f.db, func(tx pgx.Tx) error { return assertLease(ctx, tx, job{ID: id, Token: token}) })
	if e == nil {
		t.Fatal("旧租约越权")
	}
}

type searchTransport func(*http.Request) (*http.Response, error)

// RoundTrip 截获固定官方端点以验证请求，不访问真实搜索账号或消耗推送配额。
func (fn searchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

// TestSearchProviderContracts 验证百度文本、IndexNow所有权和Google签名授权及目录提交。
func TestSearchProviderContracts(t *testing.T) {
	f := newArticleFixture(t)
	ctx := context.Background()
	a := f.createArticle(t, "教育招生如何配置客服知识库", "education-help")
	_, _ = f.request(t, "POST", "platform/articles/"+a["id"].(string)+"/publish", map[string]any{"version": a["version"]}, true)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	keyRaw, _ := x509.MarshalPKCS8PrivateKey(key)
	creds, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "test@example.iam.gserviceaccount.com", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyRaw}))})
	b, _ := f.s.secret("private-baidu", "baidu", false)
	i, _ := f.s.secret("indexnow-test-key", "indexnow", false)
	g, _ := f.s.secret(string(creds), "google", false)
	_, e := f.db.Exec(ctx, "UPDATE site_article_settings SET baidu_enabled=true,baidu_token_cipher=$1,indexnow_enabled=true,indexnow_key_cipher=$2,google_enabled=true,google_credentials_cipher=$3", b, i, g)
	if e != nil {
		t.Fatal(e)
	}
	calls := map[string]int{}
	f.s.client = &http.Client{Transport: searchTransport(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Hostname()]++
		raw, _ := io.ReadAll(r.Body)
		status, body := 200, `{}`
		switch r.URL.Hostname() {
		case "data.zz.baidu.com":
			if r.URL.Query().Get("token") != "private-baidu" || !strings.Contains(string(raw), "/zh-CN/articles/education-help") {
				t.Error("百度协议")
			}
			body = `{"success":1}`
		case "api.indexnow.org":
			var value map[string]any
			_ = json.Unmarshal(raw, &value)
			if value["keyLocation"] != "https://goodchat.example/indexnow-key.txt" || value["host"] != "goodchat.example" {
				t.Error(value)
			}
			status = 202
		case "oauth2.googleapis.com":
			r.Body = io.NopCloser(bytes.NewReader(raw))
			_ = r.ParseForm()
			parts := strings.Split(r.Form.Get("assertion"), ".")
			if len(parts) != 3 {
				t.Fatal("JWT")
			}
			sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
			hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, hash[:], sig) != nil {
				t.Error("签名错误")
			}
			body = `{"access_token":"short-lived"}`
		case "www.googleapis.com":
			if r.Method != "PUT" || r.Header.Get("Authorization") != "Bearer short-lived" || !strings.Contains(r.URL.Path, "/sitemaps/https://goodchat.example/sitemap.xml") {
				t.Error(r.Method, r.URL.Path)
			}
			status = 204
		default:
			t.Error("不允许端点", r.URL.Hostname())
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	for _, provider := range []string{"baidu", "indexnow", "google"} {
		out, e := f.s.submitSearch(ctx, job{Provider: provider, ArticleID: a["id"].(string)})
		if e != nil || out["submitted"] != 1 {
			t.Fatal(out, e)
		}
	}
	if len(calls) != 4 {
		t.Fatal(calls)
	}
	_, out := f.request(t, "GET", "platform/article-settings", nil, true)
	serialized, _ := json.Marshal(out)
	if strings.Contains(string(serialized), "private-baidu") || strings.Contains(string(serialized), "PRIVATE KEY") {
		t.Fatal("泄露凭据")
	}
	code, _ := f.request(t, "PUT", "platform/article-settings", map[string]any{"section": "search", "version": out["version"], "baidu_enabled": true, "indexnow_enabled": true, "google_enabled": true}, true)
	if code != 200 {
		t.Fatal(code)
	}
	var preserved string
	_ = f.db.QueryRow(ctx, "SELECT baidu_token_cipher FROM site_article_settings WHERE id").Scan(&preserved)
	if preserved != b {
		t.Fatal("空白覆盖密钥")
	}
}
