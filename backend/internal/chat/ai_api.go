// 文件作用说明：提供机器人调试、任务查询、人工接管和已计费辅助草稿的确认发送接口。
package chat

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
)

// aiTest 创建明确计费的真实模型调试任务，使用当前成员机器人及授权资源。
func (s *Service) aiTest(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var in struct {
		Text string `json:"text"`
	}
	if e := platform.Decode(w, r, &in); e != nil {
		replyError(w, r, e)
		return
	}
	if strings.TrimSpace(in.Text) == "" || len([]byte(in.Text)) > 4000 {
		returnError(w, r, "ai_test_invalid", "请输入不超过4000字节的调试问题")
		return
	}
	id := newID()
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		var botID string
		if e := tx.QueryRow(r.Context(), "SELECT id::text FROM ai_bots WHERE owner_membership_id=$1", a.MemberID).Scan(&botID); e != nil {
			return e
		}
		b, e := loadAIBot(r.Context(), tx, botID)
		if e != nil {
			return e
		}
		var count int
		if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM ai_jobs WHERE owner_membership_id=$1 AND kind='test' AND created_at>now()-interval '1 minute'", a.MemberID).Scan(&count); e != nil {
			return e
		}
		if count >= 5 {
			return platform.Fail(429, "ai_test_rate_limit", "请稍后再试")
		}
		snapshot := aiSnapshot{Bot: b, Locale: localization.RequestLocale(r), History: []*schema.Message{schema.UserMessage(in.Text)}}
		_, e = tx.Exec(r.Context(), `INSERT INTO ai_jobs(id,tenant_id,kind,bot_id,owner_membership_id,snapshot) VALUES($1,$2,'test',$3,$4,$5)`, id, a.TenantID, botID, a.MemberID, jsonValue(snapshot))
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 202, map[string]any{"id": id, "status": "queued"})
}

// aiJob 返回本成员任务或管理员可审计结果，隐藏包含成本价格的内部快照。
func (s *Service) aiJob(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var item any
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		var e error
		item, e = txJSON(r.Context(), tx, `SELECT to_jsonb(j)-'snapshot'-'cost_micro' FROM ai_jobs j WHERE id=$1 AND ($2 OR owner_membership_id=$3)`, r.PathValue("id"), isAdmin(a), a.MemberID)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, item)
}

// aiConversation 让有接待权限的人工暂停或恢复AI，状态变化增加控制版本。
func (s *Service) aiConversation(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	if a.ContactID != "" {
		replyError(w, r, platform.Fail(403, "agent_required", "该动作需要客服身份"))
		return
	}
	id := r.PathValue("id")
	var in struct {
		Action string `json:"action"`
	}
	if r.Method == "POST" {
		if e := platform.Decode(w, r, &in); e != nil {
			replyError(w, r, e)
			return
		}
		if !inStrings(in.Action, "pause", "resume") {
			returnError(w, r, "ai_control_invalid", "请检查机器人设置")
			return
		}
	}
	var result any
	drafts := []map[string]any{}
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := canReadConversation(ctx, tx, a, id); e != nil {
			return e
		}
		if r.Method == "POST" {
			var agent, status string
			if e := tx.QueryRow(ctx, "SELECT coalesce(current_agent_id::text,''),status FROM conversations WHERE id=$1 FOR UPDATE", id).Scan(&agent, &status); e != nil {
				return e
			}
			if status == "closed" || !isAdmin(a) && (agent != a.MemberID || !a.CanServe) {
				return platform.Fail(403, "not_current_agent", "只有正式接待客服可以发送消息")
			}
			if _, e := tx.Exec(ctx, "UPDATE conversations SET ai_paused=$2,ai_epoch=ai_epoch+1,context=context-'ai_receiving' WHERE id=$1", id, in.Action == "pause"); e != nil {
				return e
			}
			if _, e := s.emit(ctx, tx, a.TenantID, id, "conversation.updated", "public", map[string]any{"id": id, "ai_paused": in.Action == "pause"}); e != nil {
				return e
			}
		}
		var e error
		result, e = txJSON(ctx, tx, "SELECT jsonb_build_object('ai_paused',ai_paused,'ai_epoch',ai_epoch) FROM conversations WHERE id=$1", id)
		if e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT to_jsonb(j)-'snapshot'-'cost_micro' FROM ai_jobs j WHERE conversation_id=$1 AND status='draft' AND owner_membership_id=$2 AND (snapshot->>'epoch')::bigint=(SELECT ai_epoch FROM conversations WHERE id=$1 AND NOT ai_paused) AND trigger_message_id=(SELECT id FROM messages WHERE conversation_id=$1 AND sender_kind='visitor' AND withdrawn_at IS NULL ORDER BY event_seq DESC LIMIT 1) ORDER BY created_at DESC LIMIT 5`, id, a.MemberID)
		if e != nil {
			return e
		}
		drafts, e = jsonRows(rows)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"control": result, "drafts": drafts})
}

// aiDraftSend 对辅助草稿再次校验会话、话术及知识版本，只首次发送且不再次扣费。
func (s *Service) aiDraftSend(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	if a.ContactID != "" || !a.CanServe {
		replyError(w, r, platform.Fail(403, "agent_required", "该动作需要客服身份"))
		return
	}
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		id := r.PathValue("id")
		if e := canReadConversation(ctx, tx, a, id); e != nil {
			return e
		}
		// 与自动提交相同顺序先锁任务再锁会话，发送成功的重试直接返回。
		var status string
		var raw []byte
		if e := tx.QueryRow(ctx, "SELECT status,result FROM ai_jobs WHERE id=$1 AND conversation_id=$2 AND owner_membership_id=$3 FOR UPDATE", r.PathValue("job"), id, a.MemberID).Scan(&status, &raw); e != nil {
			return e
		}
		if status == "succeeded" {
			return nil
		}
		if status != "draft" {
			return platform.Fail(409, "ai_draft_unavailable", "AI草稿已失效")
		}
		j, e := aiJobFromRow(ctx, tx, r.PathValue("job"))
		if e != nil {
			return e
		}
		if j.Snapshot.Agent != a.MemberID {
			return platform.Fail(403, "not_current_agent", "只有正式接待客服可以发送消息")
		}
		if e = s.aiReplyValid(ctx, tx, &j, true); e != nil {
			return platform.Fail(409, "ai_draft_unavailable", "AI草稿已失效")
		}
		var plan aiPlan
		if e = json.Unmarshal(raw, &plan); e != nil {
			return e
		}
		if e = aiCheckReadVersions(ctx, tx, &j, plan); e != nil {
			return platform.Fail(409, "ai_draft_unavailable", "AI草稿已失效")
		}
		if e = s.sendAIPlan(ctx, tx, &j, plan); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE ai_jobs SET status='succeeded',completed_at=now() WHERE id=$1", j.ID)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"sent": true})
}
