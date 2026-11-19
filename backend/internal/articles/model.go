// Package articles 提供官网多语言Markdown文章、定时生成与搜索提交，平台管理员是唯一写入入口。
package articles

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"golang.org/x/text/unicode/norm"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
)

type Article struct {
	ID          string     `json:"id"`
	GroupID     string     `json:"group_id"`
	SourceID    *string    `json:"source_id"`
	Language    string     `json:"language"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Summary     string     `json:"summary"`
	Markdown    string     `json:"body_markdown,omitempty"`
	HTML        string     `json:"body_html,omitempty"`
	Keywords    []string   `json:"keywords"`
	Status      string     `json:"status"`
	Version     int64      `json:"version"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
type Draft struct {
	Language string   `json:"language"`
	Title    string   `json:"title"`
	Slug     string   `json:"slug"`
	Summary  string   `json:"summary"`
	Markdown string   `json:"body_markdown"`
	Keywords []string `json:"keywords"`
	Version  int64    `json:"version"`
}
type VersionRequest struct {
	Version   int64    `json:"version"`
	Languages []string `json:"languages"`
}

var idPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote))

// titleKey 将标点、全半角及大小写差异归一，阻止同语言的表面改名重复发布。
func titleKey(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(norm.NFKC.String(title)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// renderMarkdown 使用默认禁止原始HTML和危险链接的CommonMark渲染器，不开放unsafe选项。
func renderMarkdown(body string) (string, error) {
	var output bytes.Buffer
	source := []byte(body)
	document := markdown.Parser().Parse(text.NewReader(source))
	// 正文最高从二级标题开始，页面保留唯一的文章标题。
	_ = ast.Walk(document, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if h, ok := n.(*ast.Heading); ok && h.Level == 1 {
				h.Level = 2
			}
		}
		return ast.WalkContinue, nil
	})
	if err := markdown.Renderer().Render(&output, source, document); err != nil {
		return "", err
	}
	return output.String(), nil
}

// validateDraft 校验内容、语言和规范路径，并生成服务端唯一可信HTML。
func validateDraft(d *Draft) (string, error) {
	if d.Keywords == nil {
		d.Keywords = []string{}
	}
	d.Title = strings.TrimSpace(d.Title)
	d.Markdown = strings.TrimSpace(d.Markdown)
	d.Summary = strings.TrimSpace(d.Summary)
	if localization.Normalize(d.Language) != d.Language || d.Language == "" {
		return "", platform.Fail(400, "invalid_language", "请选择有效的文章语言")
	}
	if utf8.RuneCountInString(d.Title) < 1 || utf8.RuneCountInString(d.Title) > 160 || titleKey(d.Title) == "" || len(d.Markdown) == 0 || len(d.Markdown) > 200000 || utf8.RuneCountInString(d.Summary) > 500 {
		return "", platform.Fail(400, "invalid_article", "请填写有效的文章标题、摘要和Markdown正文")
	}
	if d.Slug == "" {
		d.Slug = "article-" + strings.ToLower(rand.Text()[:12])
	}
	if len(d.Slug) > 120 || !slugPattern.MatchString(d.Slug) {
		return "", platform.Fail(400, "invalid_slug", "文章网址仅支持小写字母、数字和短横线")
	}
	if len(d.Keywords) > 20 {
		return "", platform.Fail(400, "invalid_keywords", "文章关键词不能超过20项")
	}
	for _, word := range d.Keywords {
		if utf8.RuneCountInString(word) > 80 {
			return "", platform.Fail(400, "invalid_keywords", "文章关键词过长")
		}
	}
	return renderMarkdown(d.Markdown)
}

// articleJSON 将数据库受控投影转换为文章，不向页面返回创建账号或内部标题指纹。
func articleJSON(raw []byte) (Article, error) {
	var a Article
	err := json.Unmarshal(raw, &a)
	return a, err
}

// respond 统一响应，唯一约束冲突转换为可处理提示且不泄露SQL及数据库详情。
func respond(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err != nil {
		if err == pgx.ErrNoRows {
			err = platform.Fail(404, "article_not_found", "文章不存在或尚未发布")
		}
		if p, ok := err.(*pgconn.PgError); ok && p.Code == "23505" {
			err = platform.Fail(409, "article_duplicate", "同语言的标题或网址已存在，请检查已有文章")
		}
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, data)
}

// languages 保留明确选择的语言并去重，不将未勾选语言隐式加入发布。
func languages(values []string) ([]string, error) {
	if len(values) > 9 {
		return nil, platform.Fail(400, "invalid_language", "请选择有效的文章语言")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if localization.Normalize(v) != v || v == "" {
			return nil, platform.Fail(400, "invalid_language", "请选择有效的文章语言")
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, nil
}
