// 文件作用说明：以固定锁顺序和数据库容量预留实现分配、接单、转接与关闭。
package chat

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// routeState 保存已锁定会话与分配池的决策字段。
type routeState struct {
	ID, Pool, Group, Status, Agent, Desired, Mode, Algorithm, AssignmentMode, ReceptionMode string
	Cursor, Version                                                                         int64
	History                                                                                 bool
	Required, Optional                                                                      []string
	Decision                                                                                map[string]any
	ReservedFor                                                                             string
}

// lockedRoute 按队列分片在前、会话在后的顺序取得事务锁。
func lockedRoute(ctx context.Context, tx pgx.Tx, id string) (routeState, error) {
	var p, link string
	var v routeState
	if err := tx.QueryRow(ctx, "SELECT routing_pool_id::text,created_entry_id::text FROM conversations WHERE id=$1", id).Scan(&p, &link); err != nil {
		return v, err
	}
	if _, err := tx.Exec(ctx, "SELECT id FROM routing_pools WHERE id=$1 FOR UPDATE", p); err != nil {
		return v, err
	}
	if _, err := tx.Exec(ctx, "SELECT id FROM access_links WHERE id=$1 FOR SHARE", link); err != nil {
		return v, err
	}
	err := tx.QueryRow(ctx, "SELECT c.id::text,c.routing_pool_id::text,c.service_group_id::text,c.status,COALESCE(c.current_agent_id::text,''),COALESCE(c.desired_agent_id::text,''),c.agent_mode,p.algorithm,p.assignment_mode,p.rr_cursor,c.version,p.history_priority,COALESCE(c.source_snapshot->'entry'->>'reception_mode',l.reception_mode) FROM conversations c JOIN routing_pools p ON p.id=c.routing_pool_id AND p.tenant_id=c.tenant_id JOIN access_links l ON l.id=c.created_entry_id AND l.tenant_id=c.tenant_id WHERE c.id=$1 FOR UPDATE OF c", id).Scan(&v.ID, &v.Pool, &v.Group, &v.Status, &v.Agent, &v.Desired, &v.Mode, &v.Algorithm, &v.AssignmentMode, &v.Cursor, &v.Version, &v.History, &v.ReceptionMode)
	if err != nil {
		return v, err
	}
	v.Algorithm = linkAlgorithm(v.Algorithm, v.ReceptionMode)
	v.Required, v.Optional, err = loadRouteSkills(ctx, tx, p, link)
	return v, err
}

// candidateAgents 选择当前可接待成员，所有算法共享相同资格与容量过滤。
func candidateAgents(ctx context.Context, tx pgx.Tx, v routeState) ([]string, error) {
	items, err := routingCandidates(ctx, tx, v)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids, nil
}

// lockCapacity 在运行行和成员行均锁定后再次检查当前资格与名额。
func lockCapacity(ctx context.Context, tx pgx.Tx, tenant, member string) error {
	var valid bool
	err := tx.QueryRow(ctx, "SELECT m.can_serve AND NOT m.disabled AND m.removed_at IS NULL AND r.available AND NOT r.busy AND r.online_until>statement_timestamp() AND r.current_load<m.capacity FROM agent_runtime r JOIN tenant_memberships m ON m.id=r.membership_id AND m.tenant_id=r.tenant_id WHERE r.membership_id=$1 AND r.tenant_id=$2 FOR UPDATE OF r,m", member, tenant).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return platform.Fail(409, "capacity_unavailable", "目标客服当前离线、示忙或已满载")
	}
	return nil
}

// assignLocked 在已锁队列会话内选择候选并原子创建唯一接待和容量占用。
func (s *Service) assignLocked(ctx context.Context, tx pgx.Tx, tenant, id, requested string, manual bool) error {
	v, err := lockedRoute(ctx, tx, id)
	if err != nil {
		return err
	}
	if v.Status != "queued" {
		if requested != "" && v.Agent == requested {
			return nil
		}
		if manual {
			return platform.Fail(409, "already_assigned", "该咨询已经被其他客服接待")
		}
		return nil
	}
	if v.Algorithm == "manual" && !manual {
		return nil
	}
	items, err := routingCandidates(ctx, tx, v)
	if err != nil {
		return err
	}
	reason := "algorithm"
	if requested != "" {
		if v.Desired != "" && v.Mode == "strict" && requested != v.Desired {
			return platform.Fail(409, "strict_target_required", "此咨询严格等待指定客服，不能由其他客服领取")
		}
		found := []routeCandidate{}
		for _, item := range items {
			if item.ID == requested {
				found = append(found, item)
			}
		}
		if len(found) == 0 {
			return candidateFailure(ctx, tx, v, requested)
		}
		items = found
		reason = "requested"
	} else {
		desired := []routeCandidate{}
		for _, item := range items {
			if item.ID == v.Desired {
				desired = append(desired, item)
			}
		}
		if len(desired) > 0 {
			items = desired
			reason = "specified"
		} else if v.Desired != "" && v.Mode == "strict" {
			return nil
		} else {
			items, reason, err = preferredCandidates(ctx, tx, v, items)
			if err != nil {
				return err
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	selected := items[0]
	if err := lockCapacity(ctx, tx, tenant, selected.ID); err != nil {
		if manual {
			return err
		}
		return nil
	}
	// 成员技能配置同样锁成员行；获得容量锁后重新读取，防止等待期间技能或组资格已变化。
	current, err := routingCandidates(ctx, tx, v)
	if err != nil {
		return err
	}
	valid := false
	for _, item := range current {
		if item.ID == selected.ID {
			valid = true
		}
	}
	if !valid {
		if manual {
			return platform.Fail(409, "candidate_unavailable", "客服资格已变化，请刷新后重试")
		}
		return nil
	}
	v.Decision = map[string]any{"algorithm": v.Algorithm, "reason": reason, "required_skill_ids": v.Required, "optional_skill_ids": v.Optional, "optional_matched": selected.Optional, "weight": selected.Weight}
	if err := s.createAssignment(ctx, tx, tenant, v, selected.ID, v.ReceptionMode != "manual" && v.AssignmentMode == "confirm"); err != nil {
		return err
	}
	if v.Algorithm == "weighted_round_robin" && reason == "algorithm" {
		return advanceWeights(ctx, tx, tenant, v.Pool, selected.ID, items)
	}
	return nil
}

// removeString 去掉已被优先排列的重复候选。
func removeString(ids []string, target string) []string {
	out := []string{}
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return out
}

// createAssignment 同事务增加名额、接待记录与分配事件。
func (s *Service) createAssignment(ctx context.Context, tx pgx.Tx, tenant string, v routeState, member string, confirm bool) error {
	id := newID()
	decision := v.Decision
	if decision == nil {
		decision = map[string]any{"algorithm": v.Algorithm, "reason": "transfer", "required_skill_ids": v.Required, "optional_skill_ids": v.Optional}
	}
	status, hold := "active", "active"
	if confirm {
		status, hold = "assigned", "held"
	}
	_, err := tx.Exec(ctx, "INSERT INTO conversation_assignments(id,tenant_id,conversation_id,membership_id,accepted_at,expires_at,decision) VALUES($1,$2,$3,$4,CASE WHEN $5 THEN NULL ELSE now() END,CASE WHEN $5 THEN now()+interval '30 seconds' ELSE NULL END,$6)", id, tenant, v.ID, member, confirm, jsonValue(decision))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO agent_capacity_holds(tenant_id,conversation_id,membership_id,assignment_id,status,expires_at) VALUES($1,$2,$3,$4,$5,CASE WHEN $6 THEN now()+interval '30 seconds' ELSE NULL END)", tenant, v.ID, member, id, hold, confirm); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE agent_runtime SET current_load=current_load+1,updated_at=now() WHERE membership_id=$1", member); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE conversations SET status=$2,current_assignment_id=$3,current_agent_id=$4,version=version+1,routing_attempts=routing_attempts+1,updated_at=now() WHERE id=$1", v.ID, status, id, member); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE routing_pools SET rr_cursor=rr_cursor+1 WHERE id=$1", v.Pool); err != nil {
		return err
	}
	_, err = s.emit(ctx, tx, tenant, v.ID, "assignment.changed", "public", map[string]any{"status": status, "agent_id": member, "assignment_id": id})
	return err
}

// releaseHolds 只释放实际仍占用的记录，并按稳定顺序减少对应名额。
func releaseHolds(ctx context.Context, tx pgx.Tx, conversation, onlyMember string) error {
	rows, err := tx.Query(ctx, "SELECT DISTINCT membership_id::text FROM agent_capacity_holds WHERE conversation_id=$1 AND status!='released' AND ($2='' OR membership_id=NULLIF($2,'')::uuid) ORDER BY membership_id::text", conversation, onlyMember)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, "SELECT membership_id FROM agent_runtime WHERE membership_id=$1 FOR UPDATE", id); err != nil {
			return err
		}
	}
	for _, id := range ids {
		tag, err := tx.Exec(ctx, "UPDATE agent_capacity_holds SET status='released',released_at=now() WHERE conversation_id=$1 AND membership_id=$2 AND status!='released'", conversation, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, "UPDATE agent_runtime SET current_load=current_load-$2,updated_at=now() WHERE membership_id=$1", id, tag.RowsAffected()); err != nil {
				return err
			}
		}
	}
	return nil
}

// closeLocked 在已经锁定会话后关闭咨询，重复请求不会再次释放名额。
func (s *Service) closeLocked(ctx context.Context, tx pgx.Tx, a actor, id, reason string) error {
	var status string
	if err := tx.QueryRow(ctx, "SELECT status FROM conversations WHERE id=$1", id).Scan(&status); err != nil {
		return err
	}
	if status == "closed" {
		return nil
	}
	if err := releaseHolds(ctx, tx, id, ""); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE conversation_assignments SET released_at=now() WHERE conversation_id=$1 AND released_at IS NULL", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE transfer_requests SET status='cancelled' WHERE conversation_id=$1 AND status='pending'", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE conversations SET status='closed',closed_at=now(),close_reason=$2,version=version+1,updated_at=now() WHERE id=$1", id, reason); err != nil {
		return err
	}
	_, err := s.emit(ctx, tx, a.TenantID, id, "conversation.closed", "public", map[string]any{"status": "closed", "reason": reason})
	return err
}

// actionInput 描述接待操作，预期版本可用于拒绝过期界面动作。
type actionInput struct {
	AgentID           string `json:"agent_id"`
	Target            string `json:"target"`
	ExpectedVersion   string `json:"expected_version"`
	Reason            string `json:"reason"`
	TransferRequestID string `json:"transfer_request_id"`
}

// assignmentAction 创建领取、确认、转接、拒接与结束操作处理器。
func (s *Service) assignmentAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := currentActor(r)
		id := r.PathValue("id")
		var in actionInput
		if r.ContentLength != 0 {
			if err := platform.Decode(w, r, &in); err != nil {
				replyError(w, r, err)
				return
			}
		}
		var result map[string]any
		err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
			ctx := r.Context()
			if err := canReadConversation(ctx, tx, a, id); err != nil {
				return err
			}
			v, err := lockedRoute(ctx, tx, id)
			if err != nil {
				return err
			}
			key := r.Header.Get("Idempotency-Key")
			requestHash := hash(action + id + string(jsonValue(in)))
			if key != "" {
				var previous []byte
				var previousHash string
				err := tx.QueryRow(ctx, "SELECT request_hash,result FROM chat_commands WHERE actor_id=$1 AND command_key=$2", actorID(a), action+id+key).Scan(&previousHash, &previous)
				if err == nil {
					if previousHash != requestHash {
						return platform.Fail(409, "idempotency_conflict", "同一重试编号携带了不同操作")
					}
					result = decodeJSONMap(previous)
					return nil
				}
				if err != pgx.ErrNoRows {
					return err
				}
			}
			if in.ExpectedVersion != "" && in.ExpectedVersion != strconv.FormatInt(v.Version, 10) {
				return platform.Fail(409, "version_conflict", "咨询状态已变化，请刷新后重试")
			}
			if action != "close" && a.ContactID != "" {
				return platform.Fail(403, "agent_required", "该动作需要客服身份")
			}
			switch action {
			case "claim":
				if !a.CanServe {
					return platform.Fail(403, "cannot_serve", "当前成员没有接待权限")
				}
				err = s.assignLocked(ctx, tx, a.TenantID, id, a.MemberID, true)
			case "accept", "reject":
				err = s.acceptOrReject(ctx, tx, a, v, in, action == "accept")
			case "transfer":
				if v.Agent != a.MemberID && !isAdmin(a) {
					return platform.Fail(403, "not_current_agent", "只有当前客服或管理员可以转接")
				}
				err = s.transferLocked(ctx, tx, a, v, in)
			case "close":
				if a.ContactID == "" && v.Agent != a.MemberID && !isAdmin(a) {
					return platform.Fail(403, "not_current_agent", "只有当前客服或管理员可以结束")
				}
				err = s.closeLocked(ctx, tx, a, id, strings.TrimSpace(in.Reason))
			}
			if err != nil {
				return err
			}
			result, err = conversationJSONForActor(ctx, tx, id, a)
			if err != nil {
				return err
			}
			if key != "" {
				_, err = tx.Exec(ctx, "INSERT INTO chat_commands(tenant_id,actor_id,command_key,request_hash,result) VALUES($1,$2,$3,$4,$5)", a.TenantID, actorID(a), action+id+key, requestHash, jsonValue(result))
			}
			return err
		})
		if err != nil {
			replyError(w, r, err)
			return
		}
		platform.JSON(w, 200, result)
	}
}
