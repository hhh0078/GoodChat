// 文件作用说明：实现个人订阅、租户已验证管理员收件人和隐私最小化通知历史。
package notifications

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// Preferences 保留业务订阅和本地小时；语言时区复用账号已确认偏好。
type Preferences struct {
	Business bool `json:"business_enabled"`
	Digest   bool `json:"digest_enabled"`
	Updates  bool `json:"updates_enabled"`
	Start    int  `json:"quiet_start"`
	End      int  `json:"quiet_end"`
}

// preferences 仅允许本人修改非安全通知订阅，关键邮件不受开关影响。
func (s *Service) preferences(w http.ResponseWriter, r *http.Request) error {
	var p Preferences
	if r.Method == http.MethodPut {
		if e := platform.Decode(w, r, &p); e != nil {
			return e
		}
		if p.Start < 0 || p.Start > 23 || p.End < 0 || p.End > 23 {
			return platform.Fail(400, "invalid_notice_hours", "请选择0至23的静默小时")
		}
	}
	var result map[string]any
	e := s.userTx(r, func(tx pgx.Tx) error {
		ctx := r.Context()
		id := platform.Current(r).UserID
		if _, e := tx.Exec(ctx, "INSERT INTO mail_notification_preferences(user_id) VALUES($1) ON CONFLICT DO NOTHING", id); e != nil {
			return e
		}
		if r.Method == http.MethodPut {
			if _, e := tx.Exec(ctx, "UPDATE mail_notification_preferences SET business_enabled=$2,digest_enabled=$3,updates_enabled=$4,quiet_start=$5,quiet_end=$6,updated_at=now() WHERE user_id=$1", id, p.Business, p.Digest, p.Updates, p.Start, p.End); e != nil {
				return e
			}
		}
		return tx.QueryRow(ctx, `SELECT to_jsonb(n)||jsonb_build_object('locale',u.preferred_locale,'timezone',u.display_timezone) FROM mail_notification_preferences n JOIN users u ON u.id=n.user_id WHERE n.user_id=$1`, id).Scan(&result)
	})
	if e != nil {
		return e
	}
	platform.JSON(w, 200, result)
	return nil
}

// Settings 限制租户业务通知收件人为有效管理员账号，金额使用整数微元。
type Settings struct {
	Recipients []string `json:"recipient_ids"`
	Low        int64    `json:"low_micro"`
	Critical   int64    `json:"critical_micro"`
	Handoff    int      `json:"handoff_minutes"`
	Service    bool     `json:"service_enabled"`
}

// settings 在租户RLS中验证收件人和阈值，所有者始终为默认账务收件人。
func (s *Service) settings(w http.ResponseWriter, r *http.Request) error {
	var in Settings
	if r.Method == http.MethodPut {
		if e := platform.Decode(w, r, &in); e != nil {
			return e
		}
		if len(in.Recipients) > 20 || in.Low < 0 || in.Low > 1000000000000 || in.Critical < 0 || in.Critical > in.Low || in.Handoff < 1 || in.Handoff > 1440 {
			return platform.Fail(400, "invalid_notice_settings", "请检查通知阈值、收件人和超时时间")
		}
	}
	var result map[string]any
	e := s.userTx(r, func(tx pgx.Tx) error {
		ctx := r.Context()
		tenant := platform.Current(r).TenantID
		if _, e := tx.Exec(ctx, "INSERT INTO notification_settings(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", tenant); e != nil {
			return e
		}
		if r.Method == http.MethodPut {
			for index, id := range in.Recipients {
				id = strings.ToLower(id)
				in.Recipients[index] = id
				if !recipientUUID.MatchString(id) {
					return platform.Fail(400, "invalid_notice_recipient", "只能选择本租户有效管理员作为通知收件人")
				}
				var ok bool
				if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=$1 AND user_id::text=$2 AND role IN('owner','admin') AND NOT disabled AND removed_at IS NULL)", tenant, id).Scan(&ok); e != nil {
					return e
				}
				if !ok {
					return platform.Fail(400, "invalid_notice_recipient", "只能选择本租户有效管理员作为通知收件人")
				}
			}
			ids, _ := json.Marshal(in.Recipients)
			if in.Recipients == nil {
				ids = []byte(`[]`)
			}
			if _, e := tx.Exec(ctx, "UPDATE notification_settings SET recipient_ids=$2,low_micro=$3,critical_micro=$4,handoff_minutes=$5,service_enabled=$6,updated_at=now() WHERE tenant_id=$1", tenant, ids, in.Low, in.Critical, in.Handoff, in.Service); e != nil {
				return e
			}
		}
		if e := tx.QueryRow(ctx, "SELECT to_jsonb(n) FROM notification_settings n WHERE tenant_id=$1", tenant).Scan(&result); e != nil {
			return e
		}
		var members []map[string]any
		rows, e := tx.Query(ctx, "SELECT u.id::text,u.email,u.display_name,m.role FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.role IN('owner','admin') AND NOT m.disabled AND m.removed_at IS NULL ORDER BY m.role,u.email", tenant)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id, email, name, role string
			if e := rows.Scan(&id, &email, &name, &role); e != nil {
				return e
			}
			members = append(members, map[string]any{"id": id, "email": email, "name": name, "role": role})
		}
		result["members"] = members
		return rows.Err()
	})
	if e != nil {
		return e
	}
	platform.JSON(w, 200, result)
	return nil
}

// loadHistory 返回固定数量脱敏记录，明确受理不等于入箱。
func loadHistory(r *http.Request, tx pgx.Tx, all bool) ([]map[string]any, error) {
	rows, e := tx.Query(r.Context(), `SELECT jsonb_build_object('id',j.id,'tenant_id',j.tenant_id,'kind',j.kind,'status',j.status,'attempts',j.attempts,'attempt_history',coalesce((SELECT jsonb_agg(jsonb_build_object('attempt',a.attempt,'status',a.status,'failure_kind',a.failure_kind,'created_at',a.created_at) ORDER BY a.attempt) FROM notification_attempts a WHERE a.job_id=j.id),'[]'::jsonb),'failure_kind',j.failure_kind,'locale',j.locale,'created_at',j.created_at,'due_at',j.due_at,'recipient',CASE WHEN $2 THEN u.email ELSE '' END) FROM notification_jobs j JOIN users u ON u.id=j.user_id WHERE ($2 OR j.user_id=$1::uuid) ORDER BY j.created_at DESC,j.id DESC LIMIT 200`, platform.Current(r).UserID, all)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var item map[string]any
		if e := rows.Scan(&item); e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// history 仅返回本人收到的通知，不因当前租户上下文泄露其他成员记录。
func (s *Service) history(w http.ResponseWriter, r *http.Request) error {
	var items []map[string]any
	e := s.userTx(r, func(tx pgx.Tx) error { var e error; items, e = loadHistory(r, tx, false); return e })
	if e != nil {
		return e
	}
	platform.JSON(w, 200, map[string]any{"items": items})
	return nil
}

// platformHistory 提供跨租户投递监控及既有认证记录，不读取认证邮件正文。
func (s *Service) platformHistory(w http.ResponseWriter, r *http.Request) error {
	result := map[string]any{}
	e := s.systemTx(r.Context(), func(tx pgx.Tx) error {
		items, e := loadHistory(r, tx, true)
		if e != nil {
			return e
		}
		result["items"] = items
		var stats map[string]any
		if e := tx.QueryRow(r.Context(), `SELECT jsonb_build_object('queued',count(*) FILTER(WHERE status='queued'),'failed',count(*) FILTER(WHERE status='failed'),'unknown',count(*) FILTER(WHERE status='unknown'),'accepted',count(*) FILTER(WHERE status='accepted'),'overdue',count(*) FILTER(WHERE status='queued' AND due_at<now()-interval '5 minutes')) FROM notification_jobs`).Scan(&stats); e != nil {
			return e
		}
		result["stats"] = stats
		rows, e := tx.Query(r.Context(), "SELECT jsonb_build_object('id',id,'recipient',recipient,'purpose',purpose,'status',status,'failure_kind',failure_kind,'created_at',created_at) FROM email_delivery_records ORDER BY created_at DESC LIMIT 100")
		if e != nil {
			return e
		}
		defer rows.Close()
		auth := []map[string]any{}
		for rows.Next() {
			var item map[string]any
			if e := rows.Scan(&item); e != nil {
				return e
			}
			auth = append(auth, item)
		}
		result["authentication"] = auth
		result["smtp_configured"] = s.cfg.SMTPHost != "" && s.cfg.SMTPPort != "" && s.cfg.SMTPFrom != ""
		if s.MailStatus != nil {
			result["smtp_configured"] = s.MailStatus()["configured"]
		}
		return rows.Err()
	})
	if e != nil {
		return e
	}
	platform.JSON(w, 200, result)
	return nil
}

// retry 只允许暂时性确定失败任务重入队列，已受理、不确定和永久失败禁止重发。
func (s *Service) retry(w http.ResponseWriter, r *http.Request) error {
	e := s.systemTx(r.Context(), func(tx pgx.Tx) error {
		tag, e := tx.Exec(r.Context(), "UPDATE notification_jobs SET status='queued',due_at=now(),lease_until=NULL WHERE id::text=$1 AND status='failed' AND failure_kind IN('temporary','transport','configuration','template') AND due_at<now()-interval '1 minute'", r.PathValue("id"))
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return platform.Fail(409, "notice_retry_denied", "该邮件不能重试，请先核对投递状态与冷却时间")
		}
		return nil
	})
	if e != nil {
		return e
	}
	platform.JSON(w, 200, map[string]bool{"queued": true})
	return nil
}

// testMail 只向当前平台管理员本人发受控测试邮件，禁止指定任意地址。
func (s *Service) testMail(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Locale   string `json:"locale"`
		Timezone string `json:"timezone"`
	}
	if e := platform.Decode(w, r, &in); e != nil {
		return e
	}
	if !validLanguage(in.Locale, in.Timezone) {
		return platform.Fail(400, "invalid_notice_locale", "请选择有效的语言与IANA时区")
	}
	e := s.systemTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		var n int
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM notification_jobs WHERE user_id=$1 AND kind='test' AND created_at>now()-interval '1 hour'", platform.Current(r).UserID).Scan(&n); e != nil {
			return e
		}
		if n >= 12 {
			return platform.Fail(429, "notice_test_limit", "测试邮件过于频繁，请稍后再试")
		}
		return Enqueue(ctx, tx, Event{User: platform.Current(r).UserID, Kind: "test", Key: time.Now().UTC().Format(time.RFC3339Nano), Params: map[string]string{"name": in.Locale, "reference": in.Timezone, "_locale": in.Locale, "_timezone": in.Timezone}})
	})
	if e != nil {
		return e
	}
	platform.JSON(w, 202, map[string]bool{"queued": true})
	return nil
}

var recipientUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
