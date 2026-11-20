// 文件作用说明：持久化脱敏的模型与工具输入输出，并按租户及机器人所有权提供可轮询的调用链路。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiTraceMessage 只记录可观察的消息和工具调用，不读取供应商内部推理或额外私有字段。
func aiTraceMessage(m *schema.Message) any {
	if m == nil {
		return nil
	}
	return map[string]any{"role": m.Role, "content": m.Content, "tool_calls": m.ToolCalls, "tool_call_id": m.ToolCallID, "parts": m.UserInputMultiContent}
}

// aiTraceSafe 在写入前递归剔除密钥、认证信息与图片内联字节，保留明确脱敏标记。
func aiTraceSafe(value any) any {
	raw, e := json.Marshal(value)
	if e != nil {
		return map[string]any{"unavailable": true}
	}
	var data any
	if json.Unmarshal(raw, &data) != nil {
		return map[string]any{"unavailable": true}
	}
	return aiTraceRedact(data)
}

// aiTraceRedact 按字段语义过滤秘密，避免将模型额外配置或内联图片存入业务日志。
func aiTraceRedact(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			key := strings.ToLower(strings.ReplaceAll(k, "_", ""))
			if strings.Contains(key, "apikey") || strings.Contains(key, "authorization") || strings.Contains(key, "base64") || strings.Contains(key, "reasoning") || strings.Contains(key, "secret") {
				v[k] = "[已脱敏]"
			} else {
				v[k] = aiTraceRedact(x)
			}
		}
	case []any:
		for i, x := range v {
			v[i] = aiTraceRedact(x)
		}
	case string:
		if strings.HasPrefix(v, "data:") {
			return "[内联图片已省略]"
		}
	}
	return value
}

// aiTraceTx 将执行事实与对应业务状态置于同一租户事务，写入失败时不伪造成功记录。
func aiTraceTx(ctx context.Context, tx pgx.Tx, j *aiJobData, kind, name string, payload any) error {
	_, e := tx.Exec(ctx, "INSERT INTO ai_job_events(tenant_id,job_id,kind,name,payload) VALUES($1,$2,$3,$4,$5)", j.Tenant, j.ID, kind, name, jsonValue(aiTraceSafe(payload)))
	return e
}

// aiTrace 为事务外模型响应和工具开始事件进入独立租户事务。
func (s *Service) aiTrace(ctx context.Context, j *aiJobData, kind, name string, payload any) error {
	return platform.TenantTx(ctx, s.pool, j.Tenant, func(tx pgx.Tx) error { return aiTraceTx(ctx, tx, j, kind, name, payload) })
}

// aiTraceJobs 返回当前成员可见的任务列表和租户内逐步链路，不开放内部快照或成本价格。
func (s *Service) aiTraceJobs(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	page, e := s.recordPageOptions(r, a, 20)
	if e != nil {
		replyError(w, r, e)
		return
	}
	var result any
	e = platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := liveMember(ctx, tx, a); e != nil {
			return e
		}
		id := r.PathValue("id")
		if id != "" {
			if !uuidPattern.MatchString(id) {
				return platform.Fail(404, "not_found", "记录不存在或无权访问")
			}
			job, e := txJSON(ctx, tx, `SELECT to_jsonb(j)-'snapshot'-'cost_micro'||jsonb_build_object('question',coalesce(m.content,j.snapshot->'history'->0->>'content',''),'bot_name',coalesce(b.name,'')) FROM ai_jobs j LEFT JOIN messages m ON m.id=j.trigger_message_id LEFT JOIN ai_bots b ON b.id=j.bot_id WHERE j.id=$1 AND ($2 OR j.owner_membership_id=$3)`, id, isAdmin(a), a.MemberID)
			if e != nil {
				return e
			}
			rows, e := tx.Query(ctx, "SELECT to_jsonb(x) FROM ai_job_events x WHERE job_id=$1 ORDER BY id", id)
			if e != nil {
				return e
			}
			events, e := jsonRows(rows)
			if e != nil {
				return e
			}
			rows, e = tx.Query(ctx, `SELECT jsonb_build_object('id',id,'message_type',message_type,'content',content,'created_at',created_at) FROM messages WHERE ai_job_id=$1 AND withdrawn_at IS NULL ORDER BY event_seq`, id)
			if e != nil {
				return e
			}
			sent, e := jsonRows(rows)
			result = map[string]any{"job": job, "events": events, "sent_messages": sent}
			return e
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		status := r.URL.Query().Get("status")
		query, args := page.query(`SELECT to_jsonb(j)-'snapshot'-'cost_micro'||jsonb_build_object('question',coalesce(m.content,j.snapshot->'history'->0->>'content',''),'bot_name',coalesce(b.name,'')) FROM ai_jobs j LEFT JOIN messages m ON m.id=j.trigger_message_id LEFT JOIN ai_bots b ON b.id=j.bot_id WHERE ($1 OR j.owner_membership_id=$2) AND ($3='' OR j.status=$3) AND ($4='' OR strpos(lower(coalesce(m.content,j.snapshot->>'body','')||coalesce(b.name,'')),lower($4))>0)`, []any{isAdmin(a), a.MemberID, status, q}, "j")
		rows, e := tx.Query(ctx, query, args...)
		if e != nil {
			return e
		}
		items, e := jsonRows(rows)
		if e != nil {
			return e
		}
		result, e = s.recordPageResult(items, page)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, result)
}
