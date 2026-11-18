// 文件作用说明：以整数微元预占和结算客服AI费用，保留供应商调用及未知计费审计。
package chat

import (
	"context"
	"math/big"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiAmount 统一按百万Token费率计算并向上取整到微元，拒绝负值和溢出。
func aiAmount(input, output, inRate, outRate int64) (int64, error) {
	if input < 0 || output < 0 || inRate < 0 || outRate < 0 {
		return 0, platform.Fail(400, "ai_invalid_usage", "AI用量需要核对")
	}
	v := new(big.Int).Mul(big.NewInt(input), big.NewInt(inRate))
	v.Add(v, new(big.Int).Mul(big.NewInt(output), big.NewInt(outRate)))
	v.Add(v, big.NewInt(999999))
	v.Div(v, big.NewInt(1000000))
	if !v.IsInt64() || v.Int64() > 1000000000000000 {
		return 0, platform.Fail(400, "ai_invalid_usage", "AI用量需要核对")
	}
	return v.Int64(), nil
}

// reserveAIWallet 以钱包行锁防止多个工作器透支，同步验证机器人本地自然日预算。
func reserveAIWallet(ctx context.Context, tx pgx.Tx, j *aiJobData) error {
	rounds := int64(4)
	if j.Kind == "summary" {
		rounds = 1
	}
	reserve, e := aiAmount(int64(j.Snapshot.Model.MaxInput)*rounds, int64(j.Snapshot.Model.MaxOutput)*rounds, j.Snapshot.Model.SaleInput, j.Snapshot.Model.SaleOutput)
	if e != nil {
		return e
	}
	var budget int64
	if j.BotID != "" {
		if e = tx.QueryRow(ctx, "SELECT (settings->>'daily_budget_micro')::bigint FROM ai_bots WHERE id=$1 FOR NO KEY UPDATE", j.BotID).Scan(&budget); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO ai_wallets(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", j.Tenant); e != nil {
		return e
	}
	var available int64
	var unlimited bool
	if e = tx.QueryRow(ctx, "SELECT balance_micro-reserved_micro,unlimited,now() FROM ai_wallets FOR UPDATE").Scan(&available, &unlimited, &j.Snapshot.BudgetAt); e != nil {
		return e
	}
	if !unlimited && reserve > available {
		return platform.Fail(402, "ai_insufficient_balance", "AI余额不足")
	}
	if j.BotID != "" {
		var used int64
		var count int
		e = tx.QueryRow(ctx, `SELECT coalesce(sum(CASE WHEN status IN('running','uncertain') THEN reserved_micro WHEN status IN('succeeded','draft') THEN sale_micro ELSE 0 END),0),count(*) FROM ai_jobs WHERE bot_id=$1 AND id<>$2 AND coalesce((snapshot->>'budget_at')::timestamptz,created_at)>=(date_trunc('day',now() AT TIME ZONE (SELECT timezone FROM tenants WHERE id=$3)) AT TIME ZONE (SELECT timezone FROM tenants WHERE id=$3))`, j.BotID, j.ID, j.Tenant).Scan(&used, &count)
		if e != nil {
			return e
		}
		if budget > 0 && budget < used+reserve || count >= 1000 {
			return platform.Fail(402, "ai_daily_limit", "AI每日额度已用完")
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE ai_wallets SET reserved_micro=reserved_micro+$1,updated_at=now()", reserve); e != nil {
		return e
	}
	j.Reserved = reserve
	_, e = tx.Exec(ctx, "UPDATE ai_jobs SET reserved_micro=$2,snapshot=$3 WHERE id=$1", j.ID, reserve, jsonValue(j.Snapshot))
	return e
}

// settleAIWallet 在消息提交事务内幂等扣费，取消结果只释放预算并保留成本记录。
func settleAIWallet(ctx context.Context, tx pgx.Tx, j *aiJobData, charge bool) error {
	var reserve, sale int64
	var status string
	if e := tx.QueryRow(ctx, "SELECT reserved_micro,sale_micro,status FROM ai_jobs WHERE id=$1 FOR UPDATE", j.ID).Scan(&reserve, &sale, &status); e != nil {
		return e
	}
	if status != "running" && status != "draft" && status != "uncertain" {
		return nil
	}
	if !charge {
		sale = 0
	}
	if sale > reserve {
		return platform.Fail(409, "ai_settlement_uncertain", "AI用量需要核对")
	}
	tag, e := tx.Exec(ctx, "UPDATE ai_wallets SET reserved_micro=reserved_micro-$1,balance_micro=balance_micro-$2,updated_at=now() WHERE reserved_micro>=$1 AND (unlimited OR balance_micro>=$2)", reserve, sale)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 && reserve > 0 {
		return platform.Fail(409, "ai_settlement_uncertain", "AI用量需要核对")
	}
	if charge {
		if _, e := tx.Exec(ctx, `INSERT INTO ai_wallet_entries(tenant_id,job_id,delta_micro,kind,reason,client_key) VALUES($1,$2,-$3::bigint,'charge',$4,$5) ON CONFLICT(tenant_id,client_key) DO NOTHING`, j.Tenant, j.ID, sale, j.Kind, "job:"+j.ID); e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, "UPDATE ai_jobs SET reserved_micro=0 WHERE id=$1", j.ID)
	return e
}

// aiWalletMode 仅允许平台为指定租户开启不限制余额的运营模式，费用仍按真实用量结算。
func (s *Service) aiWalletMode(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("id")
	var in struct {
		Unlimited bool `json:"unlimited"`
	}
	if !uuidPattern.MatchString(tenant) {
		returnError(w, r, "invalid_id", "资源编号格式不正确")
		return
	}
	if err := platform.Decode(w, r, &in); err != nil {
		replyError(w, r, err)
		return
	}
	err := platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		if err := tenantActive(r.Context(), tx, tenant); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO ai_wallets(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", tenant); err != nil {
			return err
		}
		var balance, reserved int64
		var previous bool
		if err := tx.QueryRow(r.Context(), "SELECT balance_micro,reserved_micro,unlimited FROM ai_wallets FOR UPDATE").Scan(&balance, &reserved, &previous); err != nil {
			return err
		}
		if !in.Unlimited && (balance < 0 || reserved > balance) {
			return platform.Fail(409, "ai_insufficient_balance", "AI余额不足")
		}
		if previous == in.Unlimited {
			return nil
		}
		if _, err := tx.Exec(r.Context(), "UPDATE ai_wallets SET unlimited=$1,updated_at=now()", in.Unlimited); err != nil {
			return err
		}
		reason := "平台关闭不限制余额运营模式"
		if in.Unlimited {
			reason = "平台开启不限制余额运营模式"
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO ai_wallet_entries(tenant_id,delta_micro,kind,reason,client_key,created_by) VALUES($1,0,'adjustment',$2,$3,$4)`, tenant, reason, "mode:"+newID(), platform.Current(r).UserID)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"unlimited": in.Unlimited})
}

// aiCredit 只允许平台管理员人工充值，幂等键与原始金额不一致时拒绝。
func (s *Service) aiCredit(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("id")
	var in struct {
		Amount int64  `json:"amount_micro"`
		Reason string `json:"reason"`
		Key    string `json:"client_key"`
	}
	if e := platform.Decode(w, r, &in); e != nil {
		replyError(w, r, e)
		return
	}
	if !uuidPattern.MatchString(tenant) || !uuidPattern.MatchString(in.Key) || in.Amount <= 0 || in.Amount > 1000000000000 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 500 {
		returnError(w, r, "ai_credit_invalid", "请检查充值金额和原因")
		return
	}
	e := platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, e := tx.Exec(ctx, "INSERT INTO ai_wallets(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", tenant); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "SELECT tenant_id FROM ai_wallets FOR UPDATE"); e != nil {
			return e
		}
		var amount int64
		var reason string
		e := tx.QueryRow(ctx, "SELECT delta_micro,reason FROM ai_wallet_entries WHERE client_key=$1", in.Key).Scan(&amount, &reason)
		if e == nil {
			if amount != in.Amount || reason != in.Reason {
				return platform.Fail(409, "idempotency_conflict", "相同编号携带了不同内容")
			}
			return nil
		}
		if e != pgx.ErrNoRows {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO ai_wallet_entries(tenant_id,delta_micro,kind,reason,client_key,created_by) VALUES($1,$2,'topup',$3,$4,$5)`, tenant, in.Amount, in.Reason, in.Key, platform.Current(r).UserID); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE ai_wallets SET balance_micro=balance_micro+$1,updated_at=now()", in.Amount)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"credited": true})
}

// aiUsage 返回租户钱包和按成员授权的用量，供应商成本不向租户公开。
func (s *Service) aiUsage(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var wallet any
	var jobs, entries []map[string]any
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		var e error
		if isAdmin(a) {
			wallet, e = txJSON(ctx, tx, "SELECT to_jsonb(w) FROM ai_wallets w")
			if e == pgx.ErrNoRows {
				wallet = map[string]any{"balance_micro": 0, "reserved_micro": 0}
				e = nil
			}
			if e != nil {
				return e
			}
			rows, e := tx.Query(ctx, "SELECT to_jsonb(e) FROM ai_wallet_entries e ORDER BY created_at DESC LIMIT 100")
			if e != nil {
				return e
			}
			entries, e = jsonRows(rows)
			if e != nil {
				return e
			}
		}
		rows, e := tx.Query(ctx, `SELECT (to_jsonb(j)-'snapshot'-'result'-'cost_micro')||jsonb_build_object('charged_micro',coalesce((SELECT -sum(e.delta_micro) FROM ai_wallet_entries e WHERE e.job_id=j.id AND e.kind='charge'),0),'owner_name',(SELECT coalesce(nullif(m.display_name,''),u.display_name) FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.id=j.owner_membership_id)) FROM ai_jobs j WHERE ($1 OR owner_membership_id=$2) ORDER BY created_at DESC LIMIT 100`, isAdmin(a), a.MemberID)
		if e != nil {
			return e
		}
		jobs, e = jsonRows(rows)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"wallet": wallet, "jobs": jobs, "entries": entries})
}

// aiPlatformUsage 按租户事务汇总真实成本、收入、未知调用和余额，不越过租户RLS。
func (s *Service) aiPlatformUsage(w http.ResponseWriter, r *http.Request) {
	rows, e := s.pool.Query(r.Context(), "SELECT id::text FROM tenants ORDER BY created_at DESC LIMIT 500")
	if e != nil {
		replyError(w, r, e)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			replyError(w, r, e)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		replyError(w, r, e)
		return
	}
	items := []map[string]any{}
	for _, tenant := range ids {
		e = platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
			item, e := txJSON(r.Context(), tx, `SELECT jsonb_build_object('tenant_id',($1::uuid)::text,'tenant_name',(SELECT name FROM tenants WHERE id=$1::uuid),'balance_micro',coalesce((SELECT balance_micro FROM ai_wallets),0),'reserved_micro',coalesce((SELECT reserved_micro FROM ai_wallets),0),'unlimited',coalesce((SELECT unlimited FROM ai_wallets),false),'cost_micro',coalesce((SELECT sum(cost_micro) FROM ai_jobs),0),'revenue_micro',coalesce((SELECT -sum(delta_micro) FROM ai_wallet_entries WHERE kind='charge'),0),'uncertain',(SELECT count(*) FROM ai_jobs WHERE status='uncertain'),'unknown_cost',(SELECT count(*) FROM ai_jobs WHERE billing_status IN('uncertain','waived_unknown_cost')))`, tenant)
			if e == nil {
				items = append(items, item)
			}
			return e
		})
		if e != nil {
			replyError(w, r, e)
			return
		}
	}
	platform.JSON(w, 200, map[string]any{"items": items})
}

// aiWaiveProtocolAttempt 由平台承担首次格式修复前的无效调用，保留真实用量成本但不重复向租户计价。
func (s *Service) aiWaiveProtocolAttempt(ctx context.Context, j *aiJobData, index int) error {
	return platform.TenantTx(ctx, s.pool, j.Tenant, func(tx pgx.Tx) error {
		var sale int64
		if e := tx.QueryRow(ctx, "SELECT sale_micro FROM ai_call_attempts WHERE job_id=$1 AND call_index=$2 AND status='reported' FOR UPDATE", j.ID, index).Scan(&sale); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE ai_call_attempts SET sale_micro=0,response_meta=response_meta||'{"platform_covered":true}'::jsonb WHERE job_id=$1 AND call_index=$2`, j.ID, index); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "UPDATE ai_jobs SET sale_micro=sale_micro-$2 WHERE id=$1", j.ID, sale)
		return e
	})
}
