// 文件作用说明：实现客服组、分配池、链接和客户分类资料的租户管理接口。
package chat

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// resourceSpec 限制管理资源可写字段，禁止客户端覆盖租户和系统身份。
type resourceSpec struct {
	table  string
	fields map[string]bool
}

// fieldSet 创建资源字段白名单。
func fieldSet(names string) map[string]bool {
	m := map[string]bool{}
	for _, v := range strings.Fields(names) {
		m[v] = true
	}
	return m
}

var resourceSpecs = map[string]resourceSpec{
	"routing-skills": {"routing_skills", fieldSet("name archived")},
	"groups":         {"agent_groups", fieldSet("name archived member_ids")},
	"routing-pools":  {"routing_pools", fieldSet("name service_group_id algorithm assignment_mode members_mode agent_mode archived member_ids member_weights required_skill_ids optional_skill_ids history_priority")},
	"access-links":   {"access_links", fieldSet("name service_space_id service_group_id routing_pool_id agent_id agent_mode status channel_code channel_tags reception_mode allow_routing_override expires_at binding_mode page_id pinned_release_id required_skill_ids optional_skill_ids")},
	"contacts":       {"contacts", fieldSet("display_name remark_name phone avatar_url category_id attributes notes archived tag_ids locked_fields")},
	"categories":     {"contact_categories", fieldSet("name archived")},
	"tag-categories": {"tag_categories", fieldSet("name archived")},
	"tags":           {"tags", fieldSet("name category_id color archived")},
	"custom-fields":  {"contact_field_definitions", fieldSet("name field_key field_type options archived")},
}

// resources 创建受字段白名单约束的持久化资源处理器。
func (s *Service) resources(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := memberActor(r)
		spec := resourceSpecs[name]
		ctx := r.Context()
		var result any
		err := platform.TenantTx(ctx, s.pool, a.TenantID, func(tx pgx.Tx) error {
			if err := tenantActive(ctx, tx, a.TenantID); err != nil {
				return err
			}
			if name == "contacts" {
				if err := liveMember(ctx, tx, a); err != nil {
					return err
				}
			}
			if r.Method == http.MethodPost && name == "access-links" {
				if err := s.checkFeature(ctx, tx, a.TenantID, "multi_link"); err != nil {
					return err
				}
			}
			if (r.Method == http.MethodPost || r.Method == http.MethodPatch) && (name == "tags" || name == "tag-categories") {
				if err := s.checkFeature(ctx, tx, a.TenantID, "labels"); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, "SELECT ensure_chat_defaults($1::uuid)", a.TenantID); err != nil {
				return err
			}
			if r.Method == http.MethodGet {
				query := resourceQuery(name)
				args := []any{}
				if name == "contacts" && !isAdmin(a) {
					query += " AND " + currentContactAssignment
					args = append(args, a.MemberID)
				}
				query += " ORDER BY t.created_at DESC,t.id LIMIT 500"
				rows, err := tx.Query(ctx, query, args...)
				if err != nil {
					return err
				}
				items, err := jsonRows(rows)
				result = map[string]any{"items": items}
				return err
			}
			id := r.PathValue("id")
			if id != "" && !uuidPattern.MatchString(id) {
				return platform.Fail(400, "invalid_id", "资源编号格式不正确")
			}
			if r.Method == http.MethodDelete {
				if _, err := prepareRoutingExtras(ctx, tx, name, id, map[string]any{}); err != nil {
					return err
				}
				if name == "access-links" {
					_, err := tx.Exec(ctx, "UPDATE access_links SET status='disabled' WHERE id=$1", id)
					if err != nil {
						return err
					}
				} else {
					tag, err := tx.Exec(ctx, "UPDATE "+spec.table+" SET archived=true WHERE id=$1", id)
					if err != nil {
						return err
					}
					if tag.RowsAffected() == 0 {
						return pgx.ErrNoRows
					}
				}
				result = map[string]any{"id": id, "archived": true}
				return nil
			}
			values := map[string]any{}
			if err := platform.Decode(w, r, &values); err != nil {
				return err
			}
			for k := range values {
				if !spec.fields[k] {
					return platform.Fail(400, "unsupported_field", "当前资源不接受字段："+k)
				}
			}
			if _, hasTags := values["tag_ids"]; name == "contacts" && hasTags {
				if err := s.checkFeature(ctx, tx, a.TenantID, "labels"); err != nil {
					return err
				}
			}
			for k, value := range values {
				if strings.HasSuffix(k, "_id") && value != nil {
					v, ok := value.(string)
					if !ok || (v != "" && !uuidPattern.MatchString(v)) {
						return platform.Fail(400, "invalid_id", "关联资源编号格式不正确："+k)
					}
				}
			}
			if err := prepareResource(ctx, tx, name, values, r.Method == http.MethodPost); err != nil {
				return err
			}
			extra, err := prepareRoutingExtras(ctx, tx, name, id, values)
			if err != nil {
				return err
			}
			memberIDs, hasMembers := values["member_ids"]
			delete(values, "member_ids")
			tagIDs, hasTags := values["tag_ids"]
			delete(values, "tag_ids")
			if _, explicitMode := values["members_mode"]; hasMembers && name == "routing-pools" && !explicitMode {
				values["members_mode"] = "fixed"
			}
			keys := make([]string, 0, len(values))
			for k := range values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			args := []any{}
			parts := []string{}
			for i, k := range keys {
				v := values[k]
				if text, ok := v.(string); ok && text == "" && strings.HasSuffix(k, "_id") {
					v = nil
				}
				if k == "attributes" || k == "options" || k == "locked_fields" || k == "channel_tags" {
					v = jsonValue(v)
				}
				args = append(args, v)
				parts = append(parts, fmt.Sprintf("%s=$%d", k, i+1))
			}
			if r.Method == http.MethodPost {
				id = newID()
				columns := append([]string{"id", "tenant_id"}, keys...)
				args = append([]any{id, a.TenantID}, args...)
				marks := []string{}
				for i := range args {
					marks = append(marks, fmt.Sprintf("$%d", i+1))
				}
				_, err := tx.Exec(ctx, "INSERT INTO "+spec.table+" ("+strings.Join(columns, ",")+") VALUES("+strings.Join(marks, ",")+")", args...)
				if err != nil {
					return err
				}
			} else if len(parts) > 0 {
				args = append(args, id)
				tag, err := tx.Exec(ctx, "UPDATE "+spec.table+" SET "+strings.Join(parts, ",")+fmt.Sprintf(" WHERE id=$%d", len(args)), args...)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 0 {
					return pgx.ErrNoRows
				}
			}
			if hasMembers {
				if err := setResourceMembers(ctx, tx, name, id, memberIDs); err != nil {
					return err
				}
			}
			if err := saveRoutingExtras(ctx, tx, name, id, extra); err != nil {
				return err
			}
			if hasTags {
				if err := setContactTags(ctx, tx, id, tagIDs); err != nil {
					return err
				}
			}
			if err := validateResource(ctx, tx, name, id); err != nil {
				return err
			}
			query := resourceQuery(name) + " AND t.id=$1"
			item, err := txJSON(ctx, tx, query, id)
			result = item
			return err
		})
		if err != nil {
			replyError(w, r, err)
			return
		}
		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}
		platform.JSON(w, status, result)
	}
}

// resourceQuery 生成只包含静态表名的资源JSON投影。
func resourceQuery(name string) string {
	spec := resourceSpecs[name]
	projection := "to_jsonb(t)"
	switch name {
	case "groups":
		projection += "||jsonb_build_object('member_ids',COALESCE((SELECT jsonb_agg(m.membership_id) FROM agent_group_members m WHERE m.group_id=t.id),'[]'::jsonb))"
	case "routing-pools":
		projection += routingSkillsProjection("routing_pool_skills", "routing_pool_id") + `||jsonb_build_object('member_weights',COALESCE((SELECT jsonb_object_agg(membership_id::text,weight) FROM routing_pool_members WHERE routing_pool_id=t.id),'{}'::jsonb))`
		projection += "||jsonb_build_object('member_ids',COALESCE((SELECT jsonb_agg(m.membership_id) FROM routing_pool_members m WHERE m.routing_pool_id=t.id),'[]'::jsonb))"
	case "access-links":
		projection += routingSkillsProjection("access_link_skills", "access_link_id")
	case "contacts":
		projection += "||jsonb_build_object('tag_ids',COALESCE((SELECT jsonb_agg(m.tag_id) FROM contact_tags m WHERE m.contact_id=t.id),'[]'::jsonb),'tags',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',g.id,'name',g.name,'color',g.color)) FROM contact_tags m JOIN tags g ON g.id=m.tag_id AND g.tenant_id=m.tenant_id WHERE m.contact_id=t.id),'[]'::jsonb),'blocked',EXISTS(SELECT 1 FROM block_rules b WHERE b.contact_id=t.id AND b.revoked_at IS NULL AND (b.expires_at IS NULL OR b.expires_at>now())))"
	}
	where := "NOT t.archived"
	if name == "access-links" {
		where = "true"
	}
	return "SELECT " + projection + " FROM " + spec.table + " t WHERE " + where
}

// prepareResource 补齐创建默认关联并校验可识别的产品枚举。
func prepareResource(ctx context.Context, tx pgx.Tx, name string, v map[string]any, create bool) error {
	if create && name != "contacts" {
		n, _ := v["name"].(string)
		if strings.TrimSpace(n) == "" || len([]rune(n)) > 80 {
			return platform.Fail(400, "invalid_name", "请填写不超过80字的名称")
		}
	}
	if name == "access-links" {
		if mode, ok := v["reception_mode"]; ok && !inStrings(fmt.Sprint(mode), "inherit", "auto", "manual") {
			return platform.Fail(400, "invalid_reception_mode", "接待方式只能为自动分配或客服领取")
		}
		// 资料参数始终接收；动态接待目标仍由resolveRouting执行租户范围与组池交集校验。
		v["allow_routing_override"] = true
		if raw, ok := v["channel_tags"]; ok {
			items, valid := raw.([]any)
			if !valid || len(items) > 20 {
				return platform.Fail(400, "invalid_channel_tags", "渠道标识最多20个标签")
			}
			tags, seen := []string{}, map[string]bool{}
			for _, item := range items {
				value, valid := item.(string)
				value = strings.TrimSpace(value)
				if !valid || value == "" || len([]rune(value)) > 100 {
					return platform.Fail(400, "invalid_channel_tags", "每个渠道标识须为1至100字")
				}
				if !seen[value] {
					tags, seen[value] = append(tags, value), true
				}
			}
			v["channel_tags"] = tags
		}
		if raw, ok := v["channel_code"]; ok {
			value, valid := raw.(string)
			if !valid || len([]rune(value)) > 100 {
				return platform.Fail(400, "invalid_channel", "主渠道标识最多100字")
			}
			v["channel_code"] = strings.TrimSpace(value)
		}
	}
	if name == "routing-pools" {
		if mode, ok := v["members_mode"]; ok && !inStrings(fmt.Sprint(mode), "group", "fixed") {
			return platform.Fail(400, "invalid_members_mode", "候选来源只能是动态业务组或固定分配池")
		}
		if value, ok := v["history_priority"]; ok {
			if _, valid := value.(bool); !valid {
				return platform.Fail(400, "invalid_history_priority", "历史客服优先必须为布尔值")
			}
		}
		if algo, ok := v["algorithm"]; ok && !inStrings(fmt.Sprint(algo), "least_load", "round_robin", "manual", "least_ratio", "random", "weighted_round_robin") {
			return platform.Fail(422, "algorithm_unavailable", "该分配策略尚未开放，请使用已开放的最少会话、轮询、加权轮询、负载比例、随机或人工领取")
		}
	}
	if create && (name == "routing-pools" || name == "access-links") {
		for _, field := range []string{"service_group_id", "service_space_id", "routing_pool_id"} {
			if name == "routing-pools" && field != "service_group_id" {
				continue
			}
			if val, ok := v[field].(string); ok && val != "" {
				continue
			}
			table := map[string]string{"service_group_id": "agent_groups", "service_space_id": "service_spaces", "routing_pool_id": "routing_pools"}[field]
			var id string
			if err := tx.QueryRow(ctx, "SELECT id::text FROM "+table+" WHERE is_default LIMIT 1").Scan(&id); err != nil {
				return err
			}
			v[field] = id
		}
	}
	if create && name == "access-links" {
		v["entry_key"] = strings.ReplaceAll(newID(), "-", "")
	}
	if create && name == "tags" {
		if _, ok := v["category_id"]; !ok {
			var id string
			if err := tx.QueryRow(ctx, "SELECT id::text FROM tag_categories WHERE NOT archived ORDER BY created_at LIMIT 1").Scan(&id); err != nil {
				return err
			}
			v["category_id"] = id
		}
	}
	if x, ok := v["attributes"]; ok {
		m, yes := x.(map[string]any)
		if !yes {
			return platform.Fail(400, "invalid_attributes", "客户字段必须为对象")
		}
		if err := validateAttributes(ctx, tx, m); err != nil {
			return err
		}
	}
	return nil
}

// inStrings 校验枚举是否在受支持集合中。
func inStrings(v string, choices ...string) bool {
	for _, s := range choices {
		if v == s {
			return true
		}
	}
	return false
}

// setResourceMembers 原子替换分组或固定分配池成员，复合外键阻止跨租户。
func setResourceMembers(ctx context.Context, tx pgx.Tx, name, id string, value any) error {
	ids, err := stringIDs(value)
	if err != nil {
		return err
	}
	table, foreign := "agent_group_members", "group_id"
	if name == "routing-pools" {
		table, foreign = "routing_pool_members", "routing_pool_id"
	}
	if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE "+foreign+"=$1", id); err != nil {
		return err
	}
	for _, member := range ids {
		_, err := tx.Exec(ctx, "INSERT INTO "+table+"(tenant_id,"+foreign+",membership_id) VALUES(nullif(current_setting('app.tenant_id',true),'')::uuid,$1,$2) ON CONFLICT DO NOTHING", id, member)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO agent_runtime(tenant_id,membership_id) SELECT tenant_id,id FROM tenant_memberships WHERE id=$1 AND tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid ON CONFLICT DO NOTHING", member); err != nil {
			return err
		}
	}
	return nil
}

// stringIDs 验证去重后的UUID数组。
func stringIDs(value any) ([]string, error) {
	values, ok := value.([]any)
	if !ok {
		return nil, platform.Fail(400, "invalid_members", "请选择正确的编号数组")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		id, ok := v.(string)
		if !ok || !uuidPattern.MatchString(id) {
			return nil, platform.Fail(400, "invalid_members", "数组包含错误编号")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids, nil
}

// setContactTags 原子替换客户标签，重复关联由主键去重。
func setContactTags(ctx context.Context, tx pgx.Tx, id string, value any) error {
	ids, err := stringIDs(value)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM contact_tags WHERE contact_id=$1", id); err != nil {
		return err
	}
	for _, tag := range ids {
		if _, err := tx.Exec(ctx, "INSERT INTO contact_tags(tenant_id,contact_id,tag_id) VALUES(nullif(current_setting('app.tenant_id',true),'')::uuid,$1,$2) ON CONFLICT DO NOTHING", id, tag); err != nil {
			return err
		}
	}
	return nil
}
