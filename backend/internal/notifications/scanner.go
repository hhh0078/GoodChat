// 文件作用说明：从已提交账本和业务状态产生一次性通知，避免历史数据上线后群发与重复扣费。
package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// scan 每分钟串行扫描有效租户，使用事务锁防止多个实例重复产生业务提醒。
func (s *Service) scan(ctx context.Context) {
	rows, e := s.pool.Query(ctx, "SELECT id::text FROM tenants WHERE status='active' ORDER BY id")
	if e != nil {
		slog.Error("邮件业务扫描失败", "error", e)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e != nil {
		return
	}
	for _, id := range ids {
		if e := s.scanTenant(ctx, id); e != nil && ctx.Err() == nil {
			slog.Error("租户邮件扫描失败", "error", e)
		}
	}
}

// scanTenant 在同事务保存去重状态和任务，扫描失败不会丢失业务事件。
func (s *Service) scanTenant(ctx context.Context, tenant string) error {
	return s.systemTx(ctx, func(tx pgx.Tx) error {
		var locked bool
		if e := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))", "notice-scan:"+tenant).Scan(&locked); e != nil || !locked {
			return e
		}
		if _, e := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "INSERT INTO notification_settings(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", tenant); e != nil {
			return e
		}
		var name string
		var installed time.Time
		if e := tx.QueryRow(ctx, "SELECT name,(SELECT (value->>'at')::timestamptz FROM notification_state WHERE key='installed') FROM tenants WHERE id=$1", tenant).Scan(&name, &installed); e != nil {
			return e
		}
		if e := s.scanWallet(ctx, tx, tenant, name); e != nil {
			return e
		}
		if e := s.scanSubscription(ctx, tx, tenant, name); e != nil {
			return e
		}
		if e := s.scanService(ctx, tx, tenant, name, installed); e != nil {
			return e
		}
		return s.scanDigest(ctx, tx, tenant, name, installed)
	})
}

// adminIDs 获取默认所有者及企业明确指定的有效管理员，禁止任意邮箱发送。
func adminIDs(ctx context.Context, tx pgx.Tx, tenant string) ([]string, error) {
	rows, e := tx.Query(ctx, `SELECT m.user_id::text FROM tenant_memberships m JOIN notification_settings n ON n.tenant_id=m.tenant_id WHERE m.tenant_id=$1 AND NOT m.disabled AND m.removed_at IS NULL AND (m.role='owner' OR m.role='admin' AND n.recipient_ids ? m.user_id::text)`, tenant)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if e := rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// enqueueAdmins 为同一业务事件保存每位收件人的独立幂等任务。
func enqueueAdmins(ctx context.Context, tx pgx.Tx, tenant, name, kind, key string, params map[string]string) error {
	ids, e := adminIDs(ctx, tx, tenant)
	if e != nil {
		return e
	}
	if params == nil {
		params = map[string]string{}
	}
	params["tenant"] = name
	for _, id := range ids {
		if e := Enqueue(ctx, tx, Event{tenant, id, kind, key, params}); e != nil {
			return e
		}
	}
	return nil
}

// scanWallet 只处理安装后的真实流水；零余额新租户不会凭空收到充值催促。
func (s *Service) scanWallet(ctx context.Context, tx pgx.Tx, tenant, name string) error {
	rows, e := tx.Query(ctx, `SELECT e.id::text,e.kind,e.delta_micro,e.created_at FROM notification_sources n JOIN ai_wallet_entries e ON e.id=n.source_id WHERE n.processed_at IS NULL ORDER BY n.created_at,n.source_id LIMIT 200`)
	if e != nil {
		return e
	}
	type entry struct {
		id, kind string
		amount   int64
		at       time.Time
	}
	items := []entry{}
	for rows.Next() {
		var i entry
		if e := rows.Scan(&i.id, &i.kind, &i.amount, &i.at); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	rows.Close()
	if e := rows.Err(); e != nil {
		return e
	}
	charged := false
	for _, i := range items {
		if i.kind == "topup" {
			if e := enqueueAdmins(ctx, tx, tenant, name, "topup", "topup:"+i.id, map[string]string{"amount": fmt.Sprintf("CNY %.6f", float64(i.amount)/1000000), "reference": i.id, "occurred_at": i.at.UTC().Format(time.RFC3339)}); e != nil {
				return e
			}
		}
		charged = charged || i.kind == "charge" || i.amount < 0
		if _, e := tx.Exec(ctx, "UPDATE notification_sources SET processed_at=statement_timestamp() WHERE source_id=$1", i.id); e != nil {
			return e
		}
	}
	var balance, low, critical int64
	e = tx.QueryRow(ctx, "SELECT w.balance_micro,n.low_micro,n.critical_micro FROM ai_wallets w JOIN notification_settings n ON n.tenant_id=w.tenant_id WHERE w.tenant_id=$1", tenant).Scan(&balance, &low, &critical)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	level := 0
	if balance <= low {
		level = 1
	}
	if balance <= critical {
		level = 2
	}
	var old int
	e = tx.QueryRow(ctx, "SELECT (value->>'level')::int FROM notification_state WHERE key=$1", "balance:"+tenant).Scan(&old)
	if e != nil && e != pgx.ErrNoRows {
		return e
	}
	if e == pgx.ErrNoRows {
		e = nil
	}
	if charged && level > old {
		kind := "balance_low"
		if level == 2 {
			kind = "balance_critical"
		}
		last := items[len(items)-1].id
		if e := enqueueAdmins(ctx, tx, tenant, name, kind, "balance:"+last, map[string]string{"balance": fmt.Sprintf("CNY %.6f", float64(balance)/1000000)}); e != nil {
			return e
		}
	}
	// 无流水时保持边界；充值后恢复级别，为下次真正下降重新开启提醒。
	if len(items) > 0 {
		if !charged && level > old {
			level = old
		}
		_, e = tx.Exec(ctx, "INSERT INTO notification_state(key,value) VALUES($1,jsonb_build_object('level',$2::int)) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=now()", "balance:"+tenant, level)
	}
	return e
}

// scanSubscription 按每个收件人的本地日历安排7/3/1天提醒，续费旧任务由执行器撤销。
func (s *Service) scanSubscription(ctx context.Context, tx pgx.Tx, tenant, name string) error {
	var expires *time.Time
	var started, installed time.Time
	var code string
	e := tx.QueryRow(ctx, "SELECT expires_at,started_at,plan_snapshot->>'code',(SELECT (value->>'at')::timestamptz FROM notification_state WHERE key='installed') FROM tenant_subscriptions WHERE tenant_id=$1", tenant).Scan(&expires, &started, &code, &installed)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if expires == nil || code == "free" {
		return nil
	}
	raw := expires.UTC().Format(time.RFC3339Nano)
	params := map[string]string{"tenant": name, "expires_at": expires.UTC().Format(time.RFC3339), "_expires": raw}
	if !started.Before(installed) {
		if e := enqueueAdmins(ctx, tx, tenant, name, "subscription_active", "subscription:"+started.UTC().Format(time.RFC3339Nano), params); e != nil {
			return e
		}
	}
	ids, e := adminIDs(ctx, tx, tenant)
	if e != nil {
		return e
	}
	for _, id := range ids {
		var zone string
		if e := tx.QueryRow(ctx, "SELECT coalesce(nullif(u.display_timezone,''),nullif(t.timezone,''),'UTC') FROM users u CROSS JOIN tenants t WHERE u.id=$1 AND t.id=$2", id, tenant).Scan(&zone); e != nil {
			return e
		}
		l, e := time.LoadLocation(zone)
		if e != nil {
			l = time.UTC
		}
		local := expires.In(l)
		for _, days := range []int{7, 3, 1, 0} {
			due := time.Date(local.Year(), local.Month(), local.Day()-days, 9, 0, 0, 0, l)
			kind := "subscription_expiring"
			if days == 0 {
				due = *expires
				kind = "subscription_expired"
			}
			if due.Before(time.Now()) && days > 0 {
				continue
			}
			if days == 0 && due.Before(installed) {
				continue
			}
			values := map[string]string{}
			for k, v := range params {
				values[k] = v
			}
			values["_not_before"] = due.UTC().Format(time.RFC3339Nano)
			if e := Enqueue(ctx, tx, Event{tenant, id, kind, fmt.Sprintf("expiry:%s:%d", raw, days), values}); e != nil {
				return e
			}
		}
	}
	return nil
}

// scanService 合并预算受阻与供应商故障，并只提醒仍未获人工回复的真实转人工事件。
func (s *Service) scanService(ctx context.Context, tx pgx.Tx, tenant, name string, installed time.Time) error {
	var enabled bool
	var minutes int
	if e := tx.QueryRow(ctx, "SELECT service_enabled,handoff_minutes FROM notification_settings WHERE tenant_id=$1", tenant).Scan(&enabled, &minutes); e != nil {
		return e
	}
	var blocked bool
	if e := tx.QueryRow(ctx, blockedSQL, tenant, installed).Scan(&blocked); e != nil {
		return e
	}
	if blocked {
		if e := enqueueAdmins(ctx, tx, tenant, name, "ai_blocked", "blocked:"+time.Now().UTC().Format("2006-01-02"), nil); e != nil {
			return e
		}
	}
	if !enabled {
		return nil
	}
	var errors int
	if e := tx.QueryRow(ctx, "SELECT count(*) FROM ai_jobs WHERE status IN('failed','uncertain') AND error_code NOT IN('ai_insufficient_balance','ai_daily_limit','ai_stale') AND completed_at>greatest($1,now()-interval '1 hour')", installed).Scan(&errors); e != nil {
		return e
	}
	if errors >= 3 {
		key := "errors:" + time.Now().UTC().Format("2006-01-02T15")
		if e := enqueueAdmins(ctx, tx, tenant, name, "ai_errors", key, nil); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, "SELECT id::text FROM users WHERE is_platform_admin")
		if e != nil {
			return e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e := rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if e := Enqueue(ctx, tx, Event{User: id, Kind: "ai_errors", Key: tenant + ":" + key, Params: map[string]string{"tenant": name, "_platform": "1"}}); e != nil {
				return e
			}
		}
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT ON(e.conversation_id) e.conversation_id::text,e.created_at FROM conversation_events e JOIN conversations c ON c.id=e.conversation_id WHERE e.event_type='conversation.updated' AND e.payload ? 'ai_reason' AND e.created_at>=$1 AND e.created_at<now()-make_interval(mins=>$2) AND c.ai_paused AND c.status<>'closed' AND NOT EXISTS(SELECT 1 FROM messages m WHERE m.conversation_id=c.id AND m.sender_kind='agent' AND m.ai_job_id IS NULL AND m.created_at>e.created_at) ORDER BY e.conversation_id,e.created_at DESC`, installed, minutes)
	if e != nil {
		return e
	}
	type handoff struct {
		id string
		at time.Time
	}
	items := []handoff{}
	for rows.Next() {
		var i handoff
		if e := rows.Scan(&i.id, &i.at); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	rows.Close()
	for _, i := range items {
		if e := enqueueAdmins(ctx, tx, tenant, name, "handoff_waiting", "handoff:"+i.id+":"+i.at.UTC().Format(time.RFC3339Nano), map[string]string{"reference": i.id, "_handoff_at": i.at.UTC().Format(time.RFC3339Nano)}); e != nil {
			return e
		}
	}
	return rows.Err()
}

// scanDigest 只向主动订阅成员发送本地周一摘要与一次接入引导，默认不群发。
func (s *Service) scanDigest(ctx context.Context, tx pgx.Tx, tenant, name string, installed time.Time) error {
	rows, e := tx.Query(ctx, `SELECT u.id::text,coalesce(nullif(u.display_timezone,''),t.timezone,'UTC'),n.digest_enabled,n.updates_enabled,t.created_at,m.id::text,m.role IN('owner','admin') FROM tenant_memberships m JOIN users u ON u.id=m.user_id JOIN mail_notification_preferences n ON n.user_id=u.id JOIN tenants t ON t.id=m.tenant_id WHERE m.tenant_id=$1 AND NOT m.disabled AND m.removed_at IS NULL AND (n.digest_enabled OR n.updates_enabled)`, tenant)
	if e != nil {
		return e
	}
	type person struct {
		member          string
		admin           bool
		id, zone        string
		digest, updates bool
		created         time.Time
	}
	people := []person{}
	for rows.Next() {
		var p person
		if e := rows.Scan(&p.id, &p.zone, &p.digest, &p.updates, &p.created, &p.member, &p.admin); e != nil {
			rows.Close()
			return e
		}
		people = append(people, p)
	}
	rows.Close()
	if e := rows.Err(); e != nil {
		return e
	}
	for _, p := range people {
		l, e := time.LoadLocation(p.zone)
		if e != nil {
			l = time.UTC
		}
		at := time.Now().In(l)
		if p.digest && at.Weekday() == time.Monday && at.Hour() == 9 {
			sessions, tokens, e := digestUsage(ctx, tx, p.member, p.admin)
			if e != nil {
				return e
			}
			if e := Enqueue(ctx, tx, Event{tenant, p.id, "digest", "digest:" + at.Format("2006-01-02"), map[string]string{"tenant": name, "sessions": strconv.FormatInt(sessions, 10), "tokens": strconv.FormatInt(tokens, 10)}}); e != nil {
				return e
			}
		}
		if p.updates && p.created.After(installed) && time.Since(p.created) > 24*time.Hour && time.Since(p.created) < 7*24*time.Hour {
			if e := Enqueue(ctx, tx, Event{tenant, p.id, "onboarding", "onboarding:" + tenant, map[string]string{"tenant": name}}); e != nil {
				return e
			}
		}
	}
	return nil
}

// blockedSQL 仅保留尚未被充值恢复、且本地预算日仍有效的任务失败。
const blockedSQL = `SELECT EXISTS(SELECT 1 FROM ai_jobs j JOIN tenants t ON t.id=j.tenant_id WHERE j.tenant_id=$1 AND j.error_code IN('ai_insufficient_balance','ai_daily_limit') AND j.completed_at>greatest($2::timestamptz,now()-interval '1 hour') AND (j.error_code<>'ai_daily_limit' OR (j.completed_at AT TIME ZONE t.timezone)::date=(now() AT TIME ZONE t.timezone)::date) AND (j.error_code<>'ai_insufficient_balance' OR NOT EXISTS(SELECT 1 FROM ai_wallet_entries e WHERE e.kind='topup' AND e.created_at>j.completed_at)))`

// digestUsage 与工作端权限一致，只向普通客服统计本人接待及本人AI用量。
func digestUsage(ctx context.Context, tx pgx.Tx, member string, admin bool) (int64, int64, error) {
	var sessions, tokens int64
	e := tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM conversations WHERE created_at>now()-interval '7 days' AND ($2 OR current_agent_id=$1::uuid)),coalesce((SELECT sum(input_tokens+output_tokens) FROM ai_jobs WHERE created_at>now()-interval '7 days' AND ($2 OR owner_membership_id=$1::uuid)),0)", member, admin).Scan(&sessions, &tokens)
	return sessions, tokens, e
}
