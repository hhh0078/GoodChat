// 文件作用说明：在统一路由资格与容量预留内处理确认、拒绝及转接，避免配置变化绕过技能约束。
package chat

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

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
		if err := requireCandidate(ctx, tx, v, a.MemberID, true); err != nil {
			return err
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
	if err := requireCandidate(ctx, tx, v, in.AgentID, false); err != nil {
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
	if err := requireCandidate(ctx, tx, v, target, true); err != nil {
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
