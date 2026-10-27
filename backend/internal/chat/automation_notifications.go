// 文件作用说明：为留言可靠通知注入共用真实SMTP适配器，不复制邮件发送和受理判定。
package chat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// SetLeadNotifier 在服务启动前注入真实SMTP通知发送函数，不向访客开放任意发信能力。
func (s *Service) SetLeadNotifier(sender func(context.Context, string, string, string) (string, string)) {
	s.leadNotifier = sender
}

// leadNoticeConfig 表示管理员主动配置的提醒地址，不包含客户联系方式。
type leadNoticeConfig struct {
	Enabled bool   `json:"enabled"`
	Email   string `json:"email"`
	Version int    `json:"version"`
}

// ensureLeadNoticeConfig 返回默认关闭的持久配置，不读取或展示SMTP凭证。
func ensureLeadNoticeConfig(ctx context.Context, tx pgx.Tx, tenant string) (leadNoticeConfig, error) {
	var config leadNoticeConfig
	if _, err := tx.Exec(ctx, "INSERT INTO lead_notification_settings(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", tenant); err != nil {
		return config, err
	}
	err := tx.QueryRow(ctx, "SELECT enabled,email,version FROM lead_notification_settings WHERE tenant_id=$1", tenant).Scan(&config.Enabled, &config.Email, &config.Version)
	return config, err
}

// smtpConfigured 只表示配置字段和发送适配器已存在，不代表邮件受理或收件。
func (s *Service) smtpConfigured() bool {
	return s.leadNotifier != nil && s.cfg.SMTPHost != "" && s.cfg.SMTPPort != "" && s.cfg.SMTPFrom != ""
}

// leadSettings 以版本保护管理员提醒配置，未配置SMTP时不允许开启提醒。
func (s *Service) leadSettings(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var input leadNoticeConfig
	if r.Method == http.MethodPut {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
		input.Email = strings.TrimSpace(input.Email)
		if input.Version < 1 {
			returnError(w, r, "invalid_lead_settings", "请先读取当前留言提醒配置")
			return
		}
		if input.Email != "" {
			address, err := mail.ParseAddress(input.Email)
			if err != nil || address.Address != input.Email || len(input.Email) > 254 || strings.ContainsAny(input.Email, "\r\n") {
				returnError(w, r, "invalid_notification_email", "请填写有效的收件邮箱，不支持显示名或多个地址")
				return
			}
		}
		if input.Enabled && (input.Email == "" || !s.smtpConfigured()) {
			replyError(w, r, platform.Fail(503, "mail_unavailable", "请先配置SMTP并填写收件邮箱后开启提醒"))
			return
		}
	}
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		config, err := ensureLeadNoticeConfig(ctx, tx, a.TenantID)
		if err != nil {
			return err
		}
		if r.Method == http.MethodPut {
			tag, err := tx.Exec(ctx, "UPDATE lead_notification_settings SET enabled=$2,email=$3,version=version+1,updated_at=now() WHERE tenant_id=$1 AND version=$4", a.TenantID, input.Enabled, input.Email, input.Version)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return platform.Fail(409, "lead_settings_version_conflict", "留言通知配置已更新，请重新读取")
			}
			if err := audit(ctx, tx, a, "lead_notification_settings.updated", "", map[string]any{"enabled": input.Enabled, "version": input.Version + 1}); err != nil {
				return err
			}
			config, err = ensureLeadNoticeConfig(ctx, tx, a.TenantID)
			if err != nil {
				return err
			}
		}
		result = map[string]any{"enabled": config.Enabled, "email": config.Email, "version": config.Version, "smtp_configured": s.smtpConfigured(), "attachment_limit": 3, "max_attachment_bytes": 20971520}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// enqueueLeadNotification 在新留言事务内保存一次提醒，只冻结编号与官网登录入口而不附送客户资料。
func (s *Service) enqueueLeadNotification(ctx context.Context, tx pgx.Tx, tenant, lead string) error {
	config, err := ensureLeadNoticeConfig(ctx, tx, tenant)
	if err != nil {
		return err
	}
	if !config.Enabled {
		return nil
	}
	body := fmt.Sprintf("收到一条新的待处理留言。\n留言编号：%s\n请登录GoodChat查看处理：%s/login\n本提醒不包含客户电话、聊天正文或附件。", lead, strings.TrimRight(s.cfg.PublicURL, "/"))
	_, err = tx.Exec(ctx, "INSERT INTO lead_notification_jobs(tenant_id,lead_id,recipient,settings_version,subject,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,lead_id) DO NOTHING", tenant, lead, config.Email, config.Version, "GoodChat有新的待处理留言", body)
	return err
}

// StartLeadNotifications 启动两个独立通知执行器，SMTP等待不会阻塞欢迎任务和接待分配。
func (s *Service) StartLeadNotifications(ctx context.Context) {
	for index := 0; index < 2; index++ {
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := s.tickLeadNotifications(ctx); err != nil && ctx.Err() == nil {
						slog.Error("留言提醒任务暂未完成", "error", err)
					}
				}
			}
		}()
	}
}

// tickLeadNotifications 逐租户使用RLS领取，每轮每租户至多一封，两个进程通过SKIP LOCKED协调。
func (s *Service) tickLeadNotifications(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, "SELECT id::text FROM tenants WHERE status='active' ORDER BY id")
	if err != nil {
		return err
	}
	tenants := []string{}
	for rows.Next() {
		var tenant string
		if err := rows.Scan(&tenant); err != nil {
			rows.Close()
			return err
		}
		tenants = append(tenants, tenant)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, tenant := range tenants {
		if err := s.runLeadNotifications(ctx, tenant); err != nil {
			return err
		}
	}
	return nil
}

// runLeadNotifications 在事务外调用共用SMTP，过期执行一律转为unknown且不自动重发。
func (s *Service) runLeadNotifications(ctx context.Context, tenant string) error {
	var id, recipient, subject, body string
	attempt := 0
	err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		if err := tenantActive(ctx, tx, tenant); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE lead_notification_jobs SET status='unknown',finished_at=now(),last_error='worker_interrupted',lease_until=NULL,history=history||jsonb_build_array(jsonb_build_object('attempt',attempts,'status','unknown','failure_kind','worker_interrupted','at',now())) WHERE status='running' AND lease_until<=now()"); err != nil {
			return err
		}
		config, err := ensureLeadNoticeConfig(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE lead_notification_jobs SET status='failed',finished_at=now(),last_error='settings_changed' WHERE status='queued' AND (NOT $1 OR recipient<>$2 OR settings_version<>$3)", config.Enabled, config.Email, config.Version); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, "UPDATE lead_notification_jobs SET status='running',attempts=attempts+1,started_at=now(),lease_until=now()+interval '1 minute',finished_at=NULL,last_error='' WHERE id=(SELECT id FROM lead_notification_jobs WHERE status='queued' ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id::text,recipient,subject,body,attempts").Scan(&id, &recipient, &subject, &body, &attempt)
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	})
	if err != nil || id == "" {
		return err
	}
	current := false
	if err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM lead_notification_jobs j JOIN lead_notification_settings n ON n.tenant_id=j.tenant_id JOIN tenants t ON t.id=j.tenant_id WHERE j.id=$1 AND j.status='running' AND j.attempts=$2 AND j.lease_until>now() AND n.enabled AND n.email=j.recipient AND n.version=j.settings_version AND t.status='active')", id, attempt).Scan(&current)
	}); err != nil {
		return err
	}
	status, kind := "failed", "configuration"
	if !current {
		kind = "settings_changed"
	} else if s.leadNotifier != nil {
		status, kind = s.leadNotifier(ctx, recipient, subject, body)
	}
	if !inStrings(status, "accepted", "failed", "unknown") {
		status, kind = "unknown", "invalid_sender_result"
	}
	if len(kind) > 120 || strings.ContainsAny(kind, "\r\n") {
		kind = "invalid_sender_result"
	}
	return platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE lead_notification_jobs SET status=$4,finished_at=now(),lease_until=NULL,last_error=$5,history=history||jsonb_build_array(jsonb_build_object('attempt',$3::int,'status',$4::text,'failure_kind',$5::text,'at',now())) WHERE id=$1 AND tenant_id=$2 AND status='running' AND attempts=$3 AND lease_until>now()", id, tenant, attempt, status, kind)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return audit(ctx, tx, actor{TenantID: tenant}, "lead_notification.stale_result_ignored", id, map[string]any{"attempt": attempt})
		}
		return audit(ctx, tx, actor{TenantID: tenant}, "lead_notification.completed", id, map[string]any{"attempt": attempt, "status": status, "failure_kind": kind})
	})
}

// leadNotifications 展示真实投递分类，重试仅允许确定失败且有冷却的任务。
func (s *Service) leadNotifications(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodGet {
			rows, err := tx.Query(ctx, "SELECT to_jsonb(j)-'body'-'subject'-'lease_until' FROM lead_notification_jobs j ORDER BY created_at DESC,id LIMIT 200")
			if err != nil {
				return err
			}
			items, err := jsonRows(rows)
			result = map[string]any{"items": items}
			return err
		}
		id := r.PathValue("id")
		if !uuidPattern.MatchString(id) {
			return platform.Fail(400, "invalid_notification", "通知任务编号无效")
		}
		var status string
		var finished *time.Time
		if err := tx.QueryRow(ctx, "SELECT status,finished_at FROM lead_notification_jobs WHERE id=$1 FOR UPDATE", id).Scan(&status, &finished); err != nil {
			return err
		}
		if status != "failed" {
			return platform.Fail(409, "notification_retry_forbidden", "只有确定失败的通知可以重试；结果不确定时请先人工核对邮箱")
		}
		if finished != nil && time.Since(*finished) < time.Minute {
			return platform.Fail(429, "notification_retry_cooldown", "请等待一分钟后再重试失败通知")
		}
		config, err := ensureLeadNoticeConfig(ctx, tx, a.TenantID)
		if err != nil {
			return err
		}
		if !config.Enabled || !s.smtpConfigured() {
			return platform.Fail(503, "mail_unavailable", "请先配置SMTP并启用留言通知")
		}
		result, err = txJSON(ctx, tx, "UPDATE lead_notification_jobs SET status='queued',recipient=$2,settings_version=$3,lease_until=NULL,finished_at=NULL,last_error='' WHERE id=$1 RETURNING to_jsonb(lead_notification_jobs)-'body'-'subject'-'lease_until'", id, config.Email, config.Version)
		if err != nil {
			return err
		}
		return audit(ctx, tx, a, "lead_notification.retried", id, map[string]any{})
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}
