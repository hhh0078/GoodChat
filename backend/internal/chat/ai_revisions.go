// 文件作用说明：从已完成描述任务读取知识发布历史，并按原子正文描述快照恢复指定版本。
package chat

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiKnowledgeVersions 只返回已成功发布的正文和描述，不暴露模型价格或密钥。
func (s *Service) aiKnowledgeVersions(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var items []map[string]any
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		var exists bool
		if e := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM ai_knowledge_bases WHERE id=$1 AND NOT archived)", r.PathValue("id")).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return pgx.ErrNoRows
		}
		rows, e := tx.Query(r.Context(), `SELECT jsonb_build_object('version',(snapshot->>'knowledge_version')::bigint,'body',snapshot->>'body','description',result->>'description','created_at',completed_at) FROM ai_jobs WHERE knowledge_id=$1 AND kind IN('summary','restore') AND status='succeeded' ORDER BY created_at DESC LIMIT 100`, r.PathValue("id"))
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

// aiKnowledgeRestore 使用已发布快照恢复而不重新猜测描述，操作保留零模型调用的回滚审计。
func (s *Service) aiKnowledgeRestore(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	id := r.PathValue("id")
	var in struct {
		Version  int64 `json:"version"`
		Expected int64 `json:"expected_version"`
	}
	if e := platform.Decode(w, r, &in); e != nil {
		replyError(w, r, e)
		return
	}
	if in.Version < 1 || in.Expected < 1 {
		returnError(w, r, "ai_version_conflict", "内容已更新，请刷新后重试")
		return
	}
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		var owner string
		var version int64
		if e := tx.QueryRow(ctx, "SELECT owner_membership_id::text,version FROM ai_knowledge_bases WHERE id=$1 AND NOT archived FOR UPDATE", id).Scan(&owner, &version); e != nil {
			return e
		}
		if owner != a.MemberID {
			return platform.Fail(403, "ai_owner_required", "只能修改或删除自己的内容")
		}
		if version != in.Expected {
			return platform.Fail(409, "ai_version_conflict", "内容已更新，请刷新后重试")
		}
		var body, description string
		if e := tx.QueryRow(ctx, `SELECT snapshot->>'body',result->>'description' FROM ai_jobs WHERE knowledge_id=$1 AND kind IN('summary','restore') AND status='succeeded' AND (snapshot->>'knowledge_version')::bigint=$2 ORDER BY created_at DESC LIMIT 1`, id, in.Version).Scan(&body, &description); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "UPDATE ai_knowledge_bases SET draft_body=$2,published_body=$2,description=$3,version=version+1,published_version=version+1,status='ready',error_code='',updated_at=now() WHERE id=$1", id, body, description); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO ai_jobs(tenant_id,kind,knowledge_id,owner_membership_id,status,snapshot,result,completed_at) VALUES($1,'restore',$2,$3,'succeeded',$4,$5,now())`, a.TenantID, id, a.MemberID, jsonValue(aiSnapshot{Body: body, KnowledgeVersion: version + 1}), jsonValue(aiPlan{Description: description}))
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"restored": true})
}
