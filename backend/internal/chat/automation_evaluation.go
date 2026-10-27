// 文件作用说明：按真实结束时间限制访客评价与改评，并不可覆盖地记录每个评分版本。
package chat

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// evaluationInput 仅接受访客主动填写的评价，改评必须携带读取到的当前版本。
type evaluationInput struct {
	Score   int      `json:"score"`
	Comment string   `json:"comment"`
	Tags    []string `json:"tags"`
	Version int      `json:"version"`
}

// sameEvaluation 只把相同评分、原文和标签当作提交重试，不静默覆盖其他评价。
func sameEvaluation(item map[string]any, input evaluationInput) bool {
	return item["score"] == float64(input.Score) && item["comment"] == input.Comment && string(jsonValue(item["tags"])) == string(jsonValue(input.Tags))
}

// evaluationDeadline 计算改评允许的最后时刻，不能越过首次提交窗口。
func evaluationDeadline(config automationConfig, closed, created time.Time) time.Time {
	deadline := created.Add(time.Duration(config.EvaluationEditWindowHours) * time.Hour)
	submission := closed.Add(time.Duration(config.EvaluationWindowHours) * time.Hour)
	if submission.Before(deadline) {
		return submission
	}
	return deadline
}

// handleEvaluation 在咨询锁内保存初评或带版本的改评，管理员不能代客户评分。
func (s *Service) handleEvaluation(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	if a.ContactID == "" {
		replyError(w, r, platform.Fail(403, "visitor_required", "只有咨询访客可以评价"))
		return
	}
	var input evaluationInput
	if err := platform.Decode(w, r, &input); err != nil {
		replyError(w, r, err)
		return
	}
	if input.Tags == nil {
		input.Tags = []string{}
	}
	if input.Score < 1 || input.Score > 5 || len([]rune(input.Comment)) > 2000 || len(input.Tags) > 12 {
		returnError(w, r, "invalid_evaluation", "请选择一至五星并填写不超过2000字的评价")
		return
	}
	var result map[string]any
	id := r.PathValue("id")
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tenantActive(ctx, tx, a.TenantID); err != nil {
			return err
		}
		if err := canReadConversation(ctx, tx, a, id); err != nil {
			return err
		}
		var status string
		var closed *time.Time
		var finalAgent *string
		if err := tx.QueryRow(ctx, "SELECT status,closed_at,current_agent_id::text FROM conversations WHERE id=$1 FOR UPDATE", id).Scan(&status, &closed, &finalAgent); err != nil {
			return err
		}
		if status != "closed" || closed == nil {
			return platform.Fail(409, "conversation_not_closed", "咨询结束后才能评价")
		}
		stored, err := txJSON(ctx, tx, "SELECT to_jsonb(e) FROM conversation_evaluations e WHERE conversation_id=$1", id)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if err == nil && sameEvaluation(stored, input) {
			result = stored
			return nil
		}
		config, err := loadAutomationConfig(ctx, tx, a.TenantID)
		if err != nil {
			return err
		}
		if !config.EvaluationEnabled {
			return platform.Fail(403, "evaluation_disabled", "当前租户暂未开放评价")
		}
		if time.Now().After(closed.Add(time.Duration(config.EvaluationWindowHours) * time.Hour)) {
			return platform.Fail(410, "evaluation_window_expired", "本次咨询的评价期限已结束")
		}
		seen := map[string]bool{}
		for _, tag := range input.Tags {
			if strings.TrimSpace(tag) != tag || seen[tag] || !inStrings(tag, config.EvaluationTags...) {
				return platform.Fail(400, "invalid_evaluation_tags", "请选择当前允许的评价标签且不可重复")
			}
			seen[tag] = true
		}
		if stored == nil {
			if input.Version != 0 {
				return platform.Fail(409, "evaluation_version_conflict", "本次咨询尚无评价，请重新读取")
			}
			result, err = txJSON(ctx, tx, "INSERT INTO conversation_evaluations(tenant_id,conversation_id,contact_id,score,comment,tags,final_agent_id) VALUES($1,$2,$3,$4,$5,$6,$7::uuid) RETURNING to_jsonb(conversation_evaluations)", a.TenantID, id, a.ContactID, input.Score, input.Comment, jsonValue(input.Tags), finalAgent)
		} else {
			if !config.EvaluationAllowEdit {
				return platform.Fail(409, "evaluation_exists", "本次咨询已经评价，当前不允许修改")
			}
			created, err := time.Parse(time.RFC3339Nano, stored["created_at"].(string))
			if err != nil {
				return err
			}
			if time.Now().After(evaluationDeadline(config, *closed, created)) {
				return platform.Fail(410, "evaluation_edit_window_expired", "本次评价的修改期限已结束")
			}
			if input.Version < 1 || float64(input.Version) != stored["version"] {
				return platform.Fail(409, "evaluation_version_conflict", "评价已更新，请重新读取当前版本")
			}
			result, err = txJSON(ctx, tx, "UPDATE conversation_evaluations SET score=$2,comment=$3,tags=$4,version=version+1,updated_at=now() WHERE id=$1 AND version=$5 RETURNING to_jsonb(conversation_evaluations)", stored["id"], input.Score, input.Comment, jsonValue(input.Tags), input.Version)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO evaluation_versions(tenant_id,evaluation_id,version,score,comment,tags) VALUES($1,$2,$3,$4,$5,$6)", a.TenantID, result["id"], int(result["version"].(float64)), input.Score, input.Comment, jsonValue(input.Tags))
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// evaluationState 仅为当前授权会话返回真实评价和截止时间，读操作不预填满意分。
func (s *Service) evaluationState(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tenantActive(ctx, tx, a.TenantID); err != nil {
			return err
		}
		if err := canReadConversation(ctx, tx, a, r.PathValue("id")); err != nil {
			return err
		}
		config, err := loadAutomationConfig(ctx, tx, a.TenantID)
		if err != nil {
			return err
		}
		var status string
		var closed *time.Time
		if err := tx.QueryRow(ctx, "SELECT status,closed_at FROM conversations WHERE id=$1", r.PathValue("id")).Scan(&status, &closed); err != nil {
			return err
		}
		item, err := txJSON(ctx, tx, "SELECT to_jsonb(e) FROM conversation_evaluations e WHERE conversation_id=$1", r.PathValue("id"))
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		result = map[string]any{"enabled": config.EvaluationEnabled, "can_submit": false, "can_edit": false, "evaluation": item, "allowed_tags": config.EvaluationTags, "timezone": config.Timezone}
		if status != "closed" || closed == nil {
			return nil
		}
		deadline := closed.Add(time.Duration(config.EvaluationWindowHours) * time.Hour)
		result["submission_deadline"] = deadline
		valid := config.EvaluationEnabled && a.ContactID != "" && !time.Now().After(deadline)
		result["can_submit"] = valid && item == nil
		if item != nil {
			created, err := time.Parse(time.RFC3339Nano, item["created_at"].(string))
			if err != nil {
				return err
			}
			edit := evaluationDeadline(config, *closed, created)
			result["edit_deadline"] = edit
			result["can_edit"] = valid && config.EvaluationAllowEdit && !time.Now().After(edit)
		}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// evaluationVersions 只向租户管理员展示访客提交的原始版本，不提供修改或删除历史接口。
func (s *Service) evaluationVersions(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var items []map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM conversation_evaluations WHERE id=$1)", r.PathValue("id")).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(v) FROM evaluation_versions v WHERE evaluation_id=$1 ORDER BY version DESC", r.PathValue("id"))
		if err != nil {
			return err
		}
		items, err = jsonRows(rows)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items})
}
