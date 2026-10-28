// Package presence 合并同一成员多标签在线租约，保存可按时间范围汇总的真实在线区间。
package presence

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Renew 在已鉴权的租户事务内记录一次成功探活，45秒内的重叠连接共用同一在线区间。
// tenant、member 必须来自认证上下文；不修改上班、忙碌或接待占用状态。
func Renew(ctx context.Context, tx pgx.Tx, tenant, member string) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('goodchat.presence:'||$1::text||':'||$2::text,0))", tenant, member); err != nil {
		return err
	}
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	var id int64
	var until time.Time
	err := tx.QueryRow(ctx, "SELECT id,online_until FROM member_presence_intervals WHERE tenant_id=$1 AND membership_id=$2 ORDER BY online_until DESC LIMIT 1 FOR UPDATE", tenant, member).Scan(&id, &until)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil && !until.Before(now) {
		_, err = tx.Exec(ctx, "UPDATE member_presence_intervals SET last_seen_at=greatest(last_seen_at,$2),online_until=greatest(online_until,$3) WHERE id=$1", id, now, now.Add(45*time.Second))
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO member_presence_intervals(tenant_id,membership_id,started_at,last_seen_at,online_until) VALUES($1,$2,$3,$3,$4)", tenant, member, now, now.Add(45*time.Second))
	return err
}
