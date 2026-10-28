// 文件作用说明：按可信访客身份和服务空间恢复咨询，以公开消息活动窗口重新匹配，并保证指定客服不降级到其他客服或排队。
package chat

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// visitorConversationResult 保存本次服务器判定的恢复结果，浏览器缓存时间不参与有效窗口计算。
type visitorConversationResult struct {
	ID, PreviousID   string
	Resumed, Expired bool
}

// specifiedAgentOnline 对显式指定目标使用稳定错误，离线不得静默排队或改派其他客服。
func specifiedAgentOnline(ctx context.Context, tx pgx.Tx, member string) error {
	if member == "" {
		return nil
	}
	var online bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runtime r JOIN tenant_memberships m ON m.id=r.membership_id AND m.tenant_id=r.tenant_id WHERE m.id=$1 AND r.available AND r.online_until>statement_timestamp() AND NOT m.disabled AND m.removed_at IS NULL)`, member).Scan(&online); err != nil {
		return err
	}
	if !online {
		return platform.Fail(409, "specified_agent_offline", "无客服在线，暂时无法访问")
	}
	return nil
}

// visitorConversation 同事务锁定原池和目标池，按同入口同路由与最后公开消息判定恢复，过期咨询关闭后保留历史。
func (s *Service) visitorConversation(ctx context.Context, tx pgx.Tx, a actor, entry map[string]any, in visitorInput, tracking map[string]any, newVisit bool, minutes int) (visitorConversationResult, error) {
	result := visitorConversationResult{}
	target, err := resolveRouting(ctx, tx, entry, in.Routing)
	if err != nil {
		return result, err
	}
	if err := specifiedAgentOnline(ctx, tx, target.AgentID); err != nil {
		return result, err
	}
	if in.ResumeConversationID != "" {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND contact_id=$2 AND service_space_id=$3)`, in.ResumeConversationID, a.ContactID, entry["service_space_id"]).Scan(&valid); err != nil {
			return result, err
		}
		if !valid {
			return result, platform.Fail(403, "resume_not_allowed", "恢复提示不属于当前访客或服务空间")
		}
	}
	// 所有访客入口先锁客户行，随后遵循池、会话、成员运行行的已有锁顺序。
	if _, err := tx.Exec(ctx, "SELECT id FROM contacts WHERE id=$1 FOR UPDATE", a.ContactID); err != nil {
		return result, err
	}
	var oldPool string
	err = tx.QueryRow(ctx, "SELECT routing_pool_id::text FROM conversations WHERE contact_id=$1 AND service_space_id=$2 AND status!='closed'", a.ContactID, entry["service_space_id"]).Scan(&oldPool)
	if err != nil && err != pgx.ErrNoRows {
		return result, err
	}
	pools := []string{target.PoolID}
	if oldPool != "" && oldPool != target.PoolID {
		pools = append(pools, oldPool)
	}
	sort.Strings(pools)
	for _, pool := range pools {
		if _, err := tx.Exec(ctx, "SELECT id FROM routing_pools WHERE id=$1 FOR UPDATE", pool); err != nil {
			return result, err
		}
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", a.TenantID+a.ContactID+entry["service_space_id"].(string)); err != nil {
		return result, err
	}
	var id, link, group, pool, desired, mode, agent string
	var within bool
	var contextRaw []byte
	err = tx.QueryRow(ctx, `SELECT id::text,created_entry_id::text,service_group_id::text,routing_pool_id::text,coalesce(desired_agent_id::text,''),agent_mode,coalesce(current_agent_id::text,''),($3=0 OR coalesce(last_message_at,created_at)>=statement_timestamp()-$3*interval '1 minute'),context FROM conversations WHERE contact_id=$1 AND service_space_id=$2 AND status!='closed' FOR UPDATE`, a.ContactID, entry["service_space_id"], minutes).Scan(&id, &link, &group, &pool, &desired, &mode, &agent, &within, &contextRaw)
	merged := map[string]any{}
	if err == nil {
		sameEntry := link == entry["id"].(string)
		sameTarget := sameEntry && group == target.GroupID && pool == target.PoolID && desired == target.AgentID && mode == target.AgentMode
		reusable := sameTarget && within
		if reusable && agent != "" {
			v, e := lockedRoute(ctx, tx, id)
			if e != nil {
				return result, e
			}
			e = requireCandidate(ctx, tx, v, agent, true)
			if e != nil {
				var api *platform.APIError
				if !errors.As(e, &api) {
					return result, e
				}
				if target.AgentID != "" {
					if offline := specifiedAgentOnline(ctx, tx, target.AgentID); offline != nil {
						return result, offline
					}
					return result, e
				}
				reusable = false
			}
		}
		if reusable {
			result.ID = id
			result.Resumed = true
			if newVisit && len(in.Context) > 0 {
				merged = decodeJSONMap(contextRaw)
				for key, value := range in.Context {
					merged[key] = value
				}
				if err := validateContext(merged); err != nil {
					return result, err
				}
				if _, err := tx.Exec(ctx, "UPDATE conversations SET context=$2 WHERE id=$1", id, jsonValue(merged)); err != nil {
					return result, err
				}
			}
			// 排队咨询在客服上线后可以重新尝试分配，显式目标仍不能转为普通排队。
			if target.AgentID != "" {
				err = s.assignLocked(ctx, tx, a.TenantID, id, target.AgentID, true)
			} else {
				err = s.assignLocked(ctx, tx, a.TenantID, id, "", false)
			}
			return result, err
		}
		result.PreviousID = id
		result.Expired = !within
		if sameEntry {
			merged = decodeJSONMap(contextRaw)
		}
		reason := "visitor_routing_changed"
		if !within {
			reason = "history_window_expired"
		} else if sameTarget {
			reason = "history_agent_unavailable"
		}
		if err := s.closeLocked(ctx, tx, a, id, reason); err != nil {
			return result, err
		}
	} else if err != pgx.ErrNoRows {
		return result, err
	}
	if err := s.checkFeature(ctx, tx, a.TenantID, "chat"); err != nil {
		return result, err
	}
	for key, value := range in.Context {
		merged[key] = value
	}
	if err := validateContext(merged); err != nil {
		return result, err
	}
	result.ID = newID()
	_, err = tx.Exec(ctx, `INSERT INTO conversations(id,tenant_id,service_space_id,contact_id,created_entry_id,service_group_id,routing_pool_id,desired_agent_id,agent_mode,source_snapshot,context) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10,$11)`, result.ID, a.TenantID, entry["service_space_id"], a.ContactID, entry["id"], target.GroupID, target.PoolID, target.AgentID, target.AgentMode, jsonValue(map[string]any{"entry": entry, "tracking": tracking, "routing": target}), jsonValue(merged))
	if err != nil {
		return result, err
	}
	if _, err := s.emit(ctx, tx, a.TenantID, result.ID, "conversation.created", "public", map[string]any{"status": "queued"}); err != nil {
		return result, err
	}
	if target.AgentID != "" {
		if err := specifiedAgentOnline(ctx, tx, target.AgentID); err != nil {
			return result, err
		}
		err = s.assignLocked(ctx, tx, a.TenantID, result.ID, target.AgentID, true)
		if err != nil {
			if offline := specifiedAgentOnline(ctx, tx, target.AgentID); offline != nil {
				return result, offline
			}
			return result, err
		}
	} else if err := s.assignLocked(ctx, tx, a.TenantID, result.ID, "", false); err != nil {
		return result, err
	}
	if err := s.enqueueWelcome(ctx, tx, a.TenantID, result.ID); err != nil {
		return result, err
	}
	return result, nil
}
