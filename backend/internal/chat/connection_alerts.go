// 文件作用说明：按真实WS探活租约识别上班却断线的客服，管理员看本租户，普通成员仅看自身。
package chat

import (
	"net/http"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// connectionAlerts 读取真实探活记录，HTTP状态轮询或上下班操作不能替代WS在线证明。
// 多连接共同延长成员租约，任一标签仍在线时不会因其他标签断开而误报。
func (s *Service) connectionAlerts(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var items []map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		if err := liveMember(r.Context(), tx, a); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), `SELECT jsonb_build_object(
		 'id',m.id,'display_name',COALESCE(NULLIF(m.display_name,''),u.display_name),
		 'busy',rt.busy,'last_seen_at',p.last_seen_at,'offline_since',p.online_until)
		 FROM tenant_memberships m
		 JOIN users u ON u.id=m.user_id
		 JOIN agent_runtime rt ON rt.tenant_id=m.tenant_id AND rt.membership_id=m.id
		 LEFT JOIN LATERAL(SELECT last_seen_at,online_until FROM member_presence_intervals p
		  WHERE p.tenant_id=m.tenant_id AND p.membership_id=m.id ORDER BY online_until DESC LIMIT 1)p ON true
		 WHERE m.tenant_id=$1 AND m.can_serve AND NOT m.disabled AND m.removed_at IS NULL
		 AND rt.available AND NOT coalesce(p.online_until>statement_timestamp(),false)
		 AND ($2 OR m.id=$3)
		 ORDER BY p.online_until NULLS FIRST,m.id`, a.TenantID, isAdmin(a), a.MemberID)
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
	scope := "self"
	if isAdmin(a) {
		scope = "team"
	}
	platform.JSON(w, http.StatusOK, map[string]any{
		"items": items, "scope": scope, "probe_timeout_seconds": 45, "observed_at": nowUTC(),
	})
}
