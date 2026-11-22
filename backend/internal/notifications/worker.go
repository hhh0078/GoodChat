// 文件作用说明：执行可恢复通知队列，确定失败有限退避，不确定结果禁止自动重发。
package notifications

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/localization"
)

// Start 启动两个独立邮件执行器与分钟业务扫描，不占用验证码发送通道。
func (s *Service) Start(ctx context.Context) {
	for i := 0; i < 2; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if e := s.tick(ctx); e != nil && ctx.Err() == nil {
						slog.Error("产品邮件执行失败", "error", e)
					}
				}
			}
		}()
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.scan(ctx)
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.scan(ctx)
			}
		}
	}()
}

// tick 认领任务后退出数据库事务再发送；SMTP已受理后不会因收尾失败重发。
func (s *Service) tick(ctx context.Context) error {
	var id, tenant, user, kind, recipient, locale, zone string
	var params map[string]string
	var attempt, version int
	var occurred, decisionAt time.Time
	e := s.systemTx(ctx, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "WITH stale AS (UPDATE notification_jobs SET status='unknown',failure_kind='worker_interrupted',lease_until=NULL,finished_at=now() WHERE status='running' AND lease_until<now() RETURNING id,attempts) INSERT INTO notification_attempts(job_id,attempt,status,failure_kind) SELECT id,attempts,'unknown','worker_interrupted' FROM stale ON CONFLICT DO NOTHING"); e != nil {
			return e
		}
		e := tx.QueryRow(ctx, `UPDATE notification_jobs SET status='running',attempts=attempts+1,lease_until=now()+interval '1 minute' WHERE id=(SELECT id FROM notification_jobs WHERE status='queued' AND due_at<=now() ORDER BY urgent DESC,due_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id::text,coalesce(tenant_id::text,''),user_id::text,kind,params,attempts,locale,timezone,template_version,created_at,statement_timestamp()`).Scan(&id, &tenant, &user, &kind, &params, &attempt, &locale, &zone, &version, &occurred, &decisionAt)
		if e == pgx.ErrNoRows {
			return nil
		}
		if e != nil {
			return e
		}
		savedLocale, savedZone := locale, zone
		if e := tx.QueryRow(ctx, `SELECT u.email,coalesce(nullif(u.preferred_locale,''),nullif($3,''),t.default_locale,'en'),coalesce(nullif(u.display_timezone,''),nullif($4,''),nullif(t.timezone,''),'UTC') FROM users u LEFT JOIN tenants t ON t.id=NULLIF($2,'')::uuid WHERE u.id=$1`, user, tenant, params["_locale"], params["_timezone"]).Scan(&recipient, &locale, &zone); e != nil {
			return e
		}
		if version != 0 {
			locale, zone = savedLocale, savedZone
		}
		if params["_locale"] != "" && kind == "test" {
			locale = params["_locale"]
			zone = params["_timezone"]
		}
		if localization.Normalize(locale) == "" {
			locale = "en"
		}
		if !localization.ValidTimezone(zone) {
			zone = "UTC"
		}
		var business, digest, updates bool
		var start, end int
		if e := tx.QueryRow(ctx, "SELECT coalesce(n.business_enabled,true),coalesce(n.digest_enabled,false),coalesce(n.updates_enabled,false),coalesce(n.quiet_start,22),coalesce(n.quiet_end,8) FROM users u LEFT JOIN mail_notification_preferences n ON n.user_id=u.id WHERE u.id=$1", user).Scan(&business, &digest, &updates, &start, &end); e != nil {
			return e
		}
		d, _ := Find(kind)
		cancel := !d.Urgent && !business || kind == "digest" && !digest || kind == "product_update" && !updates || kind == "onboarding" && !updates
		if params["_platform"] == "1" {
			var valid bool
			if e := tx.QueryRow(ctx, "SELECT is_platform_admin FROM users WHERE id=$1", user).Scan(&valid); e != nil {
				return e
			}
			cancel = cancel || !valid
		}

		var suppressed bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notification_jobs WHERE user_id=$1 AND status='failed' AND failure_kind='recipient_permanent' AND created_at>now()-interval '1 day')", user).Scan(&suppressed); e != nil {
			return e
		}
		if suppressed && kind != "test" {
			_, e := tx.Exec(ctx, "UPDATE notification_jobs SET status='suppressed',failure_kind='previous_permanent_failure',lease_until=NULL WHERE id=$1", id)
			id = ""
			return e
		}
		if tenant != "" {
			var active bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenants t WHERE id=$1 AND status='active')", tenant).Scan(&active); e != nil {
				return e
			}
			cancel = cancel || !active
			if kind != "permissions_changed" {
				var member bool
				if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2 AND NOT disabled AND removed_at IS NULL)", tenant, user).Scan(&member); e != nil {
					return e
				}
				cancel = cancel || !member
			}
			if kind == "balance_low" || kind == "balance_critical" || kind == "ai_blocked" || kind == "topup" || kind == "subscription_active" || kind == "subscription_expiring" || kind == "subscription_expired" || kind == "handoff_waiting" || kind == "knowledge_gap" || kind == "ai_errors" {
				var permitted bool
				if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_memberships m JOIN notification_settings n ON n.tenant_id=m.tenant_id WHERE m.tenant_id=$1 AND m.user_id=$2 AND NOT m.disabled AND m.removed_at IS NULL AND (m.role='owner' OR m.role='admin' AND n.recipient_ids ? m.user_id::text))", tenant, user).Scan(&permitted); e != nil {
					return e
				}
				cancel = cancel || !permitted
			}

			if !cancel {
				valid, e := s.stillRelevant(ctx, tx, tenant, kind, params)
				if e != nil {
					return e
				}
				cancel = !valid
			}
		}
		if cancel {
			_, e := tx.Exec(ctx, "UPDATE notification_jobs SET status='cancelled',lease_until=NULL,failure_kind='no_longer_relevant' WHERE id=$1", id)
			id = ""
			return e
		}
		due := decisionAt
		if !d.Urgent {
			due = NextAllowed(due, zone, start, end)
		}
		if due.After(decisionAt.Add(time.Second)) {
			_, e := tx.Exec(ctx, "UPDATE notification_jobs SET status='queued',due_at=$2,attempts=attempts-1,lease_until=NULL WHERE id=$1", id, due)
			id = ""
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE notification_jobs SET locale=$2,timezone=$3 WHERE id=$1", id, locale, zone)
		return e
	})
	if e != nil || id == "" {
		return e
	}
	if params == nil {
		params = map[string]string{}
	}
	if params["occurred_at"] == "" {
		params["occurred_at"] = occurred.UTC().Format(time.RFC3339)
	}
	if kind == "digest" || kind == "product_update" || kind == "onboarding" {
		category := "updates"
		if kind == "digest" {
			category = "digest"
		}
		bodyURL, e := s.unsubscribeURL(user, category, "/unsubscribe", locale, zone)
		if e != nil {
			return s.finish(ctx, id, attempt, "failed", "configuration", version)
		}
		headURL, e := s.unsubscribeURL(user, category, "/api/v1/public/mail-unsubscribe", locale, zone)
		if e != nil {
			return s.finish(ctx, id, attempt, "failed", "configuration", version)
		}
		params["_unsubscribe_url"] = bodyURL
		params["_unsubscribe_header"] = headURL
	}
	mail, e := s.renderVersion(ctx, kind, locale, zone, params, version)
	if e != nil {
		return s.finish(ctx, id, attempt, "failed", "template", 0)
	}
	if e := s.systemTx(ctx, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE notification_jobs SET template_version=$2 WHERE id=$1 AND status='running'", id, mail.Version)
		return e
	}); e != nil {
		return e
	}
	status, failure := "failed", "configuration"
	if s.send != nil {
		status, failure = s.send(ctx, recipient, mail)
	}
	if status != "accepted" && status != "failed" && status != "unknown" {
		status, failure = "unknown", "invalid_sender_result"
	}
	return s.finish(ctx, id, attempt, status, failure, mail.Version)
}

// finish 即使请求停止也记录结果，暂时失败最多自动尝试四次，永久失败不重试。
func (s *Service) finish(ctx context.Context, id string, attempt int, status, failure string, version int) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.systemTx(ctx, func(tx pgx.Tx) error {
		next := status
		delay := 0
		if status == "failed" && (failure == "temporary" || failure == "transport") && attempt < 4 {
			next = "queued"
			delay = attempt * attempt * 60
		}
		tag, e := tx.Exec(ctx, "UPDATE notification_jobs SET status=$3,failure_kind=$4,due_at=statement_timestamp()+make_interval(secs=>$5),lease_until=NULL,finished_at=CASE WHEN $3='queued' THEN NULL ELSE now() END WHERE id=$1 AND attempts=$2 AND status='running' AND lease_until>now()", id, attempt, next, failure, delay)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		_, e = tx.Exec(ctx, "INSERT INTO notification_attempts(job_id,attempt,status,failure_kind) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", id, attempt, status, failure)
		return e
	})
}

// stillRelevant 在发送前重新验证充值、续费和人工接待状态，取消过期提醒。
func (s *Service) stillRelevant(ctx context.Context, tx pgx.Tx, tenant, kind string, p map[string]string) (bool, error) {
	if _, e := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant); e != nil {
		return false, e
	}
	var valid bool
	switch kind {
	case "subscription_active", "subscription_expiring", "subscription_expired":
		e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_subscriptions WHERE tenant_id=$1 AND expires_at=$2::timestamptz)", tenant, p["_expires"]).Scan(&valid)
		return valid, e
	case "balance_low", "balance_critical":
		column := "low_micro"
		if kind == "balance_critical" {
			column = "critical_micro"
		}
		e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_wallets w JOIN notification_settings n ON n.tenant_id=w.tenant_id WHERE w.tenant_id=$1 AND w.balance_micro<=n."+column+")", tenant).Scan(&valid)
		return valid, e
	case "ai_blocked":
		e := tx.QueryRow(ctx, blockedSQL, tenant, time.Time{}).Scan(&valid)
		return valid, e
	case "knowledge_gap":
		e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_pending_items p JOIN notification_settings n ON n.tenant_id=p.tenant_id WHERE p.id::text=$1 AND p.tenant_id=$2 AND p.status IN('pending','supplementing') AND n.service_enabled)", p["reference"], tenant).Scan(&valid)
		return valid, e
	case "handoff_waiting":
		e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM conversations c JOIN notification_settings n ON n.tenant_id=c.tenant_id WHERE c.id::text=$1 AND c.status<>'closed' AND c.ai_paused AND n.service_enabled AND NOT EXISTS(SELECT 1 FROM messages WHERE conversation_id=c.id AND sender_kind='agent' AND ai_job_id IS NULL AND created_at>$2::timestamptz))", p["reference"], p["_handoff_at"]).Scan(&valid)
		return valid, e
	case "onboarding":
		e := tx.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM access_links WHERE tenant_id=$1 AND NOT is_default)", tenant).Scan(&valid)
		return valid, e
	}
	return true, nil
}

// Wait 等待正在执行的SMTP结果留痕，必须在关闭数据库连接池前调用。
func (s *Service) Wait() { s.wg.Wait() }
