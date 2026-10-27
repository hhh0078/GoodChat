// 文件作用说明：以短租约认领进入回调任务，异步投递并记录重试或最终结果。
package callbacks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// Start 启动可取消的任务循环，服务关闭后不再认领新任务。
func (s *Service) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for count := 0; count < 10; count++ {
					if !s.claim(ctx) {
						break
					}
				}
			}
		}
	}()
}

// claim 仅从无客户载荷的调度索引取得定位，再按租户权限读取任务。
func (s *Service) claim(ctx context.Context) bool {
	var id, tenant string
	err := s.pool.QueryRow(ctx, `UPDATE callback_dispatch_queue q SET lease_until=now()+interval '30 seconds' WHERE delivery_id=(SELECT delivery_id FROM callback_dispatch_queue WHERE next_attempt_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_attempt_at,delivery_id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING delivery_id,tenant_id`).Scan(&id, &tenant)
	if err != nil {
		return false
	}
	s.deliver(ctx, id, tenant)
	return true
}

// deliver 执行一次网络投递，失败结果不会回滚已经完成的咨询或消息。
func (s *Service) deliver(ctx context.Context, id, tenant string) {
	var body []byte
	var target, encrypted, status string
	var version, currentVersion int64
	var attempts int
	var enabled bool
	err := platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		allowed := true

		if s.featureChecker != nil {
			if err := s.featureChecker(ctx, tx, tenant, "callbacks"); err != nil {
				if featureDenied(err) {
					allowed = false
				} else {
					return err
				}
			}
		}
		if err := tx.QueryRow(ctx, `SELECT d.payload,d.target_url,c.secret_encrypted,d.status,d.config_version,c.version,d.attempts,c.enabled FROM callback_deliveries d JOIN tenant_callback_settings c USING(tenant_id) WHERE d.id=$1 AND d.tenant_id=$2 FOR UPDATE OF d`, id, tenant).Scan(&body, &target, &encrypted, &status, &version, &currentVersion, &attempts, &enabled); err != nil {
			return err
		}
		enabled = enabled && allowed
		if !enabled || version != currentVersion || status == "cancelled" || status == "delivered" {
			if _, err := tx.Exec(ctx, "UPDATE callback_deliveries SET status=CASE WHEN status='delivered' THEN status ELSE 'cancelled' END WHERE id=$1 AND tenant_id=$2", id, tenant); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "DELETE FROM callback_dispatch_queue WHERE delivery_id=$1", id)
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE callback_deliveries SET status='running',attempts=attempts+1,lease_until=now()+interval '30 seconds' WHERE id=$1 AND tenant_id=$2", id, tenant)
		return err
	})
	if err != nil {
		return
	}
	if !enabled || version != currentVersion || status == "cancelled" || status == "delivered" {
		return
	}
	attempts++
	code := 0
	message := ""
	retryable := true
	if err = s.validateURL(ctx, target); err != nil {
		message = "回调地址校验失败"
		retryable = false
	} else {
		secret, secretErr := s.decrypt(encrypted)
		if secretErr != nil {
			message = "回调签名配置无法读取"
			retryable = false
		} else {
			request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
			if requestErr != nil {
				message = "回调请求无法创建"
				retryable = false
			} else {
				timestamp := fmt.Sprintf("%d", time.Now().Unix())
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-GoodChat-Timestamp", timestamp)
				request.Header.Set("X-GoodChat-Signature", signature(secret, timestamp, body))
				request.Header.Set("X-GoodChat-Event-ID", extractEventID(body))
				response, requestErr := s.client().Do(request)
				if requestErr != nil {
					message = "连接或等待回调响应超时"
				} else {
					code = response.StatusCode
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
					response.Body.Close()
					if code < 200 || code >= 300 {
						message = fmt.Sprintf("回调返回HTTP %d", code)
						retryable = code >= 500 || code == 408 || code == 429
					}
				}
			}
		}
	}
	s.finish(ctx, id, tenant, attempts, code, message, retryable)
}

// finish 原子保存发送结果；稳定事件ID让接收方可以去重不确定的重试。
func (s *Service) finish(ctx context.Context, id, tenant string, attempts, code int, message string, retryable bool) {
	_ = platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		if message == "" && code >= 200 && code < 300 {
			if _, err := tx.Exec(ctx, "UPDATE callback_deliveries SET status='delivered',response_status=$3,last_error='',delivered_at=now(),lease_until=NULL WHERE id=$1 AND tenant_id=$2", id, tenant, code); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "DELETE FROM callback_dispatch_queue WHERE delivery_id=$1", id)
			return err
		}
		state := "failed"
		if retryable && attempts < 8 {
			state = "retrying"
		}
		seconds := 30 * (1 << min(attempts-1, 7))
		if seconds > 3600 {
			seconds = 3600
		}
		next := time.Now().Add(time.Duration(seconds) * time.Second)
		if _, err := tx.Exec(ctx, "UPDATE callback_deliveries SET status=$3,response_status=NULLIF($4,0),last_error=$5,next_attempt_at=$6,lease_until=NULL WHERE id=$1 AND tenant_id=$2", id, tenant, state, code, message, next); err != nil {
			return err
		}
		if state == "retrying" {
			_, err := tx.Exec(ctx, "UPDATE callback_dispatch_queue SET next_attempt_at=$2,lease_until=NULL WHERE delivery_id=$1", id, next)
			return err
		}
		_, err := tx.Exec(ctx, "DELETE FROM callback_dispatch_queue WHERE delivery_id=$1", id)
		return err
	})
}

// extractEventID 只提取公开事件标识，不记录或输出客户载荷。
func extractEventID(body []byte) string {
	var value struct {
		ID string `json:"event_id"`
	}
	_ = json.Unmarshal(body, &value)
	return value.ID
}
