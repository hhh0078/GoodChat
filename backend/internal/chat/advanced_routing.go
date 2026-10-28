// 文件作用说明：统一技能资格、可选技能排序、历史接待偏好和平滑加权轮询，决策与容量占位共用当前事务。
package chat

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
)

// routeCandidate 保存经过统一资格过滤的候选及当前算法需要的指标。
type routeCandidate struct {
	ID               string
	Weight, Optional int
	Score            int64
}

// loadRouteSkills 合并入口与池的技能约束并锁定技能字典，归档必备技能不能被静默忽略。
func loadRouteSkills(ctx context.Context, tx pgx.Tx, pool, link string) ([]string, []string, error) {
	rows, err := tx.Query(ctx, `SELECT skill_id::text,bool_or(is_required) FROM (SELECT skill_id,is_required FROM routing_pool_skills WHERE routing_pool_id=$1 UNION ALL SELECT skill_id,is_required FROM access_link_skills WHERE access_link_id=$2) rules GROUP BY skill_id ORDER BY skill_id`, pool, link)
	if err != nil {
		return nil, nil, err
	}
	required, optional := []string{}, []string{}
	for rows.Next() {
		var id string
		var mandatory bool
		if err := rows.Scan(&id, &mandatory); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if mandatory {
			required = append(required, id)
		} else {
			optional = append(optional, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	ids := append(append([]string{}, required...), optional...)
	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, "SELECT id FROM routing_skills WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE", ids); err != nil {
			return nil, nil, err
		}
	}
	return required, optional, nil
}

// routingCandidates 对全部算法、人工领取和转接使用相同组池、在线忙闲、容量与必备技能条件。
func routingCandidates(ctx context.Context, tx pgx.Tx, v routeState) ([]routeCandidate, error) {
	order := "r.current_load,m.id"
	if v.Algorithm == "least_ratio" {
		order = "r.current_load::numeric/m.capacity,m.id"
	}
	if v.Algorithm == "random" {
		order = "random()"
	}
	if v.Algorithm == "round_robin" || v.Algorithm == "weighted_round_robin" {
		order = "m.id"
	}
	rows, err := tx.Query(ctx, `SELECT m.id::text,COALESCE(pm.weight,1),
 (SELECT count(*) FROM member_routing_skills ms JOIN routing_skills sk ON sk.id=ms.skill_id AND sk.tenant_id=ms.tenant_id WHERE ms.membership_id=m.id AND ms.skill_id=ANY($3::uuid[]) AND NOT sk.archived)::integer,
 COALESCE(ws.current_weight,0)
 FROM tenant_memberships m JOIN agent_runtime r ON r.membership_id=m.id AND r.tenant_id=m.tenant_id
 JOIN agent_group_members gm ON gm.membership_id=m.id AND gm.tenant_id=m.tenant_id
 JOIN routing_pools p ON p.id=$1 AND p.tenant_id=m.tenant_id JOIN agent_groups g ON g.id=p.service_group_id AND g.tenant_id=p.tenant_id
 JOIN conversations c ON c.id=$2 AND c.tenant_id=m.tenant_id
 LEFT JOIN routing_pool_members pm ON pm.routing_pool_id=p.id AND pm.membership_id=m.id AND pm.tenant_id=m.tenant_id
 LEFT JOIN routing_weight_state ws ON ws.routing_pool_id=p.id AND ws.membership_id=m.id AND ws.tenant_id=m.tenant_id
 WHERE gm.group_id=c.service_group_id AND p.service_group_id=c.service_group_id AND m.can_serve AND NOT m.disabled AND m.removed_at IS NULL
 AND (m.capacity>r.current_load OR (m.id=NULLIF($5,'')::uuid AND m.capacity>=r.current_load))
 AND r.available AND NOT r.busy AND r.online_until>statement_timestamp() AND NOT p.archived AND NOT g.archived
 AND NOT(c.rejected_agents ? m.id::text) AND (p.members_mode='group' OR pm.membership_id IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM unnest($4::uuid[]) requirement(skill_id) WHERE NOT EXISTS(SELECT 1 FROM member_routing_skills ms JOIN routing_skills sk ON sk.id=ms.skill_id AND sk.tenant_id=ms.tenant_id WHERE ms.membership_id=m.id AND ms.skill_id=requirement.skill_id AND NOT sk.archived))
 ORDER BY `+order, v.Pool, v.ID, v.Optional, v.Required, v.ReservedFor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []routeCandidate{}
	for rows.Next() {
		var item routeCandidate
		if err := rows.Scan(&item.ID, &item.Weight, &item.Optional, &item.Score); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// requireCandidate 在成员配置锁内重判资格；确认已有预留允许刚好达到容量，但不能借预留增加新名额。
func requireCandidate(ctx context.Context, tx pgx.Tx, v routeState, member string, reserved bool) error {
	if _, err := tx.Exec(ctx, "SELECT r.membership_id FROM agent_runtime r JOIN tenant_memberships m ON m.id=r.membership_id AND m.tenant_id=r.tenant_id WHERE m.id=$1 FOR UPDATE OF r,m", member); err != nil {
		return err
	}
	if reserved {
		v.ReservedFor = member
	}
	items, err := routingCandidates(ctx, tx, v)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID == member {
			return nil
		}
	}
	return candidateFailure(ctx, tx, v, member)
}

// preferredCandidates 在技能合格候选内执行历史偏好和可选技能排序，指定客服的优先级由调用方先行处理。
func preferredCandidates(ctx context.Context, tx pgx.Tx, v routeState, items []routeCandidate) ([]routeCandidate, string, error) {
	if v.ID != "" {
		var previous *string
		// 当前指针明确记录最终接待；旧指针缺失时只用唯一冻结结束水位，未知段不按事务时间或随机编号猜测。
		err := tx.QueryRow(ctx, `SELECT coalesce(
 (SELECT final.membership_id::text FROM conversation_assignments final WHERE final.id=past.current_assignment_id AND final.tenant_id=past.tenant_id AND final.conversation_id=past.id AND final.membership_id=past.current_agent_id AND final.accepted_at IS NOT NULL),
 (SELECT min(a.membership_id::text) FROM conversation_assignments a
 WHERE a.tenant_id=past.tenant_id AND a.conversation_id=past.id AND a.accepted_at IS NOT NULL AND a.visibility_through_seq IS NOT NULL
 AND a.visibility_through_seq=(SELECT max(last.visibility_through_seq) FROM conversation_assignments last WHERE last.tenant_id=past.tenant_id AND last.conversation_id=past.id AND last.accepted_at IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM conversation_assignments unknown WHERE unknown.tenant_id=past.tenant_id AND unknown.conversation_id=past.id AND unknown.accepted_at IS NOT NULL AND unknown.visibility_through_seq IS NULL)
 HAVING count(*)=1))
 FROM conversations past JOIN conversations current ON current.id=$1 AND current.tenant_id=past.tenant_id
 WHERE past.contact_id=current.contact_id AND past.service_space_id=current.service_space_id AND past.status='closed' AND past.id<>current.id AND past.service_group_id=current.service_group_id
 AND (coalesce((SELECT (settings->>'history_agent_minutes')::integer FROM chat_settings WHERE tenant_id=current.tenant_id),0)=0 OR coalesce(past.last_message_at,past.created_at)>=statement_timestamp()-coalesce((SELECT (settings->>'history_agent_minutes')::integer FROM chat_settings WHERE tenant_id=current.tenant_id),0)*interval '1 minute')
 ORDER BY coalesce(past.last_message_at,past.created_at) DESC LIMIT 1`, v.ID).Scan(&previous)
		if err != nil && err != pgx.ErrNoRows {
			return nil, "", err
		}
		for _, item := range items {
			if previous != nil && item.ID == *previous {
				return []routeCandidate{item}, "history", nil
			}
		}
	}
	if len(items) == 0 {
		return items, "algorithm", nil
	}
	best := 0
	for _, item := range items {
		if item.Optional > best {
			best = item.Optional
		}
	}
	preferred := []routeCandidate{}
	for _, item := range items {
		if item.Optional == best {
			preferred = append(preferred, item)
		}
	}
	if v.Algorithm == "round_robin" {
		offset := int(v.Cursor % int64(len(preferred)))
		preferred = append(preferred[offset:], preferred[:offset]...)
	}
	if v.Algorithm == "weighted_round_robin" {
		sort.SliceStable(preferred, func(i, j int) bool {
			left, right := preferred[i].Score+int64(preferred[i].Weight), preferred[j].Score+int64(preferred[j].Weight)
			if left == right {
				return preferred[i].ID < preferred[j].ID
			}
			return left > right
		})
	}
	return preferred, "algorithm", nil
}

// advanceWeights 仅在成功分配事务内推进本次算法候选积分，指定、历史偏好和幂等重放均不消耗加权轮次。
func advanceWeights(ctx context.Context, tx pgx.Tx, tenant, pool, selected string, items []routeCandidate) error {
	var total int64
	for _, item := range items {
		total += int64(item.Weight)
	}
	for _, item := range items {
		delta := int64(item.Weight)
		if item.ID == selected {
			delta -= total
		}
		if _, err := tx.Exec(ctx, `INSERT INTO routing_weight_state(tenant_id,routing_pool_id,membership_id,current_weight) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,routing_pool_id,membership_id) DO UPDATE SET current_weight=routing_weight_state.current_weight+EXCLUDED.current_weight`, tenant, pool, item.ID, delta); err != nil {
			return err
		}
	}
	return nil
}
