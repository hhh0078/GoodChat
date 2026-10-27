// Package callbacks 提供租户访客进入回调配置、持久任务和投递结果，网络调用不阻塞咨询事务。
package callbacks

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// Service 管理进入回调，auth负责当前租户管理员权限。
type Service struct {
	pool           *pgxpool.Pool
	auth           platform.Authorizer
	config         platform.Config
	featureChecker func(context.Context, pgx.Tx, string, string) error
}

type settings struct {
	Enabled          bool   `json:"enabled"`
	URL              string `json:"url"`
	IncludeContext   bool   `json:"include_context"`
	IncludePhone     bool   `json:"include_phone"`
	Version          int64  `json:"version"`
	SecretConfigured bool   `json:"secret_configured"`
}

// New 建立回调服务，不启动网络投递。
func New(pool *pgxpool.Pool, auth platform.Authorizer, config platform.Config) *Service {
	return &Service{pool: pool, auth: auth, config: config}
}

// SetFeatureChecker 注入当前套餐的服务端权益校验。
func (s *Service) SetFeatureChecker(checker func(context.Context, pgx.Tx, string, string) error) {
	s.featureChecker = checker
}

// tenantTx 在租户事务中校验回调权益后执行配置、测试和日志操作。
func (s *Service) tenantTx(ctx context.Context, tenant string, action func(pgx.Tx) error) error {
	return platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		if s.featureChecker != nil {
			if err := s.featureChecker(ctx, tx, tenant, "callbacks"); err != nil {
				return err
			}
		}
		return action(tx)
	})
}

// featureDenied 区分套餐停用与数据库错误，避免回调关闭影响访客接入。
func featureDenied(err error) bool {
	var e *platform.APIError
	return errors.As(err, &e) && e.Code == "feature_not_available"
}

// Register 注册仅租户管理员可使用的配置、测试与日志接口。
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tenant/callback-settings", s.auth.RequireAdmin(s.getSettings))
	mux.HandleFunc("PUT /api/v1/tenant/callback-settings", s.auth.RequireAdmin(s.saveSettings))
	mux.HandleFunc("POST /api/v1/tenant/callback-settings/rotate-secret", s.auth.RequireAdmin(s.rotateSecret))
	mux.HandleFunc("POST /api/v1/tenant/callback-settings/test", s.auth.RequireAdmin(s.testDelivery))
	mux.HandleFunc("GET /api/v1/tenant/callback-deliveries", s.auth.RequireAdmin(s.listDeliveries))
	mux.HandleFunc("POST /api/v1/tenant/callback-deliveries/{id}/retry", s.auth.RequireAdmin(s.retryDelivery))
}

// getSettings 返回配置及密钥存在状态，不返回真实密钥。
func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) {
	result := settings{}
	err := s.tenantTx(r.Context(), platform.Current(r).TenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(r.Context(), "SELECT enabled,url,include_context,include_phone,version FROM tenant_callback_settings WHERE tenant_id=$1", platform.Current(r).TenantID).Scan(&result.Enabled, &result.URL, &result.IncludeContext, &result.IncludePhone, &result.Version)
		if err == pgx.ErrNoRows {
			return nil
		}
		result.SecretConfigured = err == nil
		return err
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// saveSettings 校验目标后更新配置版本，并取消旧的未完成任务。
func (s *Service) saveSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled        bool     `json:"enabled"`
		URL            string   `json:"url"`
		IncludeContext bool     `json:"include_context"`
		IncludePhone   bool     `json:"include_phone"`
		Events         []string `json:"events"`
	}
	if err := platform.Decode(w, r, &input); err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	input.URL = strings.TrimSpace(input.URL)
	for _, event := range input.Events {
		if event != "visitor.entered" {
			platform.ErrorJSON(w, r, platform.Fail(400, "unsupported_event", "当前支持访客进入回调"))
			return
		}
	}
	if input.Enabled {
		if err := s.validateURL(r.Context(), input.URL); err != nil {
			platform.ErrorJSON(w, r, err)
			return
		}
	}
	secret, err := platform.Token()
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	encrypted, err := s.encrypt(secret)
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	tenant := platform.Current(r).TenantID
	err = s.tenantTx(r.Context(), tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `INSERT INTO tenant_callback_settings(tenant_id,enabled,url,secret_encrypted,include_context,include_phone)VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id) DO UPDATE SET enabled=excluded.enabled,url=excluded.url,include_context=excluded.include_context,include_phone=excluded.include_phone,version=tenant_callback_settings.version+1,updated_at=now()`, tenant, input.Enabled, input.URL, encrypted, input.IncludeContext, input.IncludePhone)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), "UPDATE callback_deliveries SET status='cancelled',last_error='回调配置已经更新' WHERE tenant_id=$1 AND status IN('queued','retrying')", tenant)
		return err
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"saved": true, "enabled": input.Enabled, "url": input.URL, "secret_configured": true})
}

// rotateSecret 生成新签名密钥，仅在本次受认证响应中展示一次。
func (s *Service) rotateSecret(w http.ResponseWriter, r *http.Request) {
	secret, err := platform.Token()
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	encrypted, err := s.encrypt(secret)
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	err = s.tenantTx(r.Context(), platform.Current(r).TenantID, func(tx pgx.Tx) error {
		command, err := tx.Exec(r.Context(), "UPDATE tenant_callback_settings SET secret_encrypted=$2,version=version+1,updated_at=now() WHERE tenant_id=$1", platform.Current(r).TenantID, encrypted)
		if err == nil && command.RowsAffected() == 0 {
			return platform.Fail(409, "settings_required", "请先保存回调设置")
		}
		return err
	})
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, 200, map[string]any{"signing_secret": secret, "message": "请保存密钥，离开后将不再显示"})
}

// Enqueue 在创建访客访问的同一事务中保存经筛选的回调，重复访问编号不重复排队。
func (s *Service) Enqueue(ctx context.Context, tx pgx.Tx, tenantID, visitID string, payload map[string]any) error {
	if s.featureChecker != nil {
		if err := s.featureChecker(ctx, tx, tenantID, "callbacks"); err != nil {
			if featureDenied(err) {
				return nil
			}
			return err
		}
	}
	var configuration settings
	err := tx.QueryRow(ctx, "SELECT enabled,url,include_context,include_phone,version FROM tenant_callback_settings WHERE tenant_id=$1", tenantID).Scan(&configuration.Enabled, &configuration.URL, &configuration.IncludeContext, &configuration.IncludePhone, &configuration.Version)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if !configuration.Enabled {
		return nil
	}
	eventID, err := platform.Token()
	if err != nil {
		return err
	}
	filtered := map[string]any{"event_id": eventID, "event_type": "visitor.entered", "occurred_at": time.Now().UTC().Format(time.RFC3339Nano), "tenant_id": tenantID, "visit_id": visitID}
	for _, key := range []string{"service_space_id", "entry_id", "contact_id", "channel_code", "display_name", "external_user_id"} {
		if value, ok := payload[key]; ok {
			filtered[key] = value
		}
	}
	if profile, ok := payload["profile"]; ok {
		encoded, _ := json.Marshal(profile)
		var object map[string]any
		if json.Unmarshal(encoded, &object) == nil {
			if !configuration.IncludePhone {
				delete(object, "phone")
			}
			filtered["profile"] = object
		}
	}
	if configuration.IncludePhone {
		if phone, ok := payload["phone"]; ok {
			filtered["phone"] = phone
		}
	}
	if configuration.IncludeContext {
		if value, ok := payload["context"]; ok {
			filtered["context"] = value
		}
	}
	body, err := json.Marshal(filtered)
	if err != nil {
		return err
	}
	var deliveryID string
	err = tx.QueryRow(ctx, `INSERT INTO callback_deliveries(tenant_id,event_id,visit_id,payload,target_url,config_version)VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,event_type,visit_id) DO NOTHING RETURNING id`, tenantID, eventID, visitID, body, configuration.URL, configuration.Version).Scan(&deliveryID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO callback_dispatch_queue(delivery_id,tenant_id)VALUES($1,$2)", deliveryID, tenantID)
	return err
}

// encrypt 使用服务端身份密钥保护回调密钥，数据库中不保存明文。
func (s *Service) encrypt(value string) (string, error) {
	key := sha256.Sum256([]byte(s.config.AuthSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(value), nil)), nil
}

// decrypt 读取仅用于服务器签名的回调密钥。
func (s *Service) decrypt(value string) (string, error) {
	key := sha256.Sum256([]byte(s.config.AuthSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	body, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(body) < aead.NonceSize() {
		return "", fmt.Errorf("回调密钥格式异常")
	}
	plain, err := aead.Open(nil, body[:aead.NonceSize()], body[aead.NonceSize():], nil)
	return string(plain), err
}
