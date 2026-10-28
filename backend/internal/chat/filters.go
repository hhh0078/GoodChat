// 文件作用说明：集中解释聊天记录列表和CSV导出的授权范围、日期时区与筛选参数，避免查询口径漂移。
package chat

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

const conversationWhere = ` WHERE chat_conversation_visible(NULLIF($3,'')::uuid,NULLIF($1,'')::uuid,$2,c.id)
 AND ($4='' OR c.status=$4) AND ($5='' OR c.current_agent_id=NULLIF($5,'')::uuid) AND ($6='' OR c.contact_id=NULLIF($6,'')::uuid)
 AND ($7='' OR ` + platform.ConversationChannelSQL + `=$7)
 AND ($8='' OR u.display_name ILIKE '%'||$8||'%' OR u.phone ILIKE '%'||$8||'%')
 AND ($9::timestamptz IS NULL OR c.created_at>=$9) AND ($10::timestamptz IS NULL OR c.created_at<$10)`

// conversationFilterArgs 将自然日期按租户时区转成半开区间，统一校验列表与导出的可选编号、状态和搜索词。
func conversationFilterArgs(ctx context.Context, tx pgx.Tx, r *http.Request, a actor) ([]any, string, error) {
	q := r.URL.Query()
	var timezone string
	if err := tx.QueryRow(ctx, "SELECT timezone FROM tenants WHERE id=$1", a.TenantID).Scan(&timezone); err != nil {
		return nil, "", err
	}
	from, err := filterTime(q.Get("from"), timezone, false)
	if err != nil {
		return nil, "", err
	}
	to, err := filterTime(q.Get("to"), timezone, true)
	if err != nil {
		return nil, "", err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, "", platform.Fail(400, "invalid_date_range", "开始时间必须早于结束时间")
	}
	for _, key := range []string{"agent_id", "contact_id"} {
		if value := q.Get(key); value != "" && !uuidPattern.MatchString(value) {
			return nil, "", platform.Fail(400, "invalid_filter", "筛选编号格式不正确")
		}
	}
	if status := q.Get("status"); status != "" && !inStrings(status, "queued", "assigned", "active", "closed") {
		return nil, "", platform.Fail(400, "invalid_filter", "会话状态筛选不正确")
	}
	query := strings.TrimSpace(q.Get("query"))
	if len([]rune(query)) > 200 {
		return nil, "", platform.Fail(400, "invalid_filter", "查询词最多200字")
	}
	return []any{a.ContactID, isAdmin(a), a.MemberID, q.Get("status"), q.Get("agent_id"), q.Get("contact_id"), q.Get("channel_code"), query, from, to}, timezone, nil
}
