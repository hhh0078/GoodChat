// 文件作用说明：维护租户技能、客服技能、池与入口技能规则及权重，配置变更遵循与分配相同的池锁顺序。
package chat

import (
	"context"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// routingExtras 保存资源JSON中不直接对应主表列的路由关联配置。
type routingExtras struct {
	Required, Optional       []string
	HasRequired, HasOptional bool
	Weights                  map[string]int
}

// registerAdvancedRouting 注册租户管理员维护客服技能的真实持久化接口。
func (s *Service) registerAdvancedRouting(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenant/routing-members", s.auth.RequireAdmin(s.routingMembers))
	mux.HandleFunc("PATCH /api/v1/tenant/routing-members/{id}", s.auth.RequireAdmin(s.routingMembers))
}

// routingMembers 查看成员技能或原子替换指定成员技能，成员行锁与最终分配复核配合避免资格竞态。
func (s *Service) routingMembers(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var ids []string
	if r.Method == http.MethodPatch {
		var input struct {
			Skills *[]any `json:"skill_ids"`
		}
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
		var err error
		if input.Skills == nil {
			returnError(w, r, "skills_required", "请明确提交skill_ids数组")
			return
		}
		ids, err = stringIDs(*input.Skills)
		if err != nil {
			replyError(w, r, err)
			return
		}
		if !uuidPattern.MatchString(r.PathValue("id")) {
			returnError(w, r, "invalid_id", "客服编号不正确")
			return
		}
	}
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodPatch {
			var member string
			// 技能排序也必须与分配处于同一池临界区，避免读取候选后技能被改写而优先级漂移。
			if _, err := tx.Exec(ctx, "SELECT p.id FROM routing_pools p JOIN agent_group_members gm ON gm.group_id=p.service_group_id AND gm.tenant_id=p.tenant_id WHERE gm.membership_id=$1 ORDER BY p.id FOR UPDATE OF p", r.PathValue("id")); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, "SELECT id::text FROM tenant_memberships WHERE id=$1 AND tenant_id=$2 AND removed_at IS NULL FOR UPDATE", r.PathValue("id"), a.TenantID).Scan(&member); err != nil {
				return err
			}
			if err := validSkills(ctx, tx, ids); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "DELETE FROM member_routing_skills WHERE membership_id=$1", member); err != nil {
				return err
			}
			for _, skill := range ids {
				if _, err := tx.Exec(ctx, "INSERT INTO member_routing_skills(tenant_id,membership_id,skill_id) VALUES($1,$2,$3)", a.TenantID, member, skill); err != nil {
					return err
				}
			}
			if err := audit(ctx, tx, a, "routing.member_skills", member, map[string]any{"skill_ids": ids}); err != nil {
				return err
			}
			result = map[string]any{"id": member, "skill_ids": ids}
			return nil
		}
		rows, err := tx.Query(ctx, `SELECT jsonb_build_object('id',m.id,'display_name',COALESCE(NULLIF(m.display_name,''),u.display_name),'can_serve',m.can_serve,'disabled',m.disabled,'skill_ids',COALESCE((SELECT jsonb_agg(skill_id ORDER BY skill_id) FROM member_routing_skills WHERE membership_id=m.id),'[]'::jsonb)) FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 AND m.removed_at IS NULL ORDER BY m.created_at,m.id`, a.TenantID)
		if err != nil {
			return err
		}
		items, err := jsonRows(rows)
		result = map[string]any{"items": items}
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// validSkills 拒绝不存在、跨租户或归档的技能编号，空数组表示明确清空配置。
func validSkills(ctx context.Context, tx pgx.Tx, ids []string) error {
	if len(ids) > 100 {
		return platform.Fail(400, "too_many_skills", "最多配置100项技能")
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM routing_skills WHERE id=ANY($1::uuid[]) AND NOT archived", ids).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return platform.Fail(400, "skill_not_available", "技能不存在、已归档或不属于当前租户")
	}
	return nil
}

// prepareRoutingExtras 读取并校验关联字段，锁池后才允许修改入口或池，避免与分配的锁顺序相反。
func prepareRoutingExtras(ctx context.Context, tx pgx.Tx, name, id string, values map[string]any) (routingExtras, error) {
	extra := routingExtras{}
	if name == "groups" && id != "" {
		// 先锁关联池再修改组成员或归档状态，分配持有同一池锁，因此不会在提交前丢失组资格。
		_, err := tx.Exec(ctx, "SELECT id FROM routing_pools WHERE service_group_id=$1 ORDER BY id FOR UPDATE", id)
		return extra, err
	}
	if name != "routing-pools" && name != "access-links" {
		return extra, nil
	}
	for _, field := range []string{"required_skill_ids", "optional_skill_ids"} {
		if raw, ok := values[field]; ok {
			ids, err := stringIDs(raw)
			if err != nil {
				return extra, err
			}
			if err = validSkills(ctx, tx, ids); err != nil {
				return extra, err
			}
			if field == "required_skill_ids" {
				extra.Required, extra.HasRequired = ids, true
			} else {
				extra.Optional, extra.HasOptional = ids, true
			}
			delete(values, field)
		}
	}
	if raw, ok := values["member_weights"]; ok {
		weights, valid := raw.(map[string]any)
		if !valid || len(weights) > 1000 {
			return extra, platform.Fail(400, "invalid_weights", "权重必须是客服编号到1至1000整数的映射")
		}
		extra.Weights = map[string]int{}
		for member, value := range weights {
			number, ok := value.(float64)
			if !uuidPattern.MatchString(member) || !ok || number < 1 || number > 1000 || number != float64(int(number)) {
				return extra, platform.Fail(400, "invalid_weights", "每个客服权重必须是1至1000整数")
			}
			extra.Weights[member] = int(number)
		}
		delete(values, "member_weights")
	}
	pools := []string{}
	if name == "routing-pools" && id != "" {
		pools = append(pools, id)
	}
	if name == "access-links" {
		if id != "" {
			var previous string
			if err := tx.QueryRow(ctx, "SELECT routing_pool_id::text FROM access_links WHERE id=$1", id).Scan(&previous); err != nil {
				return extra, err
			}
			pools = append(pools, previous)
		}
		if pool, ok := values["routing_pool_id"].(string); ok && pool != "" {
			pools = append(pools, pool)
		}
	}
	sort.Strings(pools)
	for _, pool := range pools {
		var found string
		if err := tx.QueryRow(ctx, "SELECT id::text FROM routing_pools WHERE id=$1 FOR UPDATE", pool).Scan(&found); err != nil {
			return extra, err
		}
	}
	return extra, nil
}

// saveRoutingExtras 在主资源写入后原子维护规则和权重，失败时主资源也随事务回滚。
func saveRoutingExtras(ctx context.Context, tx pgx.Tx, name, id string, extra routingExtras) error {
	if name != "routing-pools" && name != "access-links" {
		return nil
	}
	table, foreign := "routing_pool_skills", "routing_pool_id"
	if name == "access-links" {
		table, foreign = "access_link_skills", "access_link_id"
	}
	if extra.HasRequired || extra.HasOptional {
		if !extra.HasRequired || !extra.HasOptional {
			rows, err := tx.Query(ctx, "SELECT skill_id::text,is_required FROM "+table+" WHERE "+foreign+"=$1", id)
			if err != nil {
				return err
			}
			for rows.Next() {
				var skill string
				var required bool
				if err := rows.Scan(&skill, &required); err != nil {
					rows.Close()
					return err
				}
				if required && !extra.HasRequired {
					extra.Required = append(extra.Required, skill)
				} else if !required && !extra.HasOptional {
					extra.Optional = append(extra.Optional, skill)
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		for _, skill := range extra.Optional {
			if inStrings(skill, extra.Required...) {
				return platform.Fail(400, "overlapping_skills", "同一技能不能同时配置为必备和可选")
			}
		}
		if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE "+foreign+"=$1", id); err != nil {
			return err
		}
		for _, group := range []struct {
			ids      []string
			required bool
		}{{extra.Required, true}, {extra.Optional, false}} {
			for _, skill := range group.ids {
				if _, err := tx.Exec(ctx, "INSERT INTO "+table+"(tenant_id,"+foreign+",skill_id,is_required) VALUES(current_tenant_id(),$1,$2,$3)", id, skill, group.required); err != nil {
					return err
				}
			}
		}
	}
	if name == "routing-pools" {
		if extra.Weights != nil {
			if _, err := tx.Exec(ctx, "UPDATE routing_pool_members SET weight=1 WHERE routing_pool_id=$1", id); err != nil {
				return err
			}
		}
		for member, weight := range extra.Weights {
			var valid bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM routing_pools p JOIN agent_group_members gm ON gm.group_id=p.service_group_id AND gm.tenant_id=p.tenant_id JOIN tenant_memberships m ON m.id=gm.membership_id AND m.tenant_id=gm.tenant_id WHERE p.id=$1 AND m.id=$2 AND m.removed_at IS NULL AND (p.members_mode='group' OR EXISTS(SELECT 1 FROM routing_pool_members pm WHERE pm.routing_pool_id=p.id AND pm.membership_id=m.id)))`, id, member).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return platform.Fail(400, "weight_member_not_allowed", "权重客服必须属于当前业务组与分配池，不能引用其他租户成员")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO routing_pool_members(tenant_id,routing_pool_id,membership_id,weight) VALUES(current_tenant_id(),$1,$2,$3) ON CONFLICT(tenant_id,routing_pool_id,membership_id) DO UPDATE SET weight=EXCLUDED.weight`, id, member, weight); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, "DELETE FROM routing_weight_state WHERE routing_pool_id=$1", id)
		return err
	}
	return nil
}

// routingSkillsProjection 返回资源的规范技能数组，前端读回同一配置而非生成模拟默认值。
func routingSkillsProjection(table, foreign string) string {
	return `||jsonb_build_object('required_skill_ids',COALESCE((SELECT jsonb_agg(skill_id ORDER BY skill_id) FROM ` + table + ` WHERE ` + foreign + `=t.id AND is_required),'[]'::jsonb),'optional_skill_ids',COALESCE((SELECT jsonb_agg(skill_id ORDER BY skill_id) FROM ` + table + ` WHERE ` + foreign + `=t.id AND NOT is_required),'[]'::jsonb))`
}
