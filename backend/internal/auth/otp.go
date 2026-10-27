// 文件作用说明：实现按邮箱用途及可信IP持久限流的验证码申请、SMTP结果留痕和事务内原子消费。
package auth

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// requestIP 只使用连接来源地址，避免信任可伪造的转发头绕过限流。
func requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limit 持久增加固定时间窗口的计数，失败请求也不会因业务回滚丢失。
func (s *Service) limit(ctx context.Context, label, value string, window time.Duration, maximum int) error {
	seconds := int64(window / time.Second)
	bucket := time.Now().Unix() / seconds
	key := hex.EncodeToString(keyedDigest(s.secret, "rate", label, value, strconv.FormatInt(bucket, 10)))
	var attempts int
	err := s.pool.QueryRow(ctx, `INSERT INTO auth_rate_limits(key,attempts,expires_at) VALUES($1,1,$2) ON CONFLICT(key) DO UPDATE SET attempts=LEAST(auth_rate_limits.attempts+1,1000000) RETURNING attempts`, key, time.Unix((bucket+1)*seconds, 0)).Scan(&attempts)
	if err != nil {
		return err
	}
	if attempts > maximum {
		return platform.Fail(429, "rate_limited", "操作过于频繁，请稍后重试")
	}
	return nil
}

// limitAuthentication 为密码及验证码校验限制来源IP和目标邮箱，保护认证资源。
func (s *Service) limitAuthentication(r *http.Request, email string) error {
	if err := s.limit(r.Context(), "auth_ip", requestIP(r), 15*time.Minute, 60); err != nil {
		return err
	}
	return s.limit(r.Context(), "auth_email", email, 15*time.Minute, 20)
}

// sendCode 生成独立用途验证码并同步报告真实SMTP受理状态，不返回验证码。
func (s *Service) sendCode(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		return err
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return err
	}
	labels := map[string]string{"register": "注册", "login": "登录", "reset_password": "重置密码", "invitation": "接受邀请", "ownership": "移交所有者"}
	label, ok := labels[input.Purpose]
	if !ok {
		return platform.Fail(400, "invalid_purpose", "验证码用途无效")
	}
	if input.Purpose == "ownership" {
		principal, err := s.authenticate(r)
		if err != nil {
			return err
		}
		if principal.Email != email || principal.Role != "owner" {
			return platform.Fail(403, "owner_required", "只有当前租户所有者可以申请移交验证码")
		}
	}
	if err := s.mailer.validate(); err != nil {
		return err
	}
	if err := s.limit(r.Context(), "send_ip", requestIP(r), time.Hour, 30); err != nil {
		return err
	}
	if err := s.limit(r.Context(), "send_email", email, time.Hour, 6); err != nil {
		return err
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	var deliveryID string
	err = pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "challenge:"+email+":"+input.Purpose); err != nil {
			return err
		}
		var previous time.Time
		err := tx.QueryRow(r.Context(), "SELECT created_at FROM email_challenges WHERE email=$1 AND purpose=$2 ORDER BY created_at DESC LIMIT 1", email, input.Purpose).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && time.Since(previous) < 60*time.Second {
			return platform.Fail(429, "code_cooldown", "请等待60秒后重新获取验证码")
		}
		if _, err := tx.Exec(r.Context(), "UPDATE email_challenges SET revoked_at=now() WHERE email=$1 AND purpose=$2 AND consumed_at IS NULL AND revoked_at IS NULL", email, input.Purpose); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO email_challenges(id,email,purpose,code_hash,expires_at) VALUES($1,$2,$3,$4,now()+interval '5 minutes')", id, email, input.Purpose, keyedDigest(s.secret, "otp", id, email, input.Purpose, code)); err != nil {
			return err
		}
		return tx.QueryRow(r.Context(), "INSERT INTO email_delivery_records(kind,source_id,recipient,purpose) VALUES('challenge',$1,$2,$3) RETURNING id::text", id, email, input.Purpose).Scan(&deliveryID)
	})
	if err != nil {
		return err
	}
	result := s.mailer.send(r.Context(), email, "GoodChat "+label+"验证码", fmt.Sprintf("您的GoodChat%s验证码为：%s\n\n验证码5分钟内有效，仅限本次用途。重新申请后旧码失效。请勿转告他人。\n如果不是您本人操作，请忽略本邮件。", label, code))
	if err := s.recordDelivery(r.Context(), "challenge", id, deliveryID, result); err != nil {
		return err
	}
	if result.Status == "failed" {
		return platform.Fail(502, "mail_failed", "邮件发送失败，请稍后重试；未报告为发送成功")
	}
	status := 200
	if result.Status == "unknown" {
		status = 202
	}
	w.Header().Set("Retry-After", "60")
	platform.JSON(w, status, map[string]any{"delivery_status": result.Status, "expires_in": 300, "retry_after": 60})
	return nil
}

// recordDelivery 即使请求被取消也短暂记录最终状态，不保存敏感邮件载荷。
func (s *Service) recordDelivery(ctx context.Context, kind, sourceID, deliveryID string, result deliveryResult) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE email_delivery_records SET status=$1,failure_kind=$2,updated_at=now() WHERE id=$3", result.Status, result.FailureKind, deliveryID); err != nil {
			return err
		}
		query := "UPDATE email_challenges SET delivery_status=$1 WHERE id=$2"
		if kind == "invitation" {
			query = "UPDATE member_invitations SET delivery_status=$1 WHERE id=$2"
		}
		_, err := tx.Exec(ctx, query, result.Status, sourceID)
		return err
	})
}

// withCode 将成功消费与业务提交放在同一事务，错误次数则单独提交后返回错误。
func (s *Service) withCode(ctx context.Context, email, purpose, code string, action func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "challenge:"+email+":"+purpose); err != nil {
		return err
	}
	var id, status string
	var expected []byte
	var expires time.Time
	var failures int
	var consumed, revoked *time.Time
	err = tx.QueryRow(ctx, `SELECT id::text,code_hash,expires_at,failed_attempts,consumed_at,revoked_at,delivery_status FROM email_challenges WHERE email=$1 AND purpose=$2 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, email, purpose).Scan(&id, &expected, &expires, &failures, &consumed, &revoked, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.Fail(400, "invalid_code", "验证码无效或已过期，请重新获取")
	}
	if err != nil {
		return err
	}
	if consumed != nil || revoked != nil || !time.Now().Before(expires) || failures >= 5 || (status != "accepted" && status != "unknown") {
		return platform.Fail(400, "invalid_code", "验证码无效或已过期，请重新获取")
	}
	actual := keyedDigest(s.secret, "otp", id, email, purpose, strings.TrimSpace(code))
	valid := len(strings.TrimSpace(code)) == 6 && subtle.ConstantTimeCompare(expected, actual) == 1
	if !valid {
		if _, err := tx.Exec(ctx, "UPDATE email_challenges SET failed_attempts=failed_attempts+1 WHERE id=$1", id); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return platform.Fail(400, "invalid_code", "验证码无效或已过期，请重新获取")
	}
	if _, err := tx.Exec(ctx, "UPDATE email_challenges SET consumed_at=now() WHERE id=$1", id); err != nil {
		return err
	}
	if err := action(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
