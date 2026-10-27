// 文件作用说明：提供进入回调的测试、投递记录及受权限控制的重试操作。
package callbacks

import (
	"github.com/jackc/pgx/v5"
	"net/http"
	"supportdesk/backend/internal/platform"
)

// testDelivery 发送无真实客户资料的测试任务，结果通过投递记录查看。
func (s *Service) testDelivery(w http.ResponseWriter, r *http.Request) {
	identifier, err := platform.Token()
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	tenant := platform.Current(r).TenantID
	var id string
	err = s.tenantTx(r.Context(), tenant, func(tx pgx.Tx) error {
		var enabled bool
		if err := tx.QueryRow(r.Context(), "SELECT enabled FROM tenant_callback_settings WHERE tenant_id=$1", tenant).Scan(&enabled); err != nil || !enabled {
			return platform.Fail(409, "callback_disabled", "请先保存并启用回调设置")
		}
		if err := s.Enqueue(r.Context(), tx, tenant, "test_"+identifier, map[string]any{"display_name": "GoodChat回调测试", "context": map[string]any{"test": true}}); err != nil {
			return err
		}
		return tx.QueryRow(r.Context(), "SELECT id FROM callback_deliveries WHERE tenant_id=$1 AND visit_id=$2", tenant, "test_"+identifier).Scan(&id)
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 202, map[string]any{"id": id, "status": "queued"})
}

// listDeliveries 只返回本租户最近投递摘要，省略可能包含客户资料的载荷。
func (s *Service) listDeliveries(w http.ResponseWriter, r *http.Request) {
	items := []map[string]any{}
	err := s.tenantTx(r.Context(), platform.Current(r).TenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), "SELECT id,event_id,event_type,visit_id,status,attempts,response_status,last_error,created_at,delivered_at FROM callback_deliveries WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 100", platform.Current(r).TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			item := map[string]any{}
			for i, name := range []string{"id", "event_id", "event_type", "visit_id", "status", "attempts", "response_status", "last_error", "created_at", "delivered_at"} {
				if raw, ok := values[i].([16]byte); ok {
					item[name] = pgUUID(raw)
				} else {
					item[name] = values[i]
				}
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items})
}

// retryDelivery 只允许对仍匹配当前配置的失败任务重新排队，不重发已成功事件。
func (s *Service) retryDelivery(w http.ResponseWriter, r *http.Request) {
	err := s.tenantTx(r.Context(), platform.Current(r).TenantID, func(tx pgx.Tx) error {
		var status string
		var version, currentVersion int64
		var enabled bool
		if err := tx.QueryRow(r.Context(), `SELECT d.status,d.config_version,c.version,c.enabled FROM callback_deliveries d JOIN tenant_callback_settings c USING(tenant_id) WHERE d.id::text=$1 AND d.tenant_id=$2 FOR UPDATE OF d`, r.PathValue("id"), platform.Current(r).TenantID).Scan(&status, &version, &currentVersion, &enabled); err == pgx.ErrNoRows {
			return platform.Fail(404, "not_found", "未找到该回调记录")
		} else if err != nil {
			return err
		}
		if status == "delivered" {
			return platform.Fail(409, "already_delivered", "该事件已经发送成功")
		}
		if status == "running" {
			return platform.Fail(409, "delivery_running", "该事件正在发送，请稍后查看")
		}
		if !enabled || version != currentVersion {
			return platform.Fail(409, "settings_changed", "回调配置已经改变，请发送新的测试事件")
		}
		if _, err := tx.Exec(r.Context(), "UPDATE callback_deliveries SET status='queued',attempts=0,next_attempt_at=now(),lease_until=NULL,last_error='' WHERE id::text=$1 AND tenant_id=$2", r.PathValue("id"), platform.Current(r).TenantID); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), "INSERT INTO callback_dispatch_queue(delivery_id,tenant_id)VALUES($1::uuid,$2) ON CONFLICT(delivery_id) DO UPDATE SET next_attempt_at=now(),lease_until=NULL", r.PathValue("id"), platform.Current(r).TenantID)
		return err
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]bool{"queued": true})
}

// pgUUID 将数据库驱动返回的UUID数组格式化为标准文本。
func pgUUID(value [16]byte) string { return fmtUUID(value[:]) }
