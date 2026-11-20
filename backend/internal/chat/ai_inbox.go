// 文件作用说明：为管理员提供知识补充待办、忽略删除及受权限保护的知识追加发布流程。
package chat

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiInbox 按租户管理知识缺口；删除保留去重标记，补充不改变既有知识所有权。
func (s *Service) aiInbox(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	page, e := s.recordPageOptions(r, a, 20)
	if e != nil {
		replyError(w, r, e)
		return
	}
	var in struct {
		Action      string `json:"action"`
		FolderID    string `json:"folder_id"`
		KnowledgeID string `json:"knowledge_id"`
		Title       string `json:"title"`
		Answer      string `json:"answer"`
		Version     int64  `json:"version"`
	}
	if r.Method == "POST" {
		if e = platform.Decode(w, r, &in); e != nil {
			replyError(w, r, e)
			return
		}
	}
	var result any
	e = platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := liveMember(ctx, tx, a); e != nil {
			return e
		}
		if r.Method == "GET" {
			status := r.URL.Query().Get("status")
			if status == "" {
				status = "pending"
			}
			q := strings.TrimSpace(r.URL.Query().Get("q"))
			query, args := page.query(`SELECT to_jsonb(p)||jsonb_build_object('conversation_id',j.conversation_id,'knowledge_title',coalesce(k.title,''),'knowledge_status',coalesce(k.status,''),'knowledge_error',coalesce(k.error_code,''),'read_knowledge',coalesce((SELECT jsonb_agg(jsonb_build_object('id',x.id,'title',x.title)) FROM ai_knowledge_bases x WHERE x.id=ANY(p.read_ids)),'[]'::jsonb)) FROM ai_pending_items p JOIN ai_jobs j ON j.id=p.last_job_id LEFT JOIN ai_knowledge_bases k ON k.id=p.knowledge_id WHERE p.status<>'deleted' AND ($1='all' OR p.status=$1 OR $1='pending' AND p.status='supplementing') AND ($2='' OR strpos(lower(p.question||p.reason),lower($2))>0)`, []any{status, q}, "p")
			rows, e := tx.Query(ctx, query, args...)
			if e != nil {
				return e
			}
			items, e := jsonRows(rows)
			if e != nil {
				return e
			}
			out, e := s.recordPageResult(items, page)
			if e != nil {
				return e
			}
			out["summary"], e = txJSON(ctx, tx, `SELECT jsonb_build_object('pending',count(*) FILTER(WHERE status IN('pending','supplementing')),'resolved',count(*) FILTER(WHERE status='resolved'),'ignored',count(*) FILTER(WHERE status='ignored')) FROM ai_pending_items`)
			result = out
			return e
		}
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,47))", a.TenantID); e != nil {
			return e
		}
		id := r.PathValue("id")
		if !uuidPattern.MatchString(id) {
			return platform.Fail(404, "not_found", "记录不存在或无权访问")
		}
		var question, status string
		if e := tx.QueryRow(ctx, "SELECT question,status FROM ai_pending_items WHERE id=$1 AND status<>'deleted' FOR UPDATE", id).Scan(&question, &status); e != nil {
			return e
		}
		if inStrings(in.Action, "ignore", "delete", "reopen") {
			if status == "supplementing" {
				return platform.Fail(409, "ai_gap_publishing", "知识正在发布，请等待结果后处理")
			}
			next := map[string]string{"ignore": "ignored", "delete": "deleted", "reopen": "pending"}[in.Action]
			_, e := tx.Exec(ctx, "UPDATE ai_pending_items SET status=$2,handled_by=$3,updated_at=now() WHERE id=$1", id, next, a.MemberID)
			result = map[string]any{"id": id}
			return e
		}
		if in.Action == "retry" {
			if status != "pending" {
				return platform.Fail(409, "ai_gap_already_handled", "该待办已处理或正在发布，请刷新")
			}
			var knowledge string
			if e := tx.QueryRow(ctx, `SELECT k.id::text FROM ai_pending_items p JOIN ai_knowledge_bases k ON k.id=p.knowledge_id WHERE p.id=$1 AND k.owner_membership_id=$2 AND k.enabled AND NOT k.archived AND k.status='failed' FOR UPDATE OF k`, id, a.MemberID).Scan(&knowledge); e != nil {
				return e
			}
			if e := s.enqueueAISummary(ctx, tx, a, knowledge); e != nil {
				return e
			}
			_, e := tx.Exec(ctx, `UPDATE ai_pending_items SET status='supplementing',knowledge_version=(SELECT version FROM ai_knowledge_bases WHERE id=$2),handled_by=$3,updated_at=now() WHERE id=$1`, id, knowledge, a.MemberID)
			result = map[string]any{"id": id, "knowledge_id": knowledge}
			return e
		}
		if in.Action != "supplement" {
			return platform.Fail(400, "ai_gap_action_invalid", "请选择有效的待办操作")
		}
		if status == "resolved" || status == "supplementing" {
			return platform.Fail(409, "ai_gap_already_handled", "该待办已处理或正在发布，请刷新")
		}
		answer := strings.TrimSpace(in.Answer)
		title := strings.TrimSpace(in.Title)
		if answer == "" || len([]byte(answer)) > 12000 || len([]rune(title)) > 100 {
			return platform.Fail(400, "ai_gap_answer_required", "请填写不超过12000字节的补充知识")
		}
		block := "\n\n问题：" + question + "\n答复：" + answer
		if len([]byte(block)) > 16000 {
			return platform.Fail(400, "ai_knowledge_limit", "知识库正文不能为空且不能超过16000字节")
		}
		knowledge := in.KnowledgeID
		if knowledge == "" {
			if title == "" {
				title = string([]rune(question)[:min(80, len([]rune(question)))])
			}
			if _, e := tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", a.TenantID); e != nil {
				return e
			}
			var count int
			if e := tx.QueryRow(ctx, "SELECT count(*) FROM ai_knowledge_bases").Scan(&count); e != nil {
				return e
			}
			if count >= 1000 {
				return platform.Fail(400, "ai_resource_limit", "资源数量已达到上限")
			}
			if e := aiValidateFolder(ctx, tx, a.TenantID, in.FolderID); e != nil {
				return e
			}
			knowledge = newID()
			if _, e := tx.Exec(ctx, `INSERT INTO ai_knowledge_bases(id,tenant_id,owner_membership_id,title,draft_body,folder_id) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid)`, knowledge, a.TenantID, a.MemberID, title, strings.TrimSpace(block), in.FolderID); e != nil {
				return e
			}
		} else {
			if !uuidPattern.MatchString(knowledge) {
				return platform.Fail(404, "not_found", "记录不存在或无权访问")
			}
			var body string
			var version int64
			if e := tx.QueryRow(ctx, "SELECT draft_body,version FROM ai_knowledge_bases WHERE id=$1 AND owner_membership_id=$2 AND NOT archived AND enabled FOR UPDATE", knowledge, a.MemberID).Scan(&body, &version); e != nil {
				return e
			}
			if version != in.Version {
				return platform.Fail(409, "ai_version_conflict", "内容已更新，请刷新后重试")
			}
			if len([]byte(body+block)) > 16000 {
				return platform.Fail(400, "ai_knowledge_limit", "知识库正文不能为空且不能超过16000字节")
			}
			if _, e := tx.Exec(ctx, "UPDATE ai_knowledge_bases SET draft_body=$2,version=version+1,status='pending',error_code='',updated_at=now() WHERE id=$1", knowledge, body+block); e != nil {
				return e
			}
		}
		// 绑定上限与机器人编辑保持一致，失败时整笔补充回滚，不留下不会生效的知识。
		var full bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_bots WHERE id IN(SELECT unnest(bot_ids) FROM ai_pending_items WHERE id=$1) AND NOT $2::uuid=ANY(knowledge_ids) AND cardinality(knowledge_ids)>=30)", id, knowledge).Scan(&full); e != nil {
			return e
		}
		if full {
			return platform.Fail(400, "ai_gap_bot_limit", "对应机器人知识数量已满，请先调整授权知识")
		}
		if e := s.enqueueAISummary(ctx, tx, a, knowledge); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `UPDATE ai_pending_items SET status='supplementing',knowledge_id=$2,knowledge_version=(SELECT version FROM ai_knowledge_bases WHERE id=$2),handled_by=$3,updated_at=now() WHERE id=$1`, id, knowledge, a.MemberID)
		result = map[string]any{"id": id, "knowledge_id": knowledge}
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, result)
}

// aiResolveGaps 在知识成功原子发布后绑定受影响机器人并完成待办；较新草稿不会被旧任务误完成。
func aiResolveGaps(ctx context.Context, tx pgx.Tx, j *aiJobData) error {
	rows, e := tx.Query(ctx, `SELECT id::text,bot_ids FROM ai_pending_items WHERE knowledge_id=$1 AND knowledge_version<=$2 AND status='supplementing' ORDER BY id FOR UPDATE`, j.KnowledgeID, j.Snapshot.KnowledgeVersion)
	if e != nil {
		return e
	}
	type target struct {
		id   string
		bots []string
	}
	targets := []target{}
	for rows.Next() {
		var t target
		if e = rows.Scan(&t.id, &t.bots); e != nil {
			rows.Close()
			return e
		}
		targets = append(targets, t)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, t := range targets {
		// 机器人可能在描述生成期间被另一个管理员编辑，发布时再次检查容量和知识启用状态。
		var available bool
		if e = tx.QueryRow(ctx, "SELECT enabled AND NOT archived FROM ai_knowledge_bases WHERE id=$1", j.KnowledgeID).Scan(&available); e != nil {
			return e
		}
		if !available {
			return platform.Fail(409, "ai_stale", "内容已更新，请刷新后重试")
		}
		rows, e := tx.Query(ctx, "SELECT id FROM ai_bots WHERE id=ANY($1::uuid[]) ORDER BY id FOR UPDATE", t.bots)
		if e != nil {
			return e
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return e
		}
		var full bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_bots WHERE id=ANY($1::uuid[]) AND NOT $2::uuid=ANY(knowledge_ids) AND cardinality(knowledge_ids)>=30)", t.bots, j.KnowledgeID).Scan(&full); e != nil {
			return e
		}
		if full {
			return platform.Fail(409, "ai_gap_bot_limit", "对应机器人知识数量已满，请先调整授权知识")
		}

		if _, e = tx.Exec(ctx, `UPDATE ai_bots SET knowledge_ids=array_append(knowledge_ids,$1::uuid),version=version+1,updated_at=now() WHERE id=ANY($2::uuid[]) AND NOT $1::uuid=ANY(knowledge_ids)`, j.KnowledgeID, t.bots); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE ai_pending_items SET status='resolved',updated_at=now() WHERE id=$1", t.id); e != nil {
			return e
		}
	}
	return nil
}
