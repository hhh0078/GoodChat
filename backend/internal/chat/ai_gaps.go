// 文件作用说明：提供报告知识缺口的 Agent 工具及租户内相似问题去重，不把主动转人工当作缺失知识。
package chat

import (
	"context"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiGapInput 只允许模型说明缺口和引用已展示待办，原问题和来源由服务端读取。
type aiGapInput struct {
	Reason     string `json:"reason"`
	ExistingID string `json:"existing_id,omitempty"`
}

// aiGapNormalize 去除标点及空白，统一大小写以合并同一问题的格式差异。
func aiGapNormalize(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}

// aiGapSimilar 使用双字集合比较明显相似问题，语义改写由模型引用已授权目录补充。
func aiGapSimilar(a, b string) bool {
	a, b = aiGapNormalize(a), aiGapNormalize(b)
	if a == b {
		return a != ""
	}
	if len([]rune(a)) < 6 || len([]rune(b)) < 6 {
		return false
	}
	// 数字不同通常代表订单或版本不同，不按字符相似度误合并。
	digits := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsDigit(r) {
				return r
			}
			return -1
		}, s)
	}
	if digits(a) != digits(b) {
		return false
	}
	grams := func(s string) map[string]bool {
		r := []rune(s)
		out := map[string]bool{}
		for i := 1; i < len(r); i++ {
			out[string(r[i-1:i+1])] = true
		}
		return out
	}
	x, y := grams(a), grams(b)
	same := 0
	for k := range x {
		if y[k] {
			same++
		}
	}
	return float64(2*same)/float64(len(x)+len(y)) >= 0.8
}

// aiGapCatalog 只展示当前租户最近的未解决及已忽略题目供模型识别语义重复，不泄露访客身份。
func aiGapCatalog(ctx context.Context, tx pgx.Tx) ([]map[string]any, error) {
	rows, e := tx.Query(ctx, `SELECT jsonb_build_object('id',id,'question',left(question,160),'status',status) FROM ai_pending_items WHERE status<>'resolved' ORDER BY updated_at DESC LIMIT 100`)
	if e != nil {
		return nil, e
	}
	return jsonRows(rows)
}

// aiReportGap 按真实任务校验来源并在租户串行锁内合并记录，调试只返回预览而不污染待办。
func (s *Service) aiReportGap(ctx context.Context, j *aiJobData, in aiGapInput, readIDs []string) (any, error) {
	var result any
	e := platform.TenantTx(ctx, s.pool, j.Tenant, func(tx pgx.Tx) error {
		if j.Kind != "reply" {
			result = map[string]any{"recorded": false, "preview": true}
			return aiTraceTx(ctx, tx, j, "tool_result", "report_knowledge_gap", result)
		}
		if e := s.aiReplyValid(ctx, tx, j, false); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,47))", j.Tenant); e != nil {
			return e
		}
		var prior string
		e := tx.QueryRow(ctx, "SELECT item_id::text FROM ai_pending_occurrences WHERE job_id=$1", j.ID).Scan(&prior)
		if e == nil {
			result = map[string]any{"recorded": true, "id": prior, "duplicate": true}
			return aiTraceTx(ctx, tx, j, "tool_result", "report_knowledge_gap", result)
		}
		if e != pgx.ErrNoRows {
			return e
		}
		var question string
		if e = tx.QueryRow(ctx, "SELECT left(content,2000) FROM messages WHERE id=$1 AND sender_kind='visitor' AND withdrawn_at IS NULL", j.Trigger).Scan(&question); e != nil {
			return e
		}
		normalized := aiGapNormalize(question)
		if normalized == "" {
			result = map[string]any{"recorded": false, "reason": "empty_question"}
			return aiTraceTx(ctx, tx, j, "tool_result", "report_knowledge_gap", result)
		}
		rows, e := tx.Query(ctx, "SELECT id::text,question FROM ai_pending_items WHERE status<>'resolved' ORDER BY updated_at DESC")
		if e != nil {
			return e
		}
		id := ""
		for rows.Next() {
			var candidate, text string
			if e = rows.Scan(&candidate, &text); e != nil {
				rows.Close()
				return e
			}
			if id == "" && (aiGapSimilar(question, text) || candidate == in.ExistingID && inStrings(candidate, j.GapIDs...)) {
				id = candidate
			}
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return e
		}
		duplicate := id != ""
		if !duplicate {
			id = newID()
			_, e = tx.Exec(ctx, `INSERT INTO ai_pending_items(id,tenant_id,question,normalized_question,reason,source_job_id,last_job_id,bot_ids,read_ids) VALUES($1,$2,$3,$4,$5,$6,$6,ARRAY[$7::uuid],$8::uuid[])`, id, j.Tenant, question, normalized, in.Reason, j.ID, j.BotID, readIDs)
		} else {
			_, e = tx.Exec(ctx, `UPDATE ai_pending_items SET occurrences=occurrences+1,last_job_id=$2,bot_ids=ARRAY(SELECT DISTINCT unnest(bot_ids||ARRAY[$3::uuid])),read_ids=ARRAY(SELECT DISTINCT unnest(read_ids||$4::uuid[])),updated_at=now() WHERE id=$1`, id, j.ID, j.BotID, readIDs)
		}
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO ai_pending_occurrences(tenant_id,job_id,item_id) VALUES($1,$2,$3)", j.Tenant, j.ID, id); e != nil {
			return e
		}
		result = map[string]any{"recorded": true, "id": id, "duplicate": duplicate}
		return aiTraceTx(ctx, tx, j, "tool_result", "report_knowledge_gap", result)
	})
	return result, e
}
