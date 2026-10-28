// 文件作用说明：按租户日历汇总每个实际访问入口的访客与访问次数，咨询归于创建入口并保留当前状态计数。
package reporting

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// linkStatistics 批量返回各入口的统计，重试visit_id不增加访问次数，跨入口复访归属实际链接。
func (s *Service) linkStatistics(w http.ResponseWriter, r *http.Request) {
	tenant := platform.Current(r).TenantID
	result := map[string]any{}
	err := platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		start, end, timezone, err := reportPeriod(r, tx, tenant)
		if err != nil {
			return err
		}
		items, err := queryMaps(r, tx, `WITH visits AS(
 SELECT access_link_id,count(*) visits,count(DISTINCT contact_id) visitors FROM contact_visits WHERE tenant_id=$1 AND created_at>=$2 AND created_at<$3 GROUP BY access_link_id
), conversations AS(
 SELECT created_entry_id,count(*)FILTER(WHERE created_at>=$2 AND created_at<$3) new_conversations,count(*)FILTER(WHERE status='queued') queued,count(*)FILTER(WHERE status IN('active','assigned')) active,count(*)FILTER(WHERE closed_at>=$2 AND closed_at<$3) closed FROM conversations WHERE tenant_id=$1 GROUP BY created_entry_id
)
 SELECT l.id::text id,coalesce(v.visits,0) visits,coalesce(v.visitors,0) visitors,coalesce(c.new_conversations,0) new_conversations,coalesce(c.queued,0) queued,coalesce(c.active,0) active,coalesce(c.closed,0) closed
 FROM access_links l LEFT JOIN visits v ON v.access_link_id=l.id LEFT JOIN conversations c ON c.created_entry_id=l.id WHERE l.tenant_id=$1 ORDER BY l.created_at DESC,l.id LIMIT 500`, tenant, start, end)
		result["items"], result["from"], result["to"], result["timezone"] = items, start, end, timezone
		return err
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}
