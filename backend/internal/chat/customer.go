// 文件作用说明：校验接待范围、自定义客户字段、访问轨迹与保留历史的封禁操作。
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// currentContactAssignment 只认可当前未释放且未过期的接待关系，旧会话不能授予客户后续资料与访问上下文的读取权。
const currentContactAssignment = `EXISTS(SELECT 1 FROM conversations c
 JOIN conversation_assignments ca ON ca.id=c.current_assignment_id AND ca.tenant_id=c.tenant_id
 JOIN tenant_memberships mm ON mm.id=ca.membership_id AND mm.tenant_id=ca.tenant_id
 WHERE c.contact_id=t.id AND c.tenant_id=t.tenant_id AND c.current_agent_id=$1::uuid AND ca.membership_id=$1::uuid
 AND c.status IN('assigned','active') AND ca.released_at IS NULL AND (ca.expires_at IS NULL OR ca.expires_at>statement_timestamp())
 AND mm.can_serve AND NOT mm.disabled AND mm.removed_at IS NULL)`

// validateResource 阻止不兼容的组池、越权候选和跨租户目标组合落库。
func validateResource(ctx context.Context, tx pgx.Tx, name, id string) error {
	var ok bool
	switch name {
	case "routing-pools":
		err := tx.QueryRow(ctx, "SELECT NOT p.archived AND NOT g.archived AND NOT EXISTS(SELECT 1 FROM routing_pool_members pm WHERE pm.routing_pool_id=p.id AND NOT EXISTS(SELECT 1 FROM agent_group_members gm WHERE gm.group_id=p.service_group_id AND gm.membership_id=pm.membership_id)) FROM routing_pools p JOIN agent_groups g ON g.id=p.service_group_id AND g.tenant_id=p.tenant_id WHERE p.id=$1", id).Scan(&ok)
		if err != nil {
			return err
		}
	case "access-links":
		err := tx.QueryRow(ctx, "SELECT NOT p.archived AND NOT g.archived AND p.service_group_id=l.service_group_id AND (l.agent_id IS NULL OR (EXISTS(SELECT 1 FROM agent_group_members gm WHERE gm.group_id=l.service_group_id AND gm.membership_id=l.agent_id) AND (p.members_mode='group' OR EXISTS(SELECT 1 FROM routing_pool_members pm WHERE pm.routing_pool_id=p.id AND pm.membership_id=l.agent_id)))) FROM access_links l JOIN routing_pools p ON p.id=l.routing_pool_id AND p.tenant_id=l.tenant_id JOIN agent_groups g ON g.id=l.service_group_id AND g.tenant_id=l.tenant_id WHERE l.id=$1", id).Scan(&ok)
		if err != nil {
			return err
		}
	default:
		return nil
	}
	if !ok {
		return platform.Fail(400, "routing_conflict", "客服组、分配池与指定客服范围不兼容")
	}
	return nil
}

// validateAttributes 按租户已登记的字段类型校验资料，不把任意上下文写入客户系统字段。
func validateAttributes(ctx context.Context, tx pgx.Tx, attributes map[string]any) error {
	if len(attributes) > 100 {
		return platform.Fail(400, "attributes_too_large", "客户字段过多")
	}
	for key, value := range attributes {
		var kind string
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT field_type,options FROM contact_field_definitions WHERE field_key=$1 AND NOT archived", key).Scan(&kind, &raw); err != nil {
			if err == pgx.ErrNoRows {
				return platform.Fail(400, "unknown_attribute", "尚未登记客户字段："+key)
			}
			return err
		}
		if value == nil {
			continue
		}
		valid := true
		switch kind {
		case "text":
			v, ok := value.(string)
			valid = ok && len([]rune(v)) <= 2000
		case "number":
			_, valid = value.(float64)
		case "boolean":
			_, valid = value.(bool)
		case "date":
			v, ok := value.(string)
			_, err := time.Parse("2006-01-02", v)
			valid = ok && err == nil
		case "select", "multiselect":
			var options []string
			_ = json.Unmarshal(raw, &options)
			if kind == "select" {
				v, ok := value.(string)
				valid = ok && inStrings(v, options...)
			} else {
				items, ok := value.([]any)
				valid = ok
				for _, item := range items {
					v, ok := item.(string)
					valid = valid && ok && inStrings(v, options...)
				}
			}
		}
		if !valid {
			return platform.Fail(400, "invalid_attribute", "客户字段类型或选项不正确："+key)
		}
	}
	return nil
}

// contactDetail 为接待资料栏读取单个客户，复用当前接待关系而不扩大客服访问范围。
func (s *Service) contactDetail(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	id := r.PathValue("id")
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := canSeeContact(r.Context(), tx, a, id); err != nil {
			return err
		}
		var err error
		result, err = txJSON(r.Context(), tx, resourceQuery("contacts")+" AND t.id=$1", id)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// contactVisits 查询授权客户的去重访问轨迹。
func (s *Service) contactVisits(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	id := r.PathValue("id")
	var items []map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := canSeeContact(r.Context(), tx, a, id); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(v) FROM contact_visits v WHERE contact_id=$1 ORDER BY created_at DESC,id LIMIT 200", id)
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

// canSeeContact 确保普通客服只能读取本人授权接待客户。
func canSeeContact(ctx context.Context, tx pgx.Tx, a actor, id string) error {
	if !uuidPattern.MatchString(id) {
		return platform.Fail(400, "invalid_id", "客户编号不正确")
	}
	if err := liveMember(ctx, tx, a); err != nil {
		return err
	}
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM contacts t WHERE t.id=$2 AND ($3 OR "+currentContactAssignment+"))", a.MemberID, id, isAdmin(a)).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return platform.Fail(404, "not_found", "客户不存在或无权访问")
	}
	return nil
}

// blockContact 建立或解除持久封禁，关闭当前会话并保留全部历史。
func (s *Service) blockContact(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	contact := r.PathValue("id")
	var input struct {
		Reason    string     `json:"reason"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if r.Method == http.MethodPost {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
	}
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := canSeeContact(r.Context(), tx, a, contact); err != nil {
			return err
		}
		if r.Method == http.MethodDelete {
			_, err := tx.Exec(r.Context(), "UPDATE block_rules SET revoked_at=now() WHERE contact_id=$1 AND revoked_at IS NULL", contact)
			return err
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO block_rules(tenant_id,contact_id,reason,expires_at,created_by) VALUES($1,$2,$3,$4,$5)", a.TenantID, contact, strings.TrimSpace(input.Reason), input.ExpiresAt, a.MemberID); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), "SELECT id::text,routing_pool_id::text FROM conversations WHERE contact_id=$1 AND status!='closed' ORDER BY routing_pool_id,id", contact)
		if err != nil {
			return err
		}
		type pair struct{ id, pool string }
		pairs := []pair{}
		for rows.Next() {
			var v pair
			if err := rows.Scan(&v.id, &v.pool); err != nil {
				rows.Close()
				return err
			}
			pairs = append(pairs, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, v := range pairs {
			if _, err := tx.Exec(r.Context(), "SELECT id FROM routing_pools WHERE id=$1 FOR UPDATE", v.pool); err != nil {
				return err
			}
			if _, err := tx.Exec(r.Context(), "SELECT id FROM conversations WHERE id=$1 FOR UPDATE", v.id); err != nil {
				return err
			}
			if err := s.closeLocked(r.Context(), tx, a, v.id, "blocked"); err != nil {
				return err
			}
		}
		_, err = tx.Exec(r.Context(), "UPDATE visitor_sessions SET revoked_at=now() WHERE contact_id=$1", contact)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"contact_id": contact, "blocked": r.Method == http.MethodPost})
}

// agentStatus 更新上班与示忙状态，心跳有效期独立于会话生命周期。
func (s *Service) agentStatus(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	if !a.CanServe {
		replyError(w, r, platform.Fail(403, "cannot_serve", "当前成员没有接待权限"))
		return
	}
	var in struct {
		Available *bool `json:"available"`
		Busy      *bool `json:"busy"`
	}
	if r.Method == http.MethodPatch {
		if err := platform.Decode(w, r, &in); err != nil {
			replyError(w, r, err)
			return
		}
	}
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), "INSERT INTO agent_runtime(tenant_id,membership_id) VALUES($1,$2) ON CONFLICT DO NOTHING", a.TenantID, a.MemberID)
		if err != nil {
			return err
		}
		if r.Method == http.MethodPatch {
			_, err = tx.Exec(r.Context(), "UPDATE agent_runtime SET available=COALESCE($2,available),busy=COALESCE($3,busy),online_until=now()+interval '45 seconds',updated_at=now() WHERE membership_id=$1", a.MemberID, in.Available, in.Busy)
			if err != nil {
				return err
			}
		}
		result, err = txJSON(r.Context(), tx, "SELECT to_jsonb(r)||jsonb_build_object('capacity',m.capacity) FROM agent_runtime r JOIN tenant_memberships m ON m.id=r.membership_id AND m.tenant_id=r.tenant_id WHERE r.membership_id=$1", a.MemberID)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// colleagues 返回接待范围内的安全客服选项，不泄露账号邮箱和管理角色。
func (s *Service) colleagues(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var items []map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		conversation := r.URL.Query().Get("conversation_id")
		if conversation != "" {
			if err := canReadConversation(ctx, tx, a, conversation); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, "SELECT jsonb_build_object('id',m.id,'display_name',COALESCE(NULLIF(m.display_name,''),u.display_name),'available',COALESCE(rt.available,false),'busy',COALESCE(rt.busy,false),'online',COALESCE(rt.online_until>now(),false),'current_load',COALESCE(rt.current_load,0),'capacity',m.capacity) FROM tenant_memberships m JOIN users u ON u.id=m.user_id LEFT JOIN agent_runtime rt ON rt.membership_id=m.id AND rt.tenant_id=m.tenant_id WHERE m.tenant_id=$1 AND m.can_serve AND NOT m.disabled AND m.removed_at IS NULL AND (($2='' AND ($3 OR EXISTS(SELECT 1 FROM agent_group_members g1 JOIN agent_group_members g2 ON g1.group_id=g2.group_id AND g1.tenant_id=g2.tenant_id WHERE g1.membership_id=$4 AND g2.membership_id=m.id))) OR ($2!='' AND EXISTS(SELECT 1 FROM conversations c JOIN routing_pools p ON p.id=c.routing_pool_id AND p.tenant_id=c.tenant_id JOIN agent_group_members gm ON gm.group_id=c.service_group_id AND gm.tenant_id=c.tenant_id WHERE c.id=NULLIF($2,'')::uuid AND gm.membership_id=m.id AND (p.members_mode='group' OR EXISTS(SELECT 1 FROM routing_pool_members pm WHERE pm.routing_pool_id=p.id AND pm.membership_id=m.id))))) ORDER BY u.display_name,m.id", a.TenantID, conversation, isAdmin(a), a.MemberID)
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

// validateContext 约束JSON深度、键数和字符串长度，允许0与false。
func validateContext(value map[string]any) error {
	if len(jsonValue(value)) > 16*1024 {
		return platform.Fail(400, "context_too_large", "业务上下文不能超过16KiB")
	}
	keys := 0
	if err := walkContext(value, 0, &keys); err != nil {
		return err
	}
	if keys > 100 {
		return platform.Fail(400, "context_too_large", "业务上下文最多100个字段")
	}
	return nil
}

// walkContext 递归检查不可信上下文中的复合值。
func walkContext(value any, depth int, keys *int) error {
	if depth > 5 {
		return platform.Fail(400, "context_too_deep", "业务上下文最多嵌套5层")
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			*keys++
			if len([]rune(key)) > 64 {
				return platform.Fail(400, "invalid_context_key", "业务字段名称过长")
			}
			if err := walkContext(item, depth+1, keys); err != nil {
				return err
			}
		}
	case []any:
		if len(v) > 100 {
			return platform.Fail(400, "context_too_large", "业务数组最多100项")
		}
		for _, item := range v {
			if err := walkContext(item, depth+1, keys); err != nil {
				return err
			}
		}
	case string:
		if len([]rune(v)) > 2000 {
			return platform.Fail(400, "context_value_too_long", "单个业务字段最多2000字")
		}
	case nil, bool, float64:
	default:
		return platform.Fail(400, "invalid_context", fmt.Sprintf("不支持的业务字段类型：%T", value))
	}
	return nil
}
