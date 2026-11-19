// 文件作用说明：装配平台文章管理及只读官网接口，公共读取始终按发布状态与当前语言过滤。
package articles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

type Authorizer interface {
	RequirePlatform(http.HandlerFunc) http.HandlerFunc
}
type Service struct {
	pool   *pgxpool.Pool
	auth   Authorizer
	cfg    platform.Config
	client *http.Client
	now    func() time.Time
	Model  func(context.Context) (platform.TextModel, error)
}

// New 使用平台既有模型配置及认证，不创建第二套账号、数据库或支付配置。
func New(pool *pgxpool.Pool, auth Authorizer, cfg platform.Config) *Service {
	return &Service{pool: pool, auth: auth, cfg: cfg, client: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}
}

// Register 分离平台写入和公开文章，不以客户端租户参数授予平台权限。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/public/articles", s.publicList)
	mux.HandleFunc("GET /api/v1/public/articles/sitemap", s.sitemap)
	mux.HandleFunc("GET /api/v1/public/articles/{slug}", s.publicArticle)
	mux.HandleFunc("GET /api/v1/public/article-indexnow-key", s.indexNowKey)
	mux.HandleFunc("GET /api/v1/platform/articles", s.auth.RequirePlatform(s.list))
	mux.HandleFunc("POST /api/v1/platform/articles", s.auth.RequirePlatform(s.create))
	mux.HandleFunc("GET /api/v1/platform/articles/{id}", s.auth.RequirePlatform(s.get))
	mux.HandleFunc("PATCH /api/v1/platform/articles/{id}", s.auth.RequirePlatform(s.update))
	mux.HandleFunc("POST /api/v1/platform/articles/{id}/publish", s.auth.RequirePlatform(s.publish))
	mux.HandleFunc("POST /api/v1/platform/articles/{id}/archive", s.auth.RequirePlatform(s.archive))
	mux.HandleFunc("POST /api/v1/platform/articles/preview", s.auth.RequirePlatform(s.preview))
	mux.HandleFunc("GET /api/v1/platform/article-settings", s.auth.RequirePlatform(s.getSettings))
	mux.HandleFunc("PUT /api/v1/platform/article-settings", s.auth.RequirePlatform(s.saveSettings))
	mux.HandleFunc("GET /api/v1/platform/article-jobs", s.auth.RequirePlatform(s.listJobs))
	mux.HandleFunc("POST /api/v1/platform/article-generate", s.auth.RequirePlatform(s.generateNow))
	mux.HandleFunc("POST /api/v1/platform/article-sync", s.auth.RequirePlatform(s.syncNow))
}

// queryArticles 将查询结果固定为受控字段，避免列表携带整个Markdown正文。
func (s *Service) queryArticles(ctx context.Context, sql string, args ...any) ([]Article, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Article{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		a, e := articleJSON(raw)
		if e != nil {
			return nil, e
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

const listProjection = "(to_jsonb(a)-'created_by'-'title_key'-'body_markdown'-'body_html')"

// listPage 按创建编号稳定翻页；位置仅改变公开结果范围，不能绕过语言和发布过滤。
func (s *Service) listPage(w http.ResponseWriter, r *http.Request, public bool) {
	q := r.URL.Query()
	language := q.Get("language")
	if public && language == "" {
		language = "zh-CN"
	}
	if language != "" {
		if _, err := languages([]string{language}); err != nil {
			respond(w, r, nil, err)
			return
		}
	}
	limit := 20
	if value := q.Get("limit"); value != "" {
		n, e := strconv.Atoi(value)
		if e != nil || n < 1 || n > 100 {
			respond(w, r, nil, platform.Fail(400, "invalid_limit", "每页记录数需为1至100"))
			return
		}
		limit = n
	}
	cursor := q.Get("cursor")
	var position struct {
		Time time.Time `json:"time"`
		ID   string    `json:"id"`
	}
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(raw, &position) != nil || position.Time.IsZero() || !idPattern.MatchString(position.ID) {
			respond(w, r, nil, platform.Fail(400, "invalid_cursor", "分页位置无效，请刷新后重试。"))
			return
		}
	}

	status := q.Get("status")
	if public {
		status = "published"
	}
	if status != "" && status != "published" && status != "draft" && status != "archived" {
		respond(w, r, nil, platform.Fail(400, "invalid_status", "文章状态无效"))
		return
	}
	query := strings.TrimSpace(q.Get("query"))
	if len(query) > 300 {
		respond(w, r, nil, platform.Fail(400, "invalid_query", "搜索文字过长"))
		return
	}
	items, err := s.queryArticles(r.Context(), "SELECT "+listProjection+" FROM site_articles a WHERE ($1='' OR language=$1) AND ($2='' OR status=$2) AND ($3='' OR strpos(lower(title||' '||summary),lower($3))>0) AND ($4::timestamptz IS NULL OR created_at<$4 OR (created_at=$4 AND id>NULLIF($5,'')::uuid)) ORDER BY created_at DESC,id LIMIT $6", language, status, query, nullableTime(position.Time), position.ID, limit+1)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	more := len(items) > limit
	next := ""
	if more {
		items = items[:limit]
		last := items[len(items)-1]
		raw, _ := json.Marshal(map[string]any{"time": last.CreatedAt, "id": last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	respond(w, r, map[string]any{"items": items, "has_more": more, "next_cursor": next}, nil)
}

// list 返回平台管理员可管理的全部语言，语言筛选由显式参数限定。
func (s *Service) list(w http.ResponseWriter, r *http.Request) { s.listPage(w, r, false) }

// publicList 只展示当前语言已发布文章，不使用其他语言兜底。
func (s *Service) publicList(w http.ResponseWriter, r *http.Request) { s.listPage(w, r, true) }

// publicArticle 用规范路径和语言联合查询，缺失译文返回404而不是另一语言的正文。
func (s *Service) publicArticle(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("language")
	if _, err := languages([]string{lang}); err != nil {
		respond(w, r, nil, err)
		return
	}
	var raw []byte
	err := s.pool.QueryRow(r.Context(), "SELECT to_jsonb(a)-'created_by'-'title_key' FROM site_articles a WHERE slug=$1 AND language=$2 AND status='published'", r.PathValue("slug"), lang).Scan(&raw)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	a, err := articleJSON(raw)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	variants, err := s.queryArticles(r.Context(), "SELECT "+listProjection+" FROM site_articles a WHERE group_id=$1 AND status='published' ORDER BY language", a.GroupID)
	respond(w, r, map[string]any{"article": a, "alternates": variants}, err)
}

// sitemap 返回所有实际已发布语言版本的目录数据，不暴露草稿或猜测未发布译文链接。
func (s *Service) sitemap(w http.ResponseWriter, r *http.Request) {
	items, err := s.queryArticles(r.Context(), "SELECT "+listProjection+" FROM site_articles a WHERE status='published' ORDER BY group_id,language")
	respond(w, r, map[string]any{"items": items}, err)
}

// preview 仅渲染当前管理员输入的Markdown，不写入或公开草稿。
func (s *Service) preview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body_markdown"`
	}
	if err := platform.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err)
		return
	}
	if len(in.Body) > 200000 {
		respond(w, r, nil, platform.Fail(400, "invalid_article", "Markdown正文过长"))
		return
	}
	html, err := renderMarkdown(in.Body)
	respond(w, r, map[string]string{"body_html": html}, err)
}

// get 返回当前语言版本及关联译文，平台权限由路由中间件验证。
func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	if !idPattern.MatchString(r.PathValue("id")) {
		respond(w, r, nil, platform.Fail(400, "invalid_id", "文章编号无效"))
		return
	}
	var raw json.RawMessage
	err := s.pool.QueryRow(r.Context(), "SELECT to_jsonb(a)-'created_by'-'title_key' FROM site_articles a WHERE id=$1", r.PathValue("id")).Scan(&raw)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	a, err := articleJSON(raw)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	variants, err := s.queryArticles(r.Context(), "SELECT "+listProjection+" FROM site_articles a WHERE group_id=$1 ORDER BY language", a.GroupID)
	respond(w, r, map[string]any{"article": a, "alternates": variants}, err)
}

// nullableTime 将未指定游标转换为空，避免零时间改变第一页范围。
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
