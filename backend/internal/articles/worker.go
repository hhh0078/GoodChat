// 文件作用说明：以数据库幂等键、租约围栏和有限重试执行文章生成、翻译及搜索提交，重启后可恢复。
package articles

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

type job struct {
	ID        string
	Kind      string
	ArticleID string
	Provider  string
	Payload   json.RawMessage
	Attempts  int
	Token     string
}
type generationInput struct {
	Prompt    string   `json:"prompt"`
	Source    string   `json:"source_language"`
	Targets   []string `json:"target_languages"`
	Scheduled bool     `json:"scheduled"`
}

// Start 周期认领到期日程和任务；模型及搜索请求不占用数据库事务锁。
func (s *Service) Start(ctx context.Context) {
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	for {
		_ = s.schedule(ctx)
		for ctx.Err() == nil {
			worked, err := s.runOne(ctx)
			if err != nil || !worked {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

// schedule 锁住唯一计划行并跳过停机错过的多日积压，每个本地日只建立一次自动生成任务。
func (s *Service) schedule(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		value, err := readSettings(tx.QueryRow(ctx, "SELECT to_jsonb(c) FROM site_article_settings c WHERE id FOR UPDATE"))
		if err != nil {
			return err
		}
		now := s.now()
		if !value.AIEnabled || value.NextRun == nil || value.NextRun.After(now) {
			return nil
		}
		zone, err := time.LoadLocation(value.Timezone)
		if err != nil {
			return err
		}
		input := generationInput{Prompt: value.Prompt, Source: value.Source, Targets: value.Targets, Scheduled: true}
		key := "daily:" + now.In(zone).Format("2006-01-02")
		if err = enqueue(ctx, tx, "generate", "", "", key, "", input); err != nil {
			return err
		}
		next, err := nextDaily(now, value.Clock, value.Timezone)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE site_article_settings SET next_run_at=$1 WHERE id", next)
		return err
	})
}

// runOne 用随机租约围栏认领一项任务，进程失去租约后不能再发布或完成旧结果。
func (s *Service) runOne(ctx context.Context) (bool, error) {
	_, sweepErr := s.pool.Exec(ctx, "UPDATE site_article_jobs SET status='failed',error_code='article_lease_expired',error_message='任务执行租约已过期，请检查后重新发起',completed_at=now(),lease_until=NULL,lease_token=NULL WHERE status='running' AND lease_until<now() AND attempts>=3")
	if sweepErr != nil {
		return false, sweepErr
	}
	var j job
	err := s.pool.QueryRow(ctx, `UPDATE site_article_jobs SET status='running',attempts=attempts+1,lease_until=now()+interval '10 minutes',lease_token=gen_random_uuid() WHERE id=(SELECT id FROM site_article_jobs WHERE (status='queued' AND run_after<=now() OR status='running' AND lease_until<now()) AND attempts<3 ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id::text,kind,coalesce(article_id::text,''),provider,payload,attempts,lease_token::text`).Scan(&j.ID, &j.Kind, &j.ArticleID, &j.Provider, &j.Payload, &j.Attempts, &j.Token)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, workErr := s.execute(ctx, j)
	if workErr != nil {
		code, message := safeJobError(workErr)
		status := "queued"
		var completed any
		if j.Attempts >= 3 {
			status = "failed"
			completed = s.now()
		}
		if code == "article_stale" || code == "article_schedule_disabled" {
			status = "cancelled"
			completed = s.now()
		}
		_, err = s.pool.Exec(ctx, `UPDATE site_article_jobs SET status=$3,error_code=$4,error_message=$5,completed_at=$6,run_after=now()+($7::int * interval '30 seconds'),lease_until=NULL,lease_token=NULL WHERE id=$1 AND lease_token=$2::uuid AND lease_until>now()`, j.ID, j.Token, status, code, message, completed, j.Attempts)
		return true, err
	}
	raw, _ := json.Marshal(result)
	_, err = s.pool.Exec(ctx, `UPDATE site_article_jobs SET status='succeeded',result=$3,completed_at=now(),lease_until=NULL,lease_token=NULL,error_code='',error_message='' WHERE id=$1 AND lease_token=$2::uuid AND lease_until>now()`, j.ID, j.Token, raw)
	return true, err
}

// safeJobError 只保存受控业务错误，不记录供应商原始响应、请求URL或任何鉴权头。
func safeJobError(err error) (string, string) {
	if value, ok := err.(*jobError); ok {
		return value.code, value.message
	}
	var business *platform.APIError
	if errors.As(err, &business) {
		return business.Code, business.Message
	}
	return "article_task_failed", "任务未完成，请检查模型或搜索平台配置后重试"
}

type jobError struct{ code, message string }

// Error 返回不含凭据的任务提示。
func (e *jobError) Error() string { return e.message }

// taskFail 生成可公开给平台管理员的受控任务错误。
func taskFail(code, message string) error { return &jobError{code: code, message: message} }

// assertLease 在最终写入前锁定任务围栏，禁止被其他执行者重新认领的旧请求修改文章。
func assertLease(ctx context.Context, tx pgx.Tx, j job) error {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT status='running' AND lease_token=$2::uuid AND lease_until>now() FROM site_article_jobs WHERE id=$1 FOR UPDATE", j.ID, j.Token).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return taskFail("article_stale", "任务版本已过期")
	}
	return nil
}

// execute 分发有限任务种类，生成及翻译仅在完整校验后发布。
func (s *Service) execute(ctx context.Context, j job) (map[string]any, error) {
	switch j.Kind {
	case "submit":
		return s.submitSearch(ctx, j)
	case "generate":
		return s.generateArticle(ctx, j)
	case "translate":
		return s.translateArticle(ctx, j)
	}
	return nil, taskFail("article_task_invalid", "任务类型无效")
}

// generateNow 仅由平台管理员主动触发一篇文章，和每日计划共用持久化任务及去重规则。
func (s *Service) generateNow(w http.ResponseWriter, r *http.Request) {
	value, err := readSettings(s.pool.QueryRow(r.Context(), "SELECT to_jsonb(c) FROM site_article_settings c WHERE id"))
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	if value.Prompt == "" {
		respond(w, r, nil, platform.Fail(400, "invalid_prompt", "请先保存AI文章提示词"))
		return
	}
	if _, err = s.currentModel(r.Context()); err != nil {
		respond(w, r, nil, platform.Fail(503, "article_ai_unavailable", "平台尚未配置可用文字模型"))
		return
	}
	err = pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		return enqueue(r.Context(), tx, "generate", "", "", "manual:"+rand.Text(), platform.Current(r).UserID, generationInput{Prompt: value.Prompt, Source: value.Source, Targets: value.Targets})
	})
	respond(w, r, map[string]bool{"queued": err == nil}, err)
}

// listJobs 返回最近任务的真实状态及安全结果，不向页面发送提示词快照或任何密钥。
func (s *Service) listJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT to_jsonb(j)-'payload'-'lease_token'-'created_by' FROM site_article_jobs j ORDER BY created_at DESC,id LIMIT 50`)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			respond(w, r, nil, err)
			return
		}
		items = append(items, raw)
	}
	respond(w, r, map[string]any{"items": items}, rows.Err())
}

// syncNow 重新提交全部已发布文章及站点地图，未配置的平台不会建立虚假成功记录。
func (s *Service) syncNow(w http.ResponseWriter, r *http.Request) {
	value, err := readSettings(s.pool.QueryRow(r.Context(), "SELECT to_jsonb(c) FROM site_article_settings c WHERE id"))
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	if !value.Baidu && !value.IndexNow && !value.Google {
		respond(w, r, nil, platform.Fail(400, "missing_search_credentials", "请先启用并配置搜索引擎"))
		return
	}
	err = pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		for provider, enabled := range map[string]bool{"baidu": value.Baidu, "indexnow": value.IndexNow, "google": value.Google} {
			if enabled {
				if err := enqueue(r.Context(), tx, "submit", "", provider, fmt.Sprintf("manual-sync:%s:%s", provider, rand.Text()), platform.Current(r).UserID, map[string]any{"all": true}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	respond(w, r, map[string]bool{"queued": err == nil}, err)
}
