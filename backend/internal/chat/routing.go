// 文件作用说明：以固定锁顺序和数据库容量预留实现分配、接单、转接与关闭。
package chat

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// routeState 保存已锁定会话与分配池的决策字段。
type routeState struct {
	ID, Pool, Group, Status, Agent, Desired, Mode, Algorithm, AssignmentMode string
	Cursor, Version                                                          int64
}

// lockedRoute 按队列分片在前、会话在后的顺序取得事务锁。
func lockedRoute(ctx context.Context, tx pgx.Tx, id string) (routeState, error) {
	var p string
	var v routeState
	if err := tx.QueryRow(ctx, "SELECT routing_pool_id::text FROM conversations WHERE id=$1", id).Scan(&p); err != nil {
		return v, err
	}
	if _, err := tx.Exec(ctx, "SELECT id FROM routing_pools WHERE id=$1 FOR UPDATE", p); err != nil {
		return v, err
	}
	err := tx.QueryRow(ctx, "SELECT c.id::text,c.routing_pool_id::text,c.service_group_id::text,c.status,COALESCE(c.current_agent_id::text,''),COALESCE(c.desired_agent_id::text,''),c.agent_mode,p.algorithm,p.assignment_mode,p.rr_cursor,c.version FROM conversations c JOIN routing_pools p ON p.id=c.routing_pool_id AND p.tenant_id=c.tenant_id WHERE c.id=$1 FOR UPDATE OF c", id).Scan(&v.ID, &v.Pool, &v.Group, &v.Status, &v.Agent, &v.Desired, &v.Mode, &v.Algorithm, &v.AssignmentMode, &v.Cursor, &v.Version)
	return v, err
}

// candidateAgents 选择当前可接待成员，所有算法共享相同资格与容量过滤。
func candidateAgents(ctx context.Context, tx pgx.Tx, v routeState) ([]string, error) {
	order := "r.current_load,m.id"
	if v.Algorithm == "least_ratio" {
		order = "r.current_load::numeric/m.capacity,m.id"
	}
	if v.Algorithm == "random" {
		order = "random()"
	}
	if v.Algorithm == "round_robin" {
		order = "m.id"
	}
	rows, err := tx.Query(ctx, "SELECT m.id::text FROM tenant_memberships m JOIN agent_runtime r ON r.membership_id=m.id AND r.tenant_id=m.tenant_id JOIN agent_group_members gm ON gm.membership_id=m.id AND gm.tenant_id=m.tenant_id JOIN routing_pools p ON p.id=$1 AND p.tenant_id=m.tenant_id JOIN conversations c ON c.id=$2 AND c.tenant_id=m.tenant_id WHERE gm.group_id=c.service_group_id AND m.can_serve AND NOT m.disabled AND m.capacity>r.current_load AND r.available AND NOT r.busy AND r.online_until>now() AND NOT p.archived AND NOT(c.rejected_agents ? m.id::text) AND (p.members_mode='group' OR EXISTS(SELECT 1 FROM routing_pool_members pm WHERE pm.routing_pool_id=p.id AND pm.membership_id=m.id)) ORDER BY "+order, v.Pool, v.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// lockCapacity 在运行行和成员行均锁定后再次检查当前资格与名额。
func lockCapacity(ctx context.Context, tx pgx.Tx, tenant, member string) error {
	var valid bool
	err := tx.QueryRow(ctx, "SELECT m.can_serve AND NOT m.disabled AND r.available AND NOT r.busy AND r.online_until>now() AND r.current_load<m.capacity FROM agent_runtime r JOIN tenant_memberships m ON m.id=r.membership_id AND m.tenant_id=r.tenant_id WHERE r.membership_id=$1 AND r.tenant_id=$2 FOR UPDATE OF r,m", member, tenant).Scan(&valid)
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
	ids, err := candidateAgents(ctx, tx, v)
	if err != nil {
		return err
	}
	if requested != "" {
		if !inStrings(requested, ids...) {
			return platform.Fail(409, "candidate_unavailable", "当前客服不在可接待范围或已满载")
		}
		ids = []string{requested}
	} else if v.Desired != "" {
		if inStrings(v.Desired, ids...) {
			ids = append([]string{v.Desired}, removeString(ids, v.Desired)...)
		} else if v.Mode == "strict" {
			return nil
		}
	} else if v.Algorithm == "round_robin" && len(ids) > 0 {
		offset := int(v.Cursor % int64(len(ids)))
		ids = append(ids[offset:], ids[:offset]...)
	}
	if len(ids) == 0 {
		return nil
	}
	selected := ids[0]
	if err := lockCapacity(ctx, tx, tenant, selected); err != nil {
		if manual {
			return err
		}
		return nil
	}
	return s.createAssignment(ctx, tx, tenant, v, selected, v.AssignmentMode == "confirm")
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
	status, hold := "active", "active"
	if confirm {
		status, hold = "assigned", "held"
	}
	_, err := tx.Exec(ctx, "INSERT INTO conversation_assignments(id,tenant_id,conversation_id,membership_id,accepted_at,expires_at) VALUES($1,$2,$3,$4,CASE WHEN $5 THEN NULL ELSE now() END,CASE WHEN $5 THEN now()+interval '30 seconds' ELSE NULL END)", id, tenant, v.ID, member, confirm)
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

// acceptOrReject 处理首次接待及独立转接预留，不重复增加目标名额。
func (s *Service) acceptOrReject(ctx context.Context, tx pgx.Tx, a actor, v routeState, in actionInput, accept bool) error {
	if in.TransferRequestID != "" {
		return s.finishTransfer(ctx, tx, a, v, in.TransferRequestID, accept)
	}
	if v.Agent != a.MemberID {
		return platform.Fail(403, "not_target_agent", "只有被分配客服可以确认")
	}
	if v.Status != "assigned" {
		if accept && v.Status == "active" {
			return nil
		}
		return platform.Fail(409, "assignment_changed", "当前接待已不再等待确认")
	}
	if accept {
		var valid bool
		if err := tx.QueryRow(ctx, "SELECT expires_at>now() FROM conversation_assignments WHERE id=(SELECT current_assignment_id FROM conversations WHERE id=$1)", v.ID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return platform.Fail(409, "assignment_expired", "接单预留已经过期")
		}
		if _, err := tx.Exec(ctx, "UPDATE conversation_assignments SET accepted_at=now(),expires_at=NULL WHERE conversation_id=$1 AND released_at IS NULL", v.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE agent_capacity_holds SET status='active',expires_at=NULL WHERE conversation_id=$1 AND assignment_id IS NOT NULL AND status='held'", v.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE conversations SET status='active',version=version+1,updated_at=now() WHERE id=$1", v.ID); err != nil {
			return err
		}
	} else {
		if err := releaseHolds(ctx, tx, v.ID, a.MemberID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE conversation_assignments SET released_at=now() WHERE conversation_id=$1 AND released_at IS NULL", v.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE conversations SET status='queued',current_agent_id=NULL,current_assignment_id=NULL,rejected_agents=rejected_agents||jsonb_build_object($2::text,true),version=version+1,updated_at=now() WHERE id=$1", v.ID, a.MemberID); err != nil {
			return err
		}
	}
	_, err := s.emit(ctx, tx, a.TenantID, v.ID, "assignment.changed", "public", map[string]any{"accepted": accept})
	return err
}

// transferLocked 检查目标资格并保留原接待，确认模式先创建独立容量预留。
func (s *Service) transferLocked(ctx context.Context, tx pgx.Tx, a actor, v routeState, in actionInput) error {
	if v.Status != "active" {
		return platform.Fail(409, "conversation_not_active", "只有接待中的咨询可以转接")
	}
	if in.Target == "queue" {
		if err := releaseHolds(ctx, tx, v.ID, ""); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE conversation_assignments SET released_at=now() WHERE conversation_id=$1 AND released_at IS NULL", v.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE transfer_requests SET status='cancelled' WHERE conversation_id=$1 AND status='pending'", v.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE conversations SET status='queued',current_agent_id=NULL,current_assignment_id=NULL,routing_attempts=0,rejected_agents='{}',version=version+1,updated_at=now() WHERE id=$1", v.ID)
		return err
	}
	if !uuidPattern.MatchString(in.AgentID) || in.AgentID == v.Agent {
		return platform.Fail(400, "invalid_target", "请选择其他有效客服")
	}
	ids, err := candidateAgents(ctx, tx, v)
	if err != nil {
		return err
	}
	if !inStrings(in.AgentID, ids...) {
		return platform.Fail(409, "target_unavailable", "目标客服不在允许范围或当前不可接待")
	}
	locks := []string{v.Agent, in.AgentID}
	sort.Strings(locks)
	for _, id := range locks {
		if _, err := tx.Exec(ctx, "SELECT membership_id FROM agent_runtime WHERE membership_id=$1 FOR UPDATE", id); err != nil {
			return err
		}
	}
	if err := lockCapacity(ctx, tx, a.TenantID, in.AgentID); err != nil {
		return err
	}
	if v.AssignmentMode == "confirm" {
		request := newID()
		if _, err := tx.Exec(ctx, "INSERT INTO transfer_requests(id,tenant_id,conversation_id,from_agent_id,target_agent_id) VALUES($1,$2,$3,$4,$5)", request, a.TenantID, v.ID, v.Agent, in.AgentID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO agent_capacity_holds(tenant_id,conversation_id,membership_id,transfer_request_id,status,expires_at) VALUES($1,$2,$3,$4,'held',now()+interval '30 seconds')", a.TenantID, v.ID, in.AgentID, request); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE agent_runtime SET current_load=current_load+1 WHERE membership_id=$1", in.AgentID); err != nil {
			return err
		}
		_, err = s.emit(ctx, tx, a.TenantID, v.ID, "transfer.requested", "public", map[string]any{"transfer_request_id": request, "target_agent_id": in.AgentID})
		return err
	}
	if err := releaseHolds(ctx, tx, v.ID, v.Agent); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE conversation_assignments SET released_at=now() WHERE conversation_id=$1 AND released_at IS NULL", v.ID); err != nil {
		return err
	}
	return s.createAssignment(ctx, tx, a.TenantID, v, in.AgentID, false)
}

// finishTransfer 原子确认或拒绝转接，确认时把预留转正式而不是再加一个名额。
func (s *Service) finishTransfer(ctx context.Context, tx pgx.Tx, a actor, v routeState, request string, accept bool) error {
	var target, from, status string
	var valid bool
	err := tx.QueryRow(ctx, "SELECT target_agent_id::text,from_agent_id::text,status,expires_at>now() FROM transfer_requests WHERE id=$1 AND conversation_id=$2 FOR UPDATE", request, v.ID).Scan(&target, &from, &status, &valid)
	if err != nil {
		return err
	}
	if target != a.MemberID {
		return platform.Fail(403, "not_transfer_target", "只有目标客服可以确认转接")
	}
	if status != "pending" || !valid || v.Status != "active" || v.Agent != from {
		return platform.Fail(409, "transfer_expired", "转接请求已失效")
	}
	ids := []string{from, target}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := tx.Exec(ctx, "SELECT membership_id FROM agent_runtime WHERE membership_id=$1 FOR UPDATE", id); err != nil {
			return err
		}
	}
	if !accept {
		if err := releaseHolds(ctx, tx, v.ID, target); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE transfer_requests SET status='rejected' WHERE id=$1", request)
		return err
	}
	if err := releaseHolds(ctx, tx, v.ID, from); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE conversation_assignments SET released_at=now() WHERE conversation_id=$1 AND released_at IS NULL", v.ID); err != nil {
		return err
	}
	assignment := newID()
	if _, err := tx.Exec(ctx, "INSERT INTO conversation_assignments(id,tenant_id,conversation_id,membership_id,accepted_at) VALUES($1,$2,$3,$4,now())", assignment, a.TenantID, v.ID, target); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE agent_capacity_holds SET status='active',assignment_id=$2,expires_at=NULL WHERE transfer_request_id=$1 AND status='held'", request, assignment); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE transfer_requests SET status='accepted' WHERE id=$1", request); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE conversations SET current_agent_id=$2,current_assignment_id=$3,version=version+1,updated_at=now() WHERE id=$1", v.ID, target, assignment); err != nil {
		return err
	}
	_, err = s.emit(ctx, tx, a.TenantID, v.ID, "assignment.changed", "public", map[string]any{"agent_id": target, "transfer_request_id": request})
	return err
}
