// 文件作用说明：为客户列表提供当前身份可见的会话资料、访问时间与字段搜索，防止借搜索读取其他接待上下文。
package chat

import (
	"net/http"
	"strings"

	"supportdesk/backend/internal/platform"
)

// contactContextSQL 对管理员返回最新客户会话；客服只读取其当前有效且未释放的接待关系，成员与角色由认证参数提供。
const contactContextSQL = `coalesce((SELECT q.context FROM conversations q WHERE q.tenant_id=t.tenant_id AND q.contact_id=t.id AND ($2::boolean OR (q.current_agent_id=$1::uuid AND q.status IN('assigned','active') AND EXISTS(SELECT 1 FROM conversation_assignments ca JOIN tenant_memberships mm ON mm.id=ca.membership_id AND mm.tenant_id=ca.tenant_id WHERE ca.id=q.current_assignment_id AND ca.tenant_id=q.tenant_id AND ca.membership_id=$1::uuid AND ca.released_at IS NULL AND (ca.expires_at IS NULL OR ca.expires_at>statement_timestamp()) AND mm.can_serve AND NOT mm.disabled AND mm.removed_at IS NULL))) ORDER BY q.created_at DESC,q.id DESC LIMIT 1),'{}'::jsonb)`

// contactListQuery 只对固定白名单字段搜索，URL参数不能改变租户或授权条件，访问统计不替代已有客户权限复核。
func contactListQuery(r *http.Request, a actor) (string, []any, error) {
	projection := `||jsonb_build_object('last_context',` + contactContextSQL + `,'last_visit_at',(SELECT max(v.created_at) FROM contact_visits v WHERE v.tenant_id=t.tenant_id AND v.contact_id=t.id),'visit_count',(SELECT count(*) FROM contact_visits v WHERE v.tenant_id=t.tenant_id AND v.contact_id=t.id))`
	query := strings.Replace(resourceQuery("contacts"), " FROM contacts t WHERE ", projection+" FROM contacts t WHERE ", 1)
	args := []any{a.MemberID, isAdmin(a)}
	if !isAdmin(a) {
		query += " AND " + currentContactAssignment
	}
	field := r.URL.Query().Get("search_field")
	if field == "" {
		field = "all"
	}
	predicates := map[string]string{
		"name":     `strpos(lower(t.display_name||' '||t.remark_name),lower($3))>0`,
		"phone":    `strpos(lower(t.phone),lower($3))>0`,
		"channel":  `strpos(lower(coalesce(t.last_source->>'channel_code','')||' '||coalesce(t.last_source->'channel_tags','[]'::jsonb)::text),lower($3))>0`,
		"extra":    `strpos(lower((t.attributes||` + contactContextSQL + `)::text),lower($3))>0`,
		"tag":      `EXISTS(SELECT 1 FROM contact_tags ct JOIN tags g ON g.id=ct.tag_id AND g.tenant_id=ct.tenant_id WHERE ct.tenant_id=t.tenant_id AND ct.contact_id=t.id AND strpos(lower(g.name),lower($3))>0)`,
		"category": `EXISTS(SELECT 1 FROM contact_categories cc WHERE cc.id=t.category_id AND cc.tenant_id=t.tenant_id AND strpos(lower(cc.name),lower($3))>0)`,
	}
	if field != "all" && predicates[field] == "" {
		return "", nil, platform.Fail(400, "invalid_contact_search", "不支持此客户搜索类型")
	}
	value := strings.TrimSpace(r.URL.Query().Get("query"))
	if len([]rune(value)) > 200 {
		return "", nil, platform.Fail(400, "contact_search_too_long", "客户搜索内容不能超过200字")
	}
	if value != "" {
		predicate := predicates[field]
		if field == "all" {
			parts := []string{}
			for _, key := range []string{"name", "phone", "channel", "extra", "tag", "category"} {
				parts = append(parts, predicates[key])
			}
			predicate = strings.Join(parts, " OR ")
		}
		query += " AND (" + predicate + ")"
		args = append(args, value)
	}
	return query, args, nil
}
