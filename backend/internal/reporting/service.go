// Package reporting 提供租户独立统计与平台概览，按明确时间范围聚合真实数据。
package reporting

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// Service 保存报表依赖，报表查询只在授权租户范围内执行。
type Service struct {
	pool           *pgxpool.Pool
	auth           platform.Authorizer
	featureChecker func(context.Context, pgx.Tx, string, string) error
}

// New 建立统计服务。
func New(pool *pgxpool.Pool, auth platform.Authorizer) *Service {
	return &Service{pool: pool, auth: auth}
}

// SetFeatureChecker 注入套餐统计权益校验。
func (s *Service) SetFeatureChecker(checker func(context.Context, pgx.Tx, string, string) error) {
	s.featureChecker = checker
}

// Register 注册租户管理员报表及平台管理概览。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenant/statistics", s.auth.RequireAdmin(s.statistics))
	mux.HandleFunc("GET /api/v1/platform/overview", s.auth.RequirePlatform(s.overview))
}

// statistics 按创建咨询和发送消息分别统计，系统自动消息不计人工首响。
func (s *Service) statistics(w http.ResponseWriter, r *http.Request) {
	tenant := platform.Current(r).TenantID
	result := map[string]any{"by_agent": []map[string]any{}, "by_channel": []map[string]any{}, "daily": []map[string]any{}}
	err := platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		if s.featureChecker != nil {
			if err := s.featureChecker(r.Context(), tx, tenant, "statistics"); err != nil {
				return err
			}
		}
		var timezone string
		if err := tx.QueryRow(r.Context(), "SELECT timezone FROM tenants WHERE id=$1", tenant).Scan(&timezone); err != nil {
			return err
		}
		zone, err := time.LoadLocation(timezone)
		if err != nil {
			return platform.Fail(409, "invalid_tenant_timezone", "租户时区配置无效，请联系管理员")
		}
		start, end, err := dateRange(r, zone)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "SELECT set_config('TimeZone',$1,true)", timezone); err != nil {
			return err
		}
		result["from"], result["to"], result["timezone"] = start, end, timezone
		var visitors, contacts, conversations, queued, active, closed, messages int64
		if err := tx.QueryRow(r.Context(), `SELECT count(DISTINCT contact_id) FROM contact_visits WHERE tenant_id=$1 AND created_at>=$2 AND created_at<$3`, tenant, start, end).Scan(&visitors); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM contacts WHERE tenant_id=$1 AND created_at>=$2 AND created_at<$3`, tenant, start, end).Scan(&contacts); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE status='queued'),count(*) FILTER(WHERE status IN('active','assigned')),count(*) FILTER(WHERE status='closed') FROM conversations WHERE tenant_id=$1 AND created_at>=$2 AND created_at<$3`, tenant, start, end).Scan(&conversations, &queued, &active, &closed); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM messages WHERE tenant_id=$1 AND created_at>=$2 AND created_at<$3 AND audience='public'`, tenant, start, end).Scan(&messages); err != nil {
			return err
		}
		var wait, firstResponse float64
		if err := tx.QueryRow(r.Context(), `SELECT coalesce(avg(extract(epoch FROM (a.accepted-c.created_at))),0)::float8 FROM conversations c JOIN LATERAL(SELECT min(accepted_at) accepted FROM conversation_assignments a WHERE a.tenant_id=c.tenant_id AND a.conversation_id=c.id AND accepted_at IS NOT NULL)a ON true WHERE c.tenant_id=$1 AND c.created_at>=$2 AND c.created_at<$3`, tenant, start, end).Scan(&wait); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `SELECT coalesce(avg(extract(epoch FROM (m.first_reply-c.created_at))),0)::float8 FROM conversations c JOIN LATERAL(SELECT min(created_at) first_reply FROM messages m WHERE m.tenant_id=c.tenant_id AND m.conversation_id=c.id AND m.sender_kind='agent' AND m.audience='public')m ON true WHERE c.tenant_id=$1 AND c.created_at>=$2 AND c.created_at<$3`, tenant, start, end).Scan(&firstResponse); err != nil {
			return err
		}
		result["overview"] = map[string]any{"visitors": visitors, "contacts": contacts, "conversations": conversations, "queued": queued, "active": active, "closed": closed, "messages": messages, "avg_wait_seconds": wait, "avg_first_response_seconds": firstResponse}
		agents, err := queryMaps(r, tx, `SELECT a.membership_id::text agent_id,coalesce(nullif(m.display_name,''),u.display_name) agent_name,count(DISTINCT c.id) conversations,count(DISTINCT c.id)FILTER(WHERE c.status IN('active','assigned') AND c.current_agent_id=a.membership_id)active,count(DISTINCT c.id)FILTER(WHERE c.status='closed' AND c.current_agent_id=a.membership_id)closed FROM conversation_assignments a JOIN conversations c ON c.tenant_id=a.tenant_id AND c.id=a.conversation_id JOIN tenant_memberships m ON m.id=a.membership_id AND m.tenant_id=a.tenant_id JOIN users u ON u.id=m.user_id WHERE a.tenant_id=$1 AND c.created_at>=$2 AND c.created_at<$3 GROUP BY a.membership_id,m.display_name,u.display_name ORDER BY conversations DESC,agent_id`, tenant, start, end)
		if err != nil {
			return err
		}
		result["by_agent"] = agents
		channels, err := queryMaps(r, tx, `SELECT coalesce(nullif(`+platform.ConversationChannelSQL+`,''),'未标记')channel_code,count(*)conversations,count(DISTINCT c.contact_id)visitors FROM conversations c WHERE c.tenant_id=$1 AND c.created_at>=$2 AND c.created_at<$3 GROUP BY channel_code ORDER BY conversations DESC,channel_code`, tenant, start, end)
		if err != nil {
			return err
		}
		result["by_channel"] = channels
		daily, err := queryMaps(r, tx, `SELECT to_char(day,'YYYY-MM-DD')date,(SELECT count(DISTINCT v.contact_id) FROM contact_visits v WHERE v.tenant_id=$1 AND v.created_at>=day AND v.created_at<day+interval '1 day')visitors,(SELECT count(*) FROM conversations c WHERE c.tenant_id=$1 AND c.created_at>=day AND c.created_at<day+interval '1 day')conversations,(SELECT count(*) FROM messages m WHERE m.tenant_id=$1 AND m.created_at>=day AND m.created_at<day+interval '1 day' AND m.audience='public')messages FROM generate_series($2::timestamptz,$3::timestamptz-interval '1 day',interval '1 day')day ORDER BY day`, tenant, start, end)
		if err != nil {
			return err
		}
		result["daily"] = daily
		return nil
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// overview 返回平台规模与真实配置计数，不查询租户消息正文。
func (s *Service) overview(w http.ResponseWriter, r *http.Request) {
	var tenants, users int64
	if err := s.pool.QueryRow(r.Context(), "SELECT (SELECT count(*) FROM tenants),(SELECT count(*) FROM users)").Scan(&tenants, &users); err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"tenants": tenants, "users": users})
}

// queryMaps 将带明确别名的统计行转为响应对象。
func queryMaps(r *http.Request, tx pgx.Tx, query string, args ...any) ([]map[string]any, error) {
	rows, err := tx.Query(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	fields := rows.FieldDescriptions()
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		item := map[string]any{}
		for index, field := range fields {
			item[field.Name] = values[index]
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// dateRange 解析最近七天或指定日期，限制跨度防止不受控聚合。
func dateRange(r *http.Request, zone *time.Location) (time.Time, time.Time, error) {
	today := time.Now().In(zone)
	end := time.Date(today.Year(), today.Month(), today.Day()+1, 0, 0, 0, 0, zone)
	start := end.AddDate(0, 0, -7)
	for _, item := range []struct {
		key       string
		target    *time.Time
		inclusive bool
	}{{"from", &start, false}, {"to", &end, true}} {
		if raw := r.URL.Query().Get(item.key); raw != "" {
			value, err := time.ParseInLocation("2006-01-02", raw, zone)
			if err != nil {
				value, err = time.Parse(time.RFC3339, raw)
			} else if item.inclusive {
				value = value.AddDate(0, 0, 1)
			}
			if err != nil {
				return start, end, platform.Fail(400, "invalid_date", "请使用有效的日期范围")
			}
			*item.target = value
		}
	}
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		return start, end, platform.Fail(400, "invalid_date_range", "请选择一年以内的有效日期范围")
	}
	return start, end, nil
}
