// 文件作用说明：为管理员提供团队日常指标，为客服提供仅属于本人的接待与未读指标，避免列表上限影响计数。
package chat

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// dashboard 使用租户日历和真实接待段统计今天，普通成员不返回团队汇总。
func (s *Service) dashboard(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var result map[string]any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		var timezone string
		if err := tx.QueryRow(ctx, "SELECT timezone FROM tenants WHERE id=$1", a.TenantID).Scan(&timezone); err != nil {
			return err
		}
		zone, err := time.LoadLocation(timezone)
		if err != nil {
			return platform.Fail(409, "invalid_tenant_timezone", "租户时区配置无效")
		}
		now := time.Now().In(zone)
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
		to := from.AddDate(0, 0, 1)
		query := `SELECT jsonb_build_object(
		 'assigned',count(*) FILTER(WHERE c.status='assigned'),
		 'active',count(*) FILTER(WHERE c.status='active'),
		 'today_closed',count(*) FILTER(WHERE c.closed_at>=$2 AND c.closed_at<$3),
		 'unread',count(*) FILTER(WHERE c.status IN('assigned','active') AND EXISTS(SELECT 1 FROM messages m WHERE m.conversation_id=c.id AND m.audience='public' AND m.sender_kind='visitor' AND m.withdrawn_at IS NULL AND m.event_seq>coalesce((SELECT rc.last_read_seq FROM read_cursors rc WHERE rc.conversation_id=c.id AND rc.participant_kind='agent' AND rc.participant_id=$1::uuid),0) AND chat_message_visible($1::uuid,NULL,$4,m.id))))
		 FROM conversations c WHERE ($4 OR c.current_agent_id=$1::uuid)`
		result, err = txJSON(ctx, tx, query, a.MemberID, from, to, isAdmin(a))
		if err != nil {
			return err
		}
		received, err := txJSON(ctx, tx, `SELECT jsonb_build_object('today_received',count(DISTINCT conversation_id)) FROM conversation_assignments WHERE membership_id=$1 AND accepted_at>=$2 AND accepted_at<$3`, a.MemberID, from, to)
		if err != nil {
			return err
		}
		result["today_received"] = received["today_received"]
		if isAdmin(a) {
			team, err := txJSON(ctx, tx, `SELECT jsonb_build_object('queued',count(*) FILTER(WHERE status='queued'),'today_created',count(*) FILTER(WHERE created_at>=$1 AND created_at<$2)) FROM conversations`, from, to)
			if err != nil {
				return err
			}
			result["queued"], result["today_created"] = team["queued"], team["today_created"]
			var online int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM tenant_memberships m WHERE m.can_serve AND NOT m.disabled AND m.removed_at IS NULL AND EXISTS(SELECT 1 FROM member_presence_intervals p WHERE p.membership_id=m.id AND p.online_until>statement_timestamp())`).Scan(&online); err != nil {
				return err
			}
			result["online_agents"] = online
		}
		result["timezone"], result["observed_at"] = timezone, now
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}
