// 文件作用说明：实现平台文章草稿、乐观并发发布和语言版本翻译队列，源文件始终保留Markdown。
package articles

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// create 创建一个语言版本的私有草稿，HTML只由服务端从Markdown生成。
func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	var in Draft
	if err := platform.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err)
		return
	}
	html, err := validateDraft(&in)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	var raw []byte
	err = s.pool.QueryRow(r.Context(), `INSERT INTO site_articles(language,title,title_key,slug,summary,body_markdown,body_html,keywords,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid) RETURNING to_jsonb(site_articles)`, in.Language, in.Title, titleKey(in.Title), in.Slug, in.Summary, in.Markdown, html, in.Keywords, platform.Current(r).UserID).Scan(&raw)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	a, err := articleJSON(raw)
	respond(w, r, map[string]any{"article": a}, err)
}

// loadForUpdate 在事务内读取目标版本，拒绝陈旧编辑覆盖后台任务或其他管理员修改。
func loadForUpdate(ctx context.Context, tx pgx.Tx, id string, version int64) (Article, error) {
	if !idPattern.MatchString(id) {
		return Article{}, platform.Fail(400, "invalid_id", "文章编号无效")
	}
	var raw []byte
	err := tx.QueryRow(ctx, "SELECT to_jsonb(a) FROM site_articles a WHERE id=$1 FOR UPDATE", id).Scan(&raw)
	if err != nil {
		return Article{}, err
	}
	a, err := articleJSON(raw)
	if err != nil {
		return a, err
	}
	if version < 1 || a.Version != version {
		return a, platform.Fail(409, "version_conflict", "文章已被修改，请刷新后重试")
	}
	return a, nil
}

// update 保存标题和Markdown；曾发布的网址与语言保持稳定，避免既有搜索链接失效。
func (s *Service) update(w http.ResponseWriter, r *http.Request) {
	var in Draft
	if err := platform.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err)
		return
	}
	html, err := validateDraft(&in)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	var result Article
	err = pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		a, e := loadForUpdate(r.Context(), tx, r.PathValue("id"), in.Version)
		if e != nil {
			return e
		}
		if a.PublishedAt != nil && (in.Slug != a.Slug || in.Language != a.Language) {
			return platform.Fail(409, "article_url_locked", "已发布文章的语言与网址不能直接变更")
		}
		var raw []byte
		e = tx.QueryRow(r.Context(), `UPDATE site_articles SET language=$2,title=$3,title_key=$4,slug=$5,summary=$6,body_markdown=$7,body_html=$8,keywords=$9,version=version+1,updated_at=now() WHERE id=$1 RETURNING to_jsonb(site_articles)`, a.ID, in.Language, in.Title, titleKey(in.Title), in.Slug, in.Summary, in.Markdown, html, in.Keywords).Scan(&raw)
		if e != nil {
			return e
		}
		result, e = articleJSON(raw)
		if e != nil {
			return e
		}
		if result.Status == "published" {
			return s.enqueueSearch(r.Context(), tx, result)
		}
		return nil
	})
	respond(w, r, map[string]any{"article": result}, err)
}

// enqueue 写入幂等任务，不在HTTP请求内等待模型生成或外部搜索平台。
func enqueue(ctx context.Context, tx pgx.Tx, kind, article, provider, key, creator string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO site_article_jobs(kind,article_id,provider,dedupe_key,created_by,payload) VALUES($1,NULLIF($2,'')::uuid,$3,$4,NULLIF($5,'')::uuid,$6) ON CONFLICT(dedupe_key) DO NOTHING`, kind, article, provider, key, creator, raw)
	return err
}

// publishVersion 原子发布当前语言并建立所选译文任务，各译文仅在生成成功后单独对外可见。
func (s *Service) publishVersion(ctx context.Context, tx pgx.Tx, a Article, targets []string, creator string) (Article, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `UPDATE site_articles SET status='published',published_at=coalesce(published_at,now()),updated_at=now(),version=version+1 WHERE id=$1 RETURNING to_jsonb(site_articles)`, a.ID).Scan(&raw)
	if err != nil {
		return a, err
	}
	a, err = articleJSON(raw)
	if err != nil {
		return a, err
	}
	for _, language := range targets {
		if language == a.Language {
			continue
		}
		if a.SourceID != nil {
			return a, platform.Fail(400, "translation_source_required", "请从原文发布译文")
		}
		var targetVersion int64
		e := tx.QueryRow(ctx, "SELECT version FROM site_articles WHERE group_id=$1 AND language=$2", a.GroupID, language).Scan(&targetVersion)
		if e != nil && e != pgx.ErrNoRows {
			return a, e
		}
		err = enqueue(ctx, tx, "translate", a.ID, "", fmt.Sprintf("translate:%s:%d:%s", a.ID, a.Version, language), creator, map[string]any{"language": language, "version": a.Version, "target_version": targetVersion})
		if err != nil {
			return a, err
		}
	}
	return a, s.enqueueSearch(ctx, tx, a)
}

// publish 立即公开已保存的原文，按管理员明确勾选的语言异步翻译并记录结果。
func (s *Service) publish(w http.ResponseWriter, r *http.Request) {
	var in VersionRequest
	if err := platform.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err)
		return
	}
	targets, err := languages(in.Languages)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	var result Article
	err = pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		a, e := loadForUpdate(r.Context(), tx, r.PathValue("id"), in.Version)
		if e != nil {
			return e
		}
		result, e = s.publishVersion(r.Context(), tx, a, targets, platform.Current(r).UserID)
		return e
	})
	respond(w, r, map[string]any{"article": result, "translation_languages": targets}, err)
}

// archive 只下架所选语言版本，并停止以该版本为来源的未开始翻译，不删除历史内容。
func (s *Service) archive(w http.ResponseWriter, r *http.Request) {
	var in VersionRequest
	if err := platform.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err)
		return
	}
	err := pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		a, e := loadForUpdate(r.Context(), tx, r.PathValue("id"), in.Version)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), "UPDATE site_articles SET status='archived',version=version+1,updated_at=now() WHERE id=$1", a.ID); e != nil {
			return e
		}
		_, e = tx.Exec(r.Context(), "UPDATE site_article_jobs SET status='cancelled',completed_at=now() WHERE article_id=$1 AND kind='translate' AND status='queued'", a.ID)
		return e
	})
	respond(w, r, map[string]bool{"archived": err == nil}, err)
}
