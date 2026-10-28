// 文件作用说明：按成员接待段、真实公开回复、固化评价归属和在线租约计算独立成员统计。
package reporting

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// reportPeriod 统一新增统计的租户日历与状态校验，不使用浏览器时区推断今天。
func reportPeriod(r *http.Request, tx pgx.Tx, tenant string) (time.Time, time.Time, string, error) {
	var timezone string
	err := tx.QueryRow(r.Context(), "SELECT timezone FROM tenants WHERE id=$1 AND status='active'", tenant).Scan(&timezone)
	if err == pgx.ErrNoRows {
		return time.Time{}, time.Time{}, "", platform.Fail(403, "tenant_inactive", "租户当前不可查看统计")
	}
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, time.Time{}, "", platform.Fail(409, "invalid_tenant_timezone", "租户时区配置无效")
	}
	start, end, err := dateRange(r, zone)
	return start, end, timezone, err
}

// memberStatistics 返回本租户全部有效历史成员的指标，未发生首响或评价时保留空值而不是伪零。
func (s *Service) memberStatistics(w http.ResponseWriter, r *http.Request) {
	tenant := platform.Current(r).TenantID
	result := map[string]any{}
	err := platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		if s.featureChecker != nil {
			if err := s.featureChecker(r.Context(), tx, tenant, "statistics"); err != nil {
				return err
			}
		}
		start, end, timezone, err := reportPeriod(r, tx, tenant)
		if err != nil {
			return err
		}
		result["from"], result["to"], result["timezone"] = start, end, timezone
		var first *time.Time
		if err := tx.QueryRow(r.Context(), "SELECT min(started_at) FROM member_presence_intervals WHERE tenant_id=$1", tenant).Scan(&first); err != nil {
			return err
		}
		result["presence_started_at"] = first
		query := `WITH presence AS (
 SELECT membership_id,sum(greatest(0,extract(epoch FROM(least(online_until,$3,statement_timestamp())-greatest(started_at,$2)))))::float8 online_seconds
 FROM member_presence_intervals WHERE tenant_id=$1 AND started_at<least($3,statement_timestamp()) AND online_until>$2 GROUP BY membership_id
), episodes AS (
 SELECT a.membership_id,a.conversation_id,extract(epoch FROM(reply.first_reply-a.accepted_at))::float8 seconds
 FROM conversation_assignments a LEFT JOIN LATERAL(
 SELECT min(m.created_at) first_reply FROM messages m WHERE m.tenant_id=a.tenant_id AND m.conversation_id=a.conversation_id AND m.sender_id=a.membership_id AND m.sender_kind='agent' AND m.audience='public' AND m.withdrawn_at IS NULL AND m.created_at>=a.accepted_at AND (a.released_at IS NULL OR m.created_at<a.released_at)
 )reply ON true WHERE a.tenant_id=$1 AND a.accepted_at>=$2 AND a.accepted_at<$3
), reply_delays AS (
 SELECT m.sender_id,greatest(0,extract(epoch FROM(m.created_at-greatest(waiting.created_at,segment.accepted_at))))::float8 seconds
 FROM messages m JOIN LATERAL(
 SELECT a.accepted_at FROM conversation_assignments a WHERE a.tenant_id=m.tenant_id AND a.conversation_id=m.conversation_id AND a.membership_id=m.sender_id AND a.accepted_at<=m.created_at AND (a.released_at IS NULL OR a.released_at>m.created_at) ORDER BY a.accepted_at DESC,a.id LIMIT 1
 )segment ON true JOIN LATERAL(
 SELECT v.created_at FROM messages v WHERE v.tenant_id=m.tenant_id AND v.conversation_id=m.conversation_id AND v.sender_kind='visitor' AND v.audience='public' AND v.withdrawn_at IS NULL AND v.event_seq<m.event_seq AND v.event_seq>coalesce((SELECT max(previous.event_seq) FROM messages previous WHERE previous.tenant_id=m.tenant_id AND previous.conversation_id=m.conversation_id AND previous.sender_kind='agent' AND previous.audience='public' AND previous.withdrawn_at IS NULL AND previous.event_seq<m.event_seq),0) ORDER BY v.event_seq LIMIT 1
 )waiting ON true WHERE m.tenant_id=$1 AND m.sender_kind='agent' AND m.audience='public' AND m.withdrawn_at IS NULL AND m.created_at>=$2 AND m.created_at<$3
), reply_speed AS (
 SELECT sender_id,count(*) reply_samples,avg(seconds)::float8 avg_reply_seconds FROM reply_delays GROUP BY sender_id
), accepted AS (
 SELECT membership_id,count(DISTINCT conversation_id) received_conversations,count(*) assignments,count(seconds) first_response_samples,avg(seconds)::float8 avg_first_response_seconds FROM episodes GROUP BY membership_id
), sent AS (
 SELECT sender_id,count(*) public_messages,count(*)FILTER(WHERE withdrawn_at IS NOT NULL) withdrawn_messages FROM messages WHERE tenant_id=$1 AND sender_kind='agent' AND audience='public' AND created_at>=$2 AND created_at<$3 GROUP BY sender_id
), closed AS (
 SELECT current_agent_id,count(*) closed_conversations FROM conversations WHERE tenant_id=$1 AND status='closed' AND closed_at>=$2 AND closed_at<$3 GROUP BY current_agent_id
), active AS (
 SELECT current_agent_id,count(*) active_conversations FROM conversations WHERE tenant_id=$1 AND status IN('assigned','active') GROUP BY current_agent_id
), evaluations AS (
 SELECT final_agent_id,count(*) evaluation_count,avg(score)::float8 average_score,(100.0*count(*)FILTER(WHERE score>=4)/count(*))::float8 satisfaction_percent FROM conversation_evaluations WHERE tenant_id=$1 AND created_at>=$2 AND created_at<$3 GROUP BY final_agent_id
)
 SELECT m.id::text id,coalesce(nullif(m.display_name,''),u.display_name) display_name,m.role,m.can_serve,m.disabled,m.removed_at,` + platform.AvatarURLSQL("m") + ` avatar_url,
 NOT m.disabled AND m.removed_at IS NULL AND coalesce(live.online_until>statement_timestamp(),false) online,live.last_seen_at,
 coalesce(p.online_seconds,0) online_seconds,coalesce(r.available,false) available,coalesce(r.busy,false) busy,coalesce(r.current_load,0) current_load,m.capacity,
 coalesce(a.received_conversations,0) received_conversations,coalesce(a.assignments,0) assignments,coalesce(a.first_response_samples,0) first_response_samples,a.avg_first_response_seconds,coalesce(speed.reply_samples,0) reply_samples,speed.avg_reply_seconds,
 coalesce(sent.public_messages,0) public_messages,coalesce(sent.withdrawn_messages,0) withdrawn_messages,coalesce(closed.closed_conversations,0) closed_conversations,coalesce(active.active_conversations,0) active_conversations,
 coalesce(e.evaluation_count,0) evaluation_count,e.average_score,e.satisfaction_percent
 FROM tenant_memberships m JOIN users u ON u.id=m.user_id
 LEFT JOIN LATERAL(SELECT online_until,last_seen_at FROM member_presence_intervals live WHERE live.tenant_id=m.tenant_id AND live.membership_id=m.id ORDER BY online_until DESC LIMIT 1)live ON true
 LEFT JOIN presence p ON p.membership_id=m.id LEFT JOIN agent_runtime r ON r.tenant_id=m.tenant_id AND r.membership_id=m.id
 LEFT JOIN accepted a ON a.membership_id=m.id LEFT JOIN reply_speed speed ON speed.sender_id=m.id LEFT JOIN sent ON sent.sender_id=m.id LEFT JOIN closed ON closed.current_agent_id=m.id LEFT JOIN active ON active.current_agent_id=m.id LEFT JOIN evaluations e ON e.final_agent_id=m.id
 WHERE m.tenant_id=$1 ORDER BY online DESC,received_conversations DESC,m.created_at,m.id LIMIT 1000`
		items, err := queryMaps(r, tx, query, tenant, start, end)
		result["items"] = items
		result["definitions"] = map[string]string{"online": "真实探活租约累计，断开后最多保留45秒；启用前没有历史", "first_response": "本期接单的每段接待从确认到本人首条未撤回公开回复，不含排队、系统及内部消息；未回复不进入均值", "evaluations": "本期创建评价的最新有效评分，只计首次评价时冻结的最终接待人", "received": "本期确认接待的去重咨询数，转接两端分别计各自接待", "closed": "本期结束且最终接待人为本成员的咨询"}
		result["definitions"].(map[string]string)["reply_speed"] = "本期每条未撤回公开人工回复配对上一条公开人工回复之后最早的访客问题，同一等待段只计一次；接单前等待裁剪到本人接单时刻，无问题等待的主动消息不计"
		return err
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}
