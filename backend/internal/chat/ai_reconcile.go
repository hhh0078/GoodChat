// 文件作用说明：为平台提供未知供应商用量的人工核对，不推测费用或对失败回复强制向租户收费。
package chat

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiPlatformJobs 通过显式平台权限在指定租户事务中读取计费审计，密钥快照不对外返回。
func (s *Service) aiPlatformJobs(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("id")
	if !uuidPattern.MatchString(tenant) {
		returnError(w, r, "invalid_id", "记录不存在或无权访问")
		return
	}
	var items []map[string]any
	e := platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		rows, e := tx.Query(r.Context(), `SELECT to_jsonb(j)-'snapshot'-'result'||jsonb_build_object('attempts',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY call_index),'[]'::jsonb) FROM ai_call_attempts a WHERE a.job_id=j.id)) FROM ai_jobs j ORDER BY created_at DESC LIMIT 100`)
		if e != nil {
			return e
		}
		items, e = jsonRows(rows)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items})
}

// aiReconcile 接受供应商实际用量或平台承担未知成本，均释放预占而不补扣失败任务。
func (s *Service) aiReconcile(w http.ResponseWriter, r *http.Request) {
	tenant, id := r.PathValue("id"), r.PathValue("job")
	var in struct {
		Action   string `json:"action"`
		Reason   string `json:"reason"`
		Key      string `json:"client_key"`
		Attempts []struct {
			Index  int   `json:"call_index"`
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
		} `json:"attempts"`
	}
	if e := platform.Decode(w, r, &in); e != nil {
		replyError(w, r, e)
		return
	}
	if !uuidPattern.MatchString(tenant) || !uuidPattern.MatchString(id) || !uuidPattern.MatchString(in.Key) || !inStrings(in.Action, "report", "waive") || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 500 {
		returnError(w, r, "ai_reconcile_invalid", "请填写核对说明和实际用量")
		return
	}
	e := platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		ctx := r.Context()
		var status string
		if e := tx.QueryRow(ctx, "SELECT status FROM ai_jobs WHERE id=$1 FOR UPDATE", id).Scan(&status); e != nil {
			return e
		}
		if status != "uncertain" {
			var exists bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_wallet_entries WHERE job_id=$1 AND client_key=$2 AND reason=$3)", id, in.Key, in.Reason).Scan(&exists); e != nil {
				return e
			}
			if exists {
				return nil
			}
			return platform.Fail(409, "ai_reconcile_conflict", "任务状态已变化，请刷新后重试")
		}
		billing := "waived_unknown_cost"
		if in.Action == "report" {
			if len(in.Attempts) == 0 || len(in.Attempts) > 4 {
				return platform.Fail(400, "ai_reconcile_invalid", "请填写核对说明和实际用量")
			}
			seen := map[int]bool{}
			for _, a := range in.Attempts {
				if a.Index < 1 || a.Index > 4 || seen[a.Index] || a.Input < 0 || a.Output < 0 || a.Input > 1000000 || a.Output > 1000000 {
					return platform.Fail(400, "ai_reconcile_invalid", "请填写核对说明和实际用量")
				}
				seen[a.Index] = true
				var raw []byte
				if e := tx.QueryRow(ctx, `SELECT v.config FROM ai_call_attempts a JOIN ai_chat_model_versions v ON v.model_id=a.model_id AND v.version=a.model_version WHERE a.job_id=$1 AND a.call_index=$2 AND a.status IN('started','uncertain')`, id, a.Index).Scan(&raw); e != nil {
					return e
				}
				var m aiModel
				if e := json.Unmarshal(raw, &m); e != nil {
					return e
				}
				cost, e := aiAmount(a.Input, a.Output, m.CostInput, m.CostOutput)
				if e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, "UPDATE ai_call_attempts SET input_tokens=$3,output_tokens=$4,cost_micro=$5,sale_micro=0,status='reconciled',completed_at=now() WHERE job_id=$1 AND call_index=$2", id, a.Index, a.Input, a.Output, cost); e != nil {
					return e
				}
			}
			var pending bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_call_attempts WHERE job_id=$1 AND status IN('started','uncertain'))", id).Scan(&pending); e != nil {
				return e
			}
			if pending {
				return platform.Fail(400, "ai_reconcile_invalid", "请填写核对说明和实际用量")
			}
			if _, e := tx.Exec(ctx, `UPDATE ai_jobs SET input_tokens=(SELECT coalesce(sum(input_tokens),0) FROM ai_call_attempts WHERE job_id=$1),output_tokens=(SELECT coalesce(sum(output_tokens),0) FROM ai_call_attempts WHERE job_id=$1),cost_micro=(SELECT coalesce(sum(cost_micro),0) FROM ai_call_attempts WHERE job_id=$1) WHERE id=$1`, id); e != nil {
				return e
			}
			billing = "reconciled"
		}
		j := aiJobData{ID: id, Tenant: tenant}
		if e := settleAIWallet(ctx, tx, &j, false); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO ai_wallet_entries(tenant_id,job_id,delta_micro,kind,reason,client_key,created_by) VALUES($1,$2,0,'adjustment',$3,$4,$5)`, tenant, id, in.Reason, in.Key, platform.Current(r).UserID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "UPDATE ai_jobs SET status='failed',billing_status=$2,error_code='ai_reconciled',completed_at=now() WHERE id=$1", id, billing)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"reconciled": true})
}
