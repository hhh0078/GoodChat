// 文件作用说明：装配通知服务、受控事务入队与模板版本渲染，不对外开放任意收件人发信。
package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
	"sync"
	"time"
)

// Event 是已提交业务同事务产生的受控通知，不接受客户端自定事件类型与收件人。
type Event struct {
	Tenant, User, Kind, Key string
	Params                  map[string]string
}

// Service 保存独立通知执行器与现有SMTP适配器。
type Service struct {
	wg         sync.WaitGroup
	MailStatus func() map[string]any
	pool       *pgxpool.Pool
	auth       platform.Authorizer
	cfg        platform.Config
	send       func(context.Context, string, Mail) (string, string)
}

// New 复用现有身份校验和SMTP，不新增外部服务依赖。
func New(pool *pgxpool.Pool, auth platform.Authorizer, cfg platform.Config, send func(context.Context, string, Mail) (string, string)) *Service {
	return &Service{pool: pool, auth: auth, cfg: cfg, send: send}
}

// Enqueue 将通知与业务放在同一事务，唯一索引阻止重复请求重发。
func Enqueue(ctx context.Context, tx pgx.Tx, e Event) error {
	if e.Tenant != "" {
		e.Key = e.Tenant + ":" + e.Key
	}
	d, ok := Find(e.Kind)
	if !ok || e.User == "" || e.Key == "" || strings.HasPrefix(e.Kind, "code_") || (e.Kind == "invitation" || e.Kind == "admin_invitation") {
		return fmt.Errorf("通知事件无效")
	}
	if _, ok := e.Params["code"]; ok {
		return fmt.Errorf("禁止持久化验证码")
	}
	if _, ok := e.Params["token"]; ok {
		return fmt.Errorf("禁止持久化邀请凭证")
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.notice_user',$1,true)", e.User); err != nil {
		return err
	}
	var due *time.Time
	if v, err := time.Parse(time.RFC3339Nano, e.Params["_not_before"]); err == nil {
		due = &v
	}
	raw, err := json.Marshal(e.Params)
	if err != nil {
		return err
	}
	if e.Params == nil {
		raw = []byte(`{}`)
	}
	_, err = tx.Exec(ctx, `INSERT INTO notification_jobs(tenant_id,user_id,kind,event_key,params,urgent,due_at) VALUES(NULLIF($1,'')::uuid,$2,$3,$4,$5,$6,coalesce($7::timestamptz,statement_timestamp())) ON CONFLICT(user_id,kind,event_key) DO NOTHING`, e.Tenant, e.User, e.Kind, e.Key, raw, d.Urgent, due)
	return err
}

// systemTx 只供已授权的后台调度器或平台管理员使用，作用域限定在当前事务。
func (s *Service) systemTx(ctx context.Context, action func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.notice_worker','on',true)"); err != nil {
			return err
		}
		return action(tx)
	})
}

// userTx 为个人读写设置经过认证的账号与租户上下文。
func (s *Service) userTx(r *http.Request, action func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		p := platform.Current(r)
		if _, err := tx.Exec(r.Context(), "SELECT set_config('app.notice_user',$1,true),set_config('app.tenant_id',$2,true)", p.UserID, p.TenantID); err != nil {
			return err
		}
		return action(tx)
	})
}

// RenderMail 为主动认证邮件读取当前已发布的语言模板，正文仅保存在请求内存。
func (s *Service) RenderMail(ctx context.Context, kind, locale, zone string, params map[string]string) (Mail, error) {
	return s.renderVersion(ctx, kind, locale, zone, params, 0)
}

// renderVersion 固定重试的历史模板版本，缺少自定义版本时使用完整内置译文。
func (s *Service) renderVersion(ctx context.Context, kind, locale, zone string, params map[string]string, version int) (Mail, error) {
	d, ok := Find(kind)
	if kind == "ai_errors" && params["_platform"] == "1" {
		d.Path = "/platform"
	}
	if !ok {
		return Mail{}, fmt.Errorf("通知模板无效")
	}
	if localization.Normalize(locale) == "" {
		locale = "en"
	}
	subject, body, button := localization.Format(locale, d.Subject), localization.Format(locale, d.Body), localization.Format(locale, d.Button)
	selected := -1
	if version != -1 {
		err := s.pool.QueryRow(ctx, `SELECT subject,body,button,version FROM notification_templates WHERE kind=$1 AND locale=$2 AND ($3=0 OR version=$3) ORDER BY version DESC LIMIT 1`, kind, locale, version).Scan(&subject, &body, &button, &selected)
		if err != nil && err != pgx.ErrNoRows {
			return Mail{}, err
		}
		if err == pgx.ErrNoRows && version > 0 {
			return Mail{}, fmt.Errorf("通知模板版本不存在")
		}
	}
	values := map[string]string{}
	for k, v := range params {
		values[k] = v
	}
	if kind == "product_update" && params["_announcement"] != "" {
		var translations map[string]string
		if e := s.pool.QueryRow(ctx, "SELECT value->'translations' FROM notification_state WHERE key=$1", params["_announcement"]).Scan(&translations); e != nil {
			return Mail{}, e
		}
		if text := translations[locale]; text != "" {
			values["reference"] = text
		}
	}
	for _, k := range []string{"expires_at", "occurred_at"} {
		if at, e := time.Parse(time.RFC3339, values[k]); e == nil {
			values[k] = LocalTime(at, zone)
		}
	}
	return Render(s.cfg.PublicURL, locale, zone, d, values, subject, body, button, selected)
}

// endpoint 统一输出安全错误，不回显数据库和邮件正文。
func endpoint(action func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if e := action(w, r); e != nil {
			platform.ErrorJSON(w, r, e)
		}
	}
}

// Register 注册个人、租户与平台通知入口，权限来自认证上下文。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/public/mail-unsubscribe", endpoint(s.unsubscribe))
	mux.HandleFunc("POST /api/v1/public/mail-unsubscribe", endpoint(s.unsubscribe))
	mux.HandleFunc("GET /api/v1/notifications", s.auth.RequireUser(endpoint(s.history)))
	mux.HandleFunc("GET /api/v1/notifications/preferences", s.auth.RequireUser(endpoint(s.preferences)))
	mux.HandleFunc("PUT /api/v1/notifications/preferences", s.auth.RequireUser(endpoint(s.preferences)))
	mux.HandleFunc("GET /api/v1/tenant/notifications/settings", s.auth.RequireAdmin(endpoint(s.settings)))
	mux.HandleFunc("PUT /api/v1/tenant/notifications/settings", s.auth.RequireAdmin(endpoint(s.settings)))
	mux.HandleFunc("GET /api/v1/platform/notifications", s.auth.RequirePlatform(endpoint(s.platformHistory)))
	mux.HandleFunc("POST /api/v1/platform/notifications/{id}/retry", s.auth.RequirePlatform(endpoint(s.retry)))
	mux.HandleFunc("GET /api/v1/platform/mail-templates", s.auth.RequirePlatform(endpoint(s.templates)))
	mux.HandleFunc("POST /api/v1/platform/mail-templates", s.auth.RequirePlatform(endpoint(s.publish)))
	mux.HandleFunc("POST /api/v1/platform/mail-templates/preview", s.auth.RequirePlatform(endpoint(s.preview)))
	mux.HandleFunc("POST /api/v1/platform/mail-test", s.auth.RequirePlatform(endpoint(s.testMail)))
	mux.HandleFunc("POST /api/v1/platform/mail-announcement", s.auth.RequirePlatform(endpoint(s.announcement)))
}
