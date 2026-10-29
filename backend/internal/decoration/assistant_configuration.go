// 文件作用说明：保存相互隔离的租户与平台模型配置，以绑定归属的AES-GCM密文保护密钥，并管理不可变平台创作约束。
package decoration

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// assistantConfiguration 定义不含可读密钥的供应商设置，密钥只在写入请求及服务端内存中使用。
type assistantConfiguration struct {
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKey         string `json:"api_key,omitempty"`
	HasAPIKey      bool   `json:"has_api_key"`
	Enabled        bool   `json:"enabled"`
	ImageBaseURL   string `json:"image_base_url"`
	ImageModel     string `json:"image_model"`
	ImageAPIKey    string `json:"image_api_key,omitempty"`
	HasImageAPIKey bool   `json:"has_image_api_key"`
	Version        int64  `json:"version"`
	Configured     bool   `json:"configured"`
}

// assistantFoundation 保存平台已发布的提示词和核心脚本版本。
type assistantFoundation struct {
	Version int64           `json:"version"`
	Prompt  string          `json:"prompt"`
	Scripts TemplateScripts `json:"scripts"`
}

// assistantSecret 用独立用途和配置归属作为密钥派生及附加认证数据，阻止密文跨租户挪用。
func (s *Service) assistantSecret(value, owner string, decrypt bool) (string, error) {
	if len(s.cfg.AuthSecret) < 32 {
		return "", platform.Fail(503, "assistant_secret_unavailable", "请先配置服务端安全密钥")
	}
	key := sha256.Sum256([]byte(s.cfg.AuthSecret + "\x00goodchat-assistant"))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if decrypt {
		raw, e := base64.RawStdEncoding.DecodeString(value)
		if e != nil || len(raw) < aead.NonceSize() {
			return "", platform.Fail(503, "assistant_secret_invalid", "模型密钥无法读取，请重新保存配置")
		}
		plain, e := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(owner))
		if e != nil {
			return "", platform.Fail(503, "assistant_secret_invalid", "模型密钥无法读取，请重新保存配置")
		}
		return string(plain), nil
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(value), []byte(owner))), nil
}

// loadAssistantConfiguration 读取当前明确归属的配置，平台源不回落使用租户密钥。
func (s *Service) loadAssistantConfiguration(ctx context.Context, tx pgx.Tx, source, tenant string, secrets bool) (assistantConfiguration, error) {
	var cfg assistantConfiguration
	var raw []byte
	var secret, imageSecret string
	query := "SELECT config,secret_ciphertext,image_secret_ciphertext,version FROM assistant_tenant_ai_settings WHERE tenant_id=$1"
	args := []any{tenant}
	owner := "tenant:" + tenant
	if source == "platform" {
		query = "SELECT config,secret_ciphertext,image_secret_ciphertext,version FROM assistant_platform_ai_settings WHERE singleton"
		args = nil
		owner = "platform"
	}
	err := tx.QueryRow(ctx, query, args...).Scan(&raw, &secret, &imageSecret, &cfg.Version)
	if err == pgx.ErrNoRows {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	version := cfg.Version
	if json.Unmarshal(raw, &cfg) != nil {
		return cfg, platform.Fail(503, "assistant_configuration_invalid", "AI配置无法读取")
	}
	cfg.Version = version
	cfg.APIKey = ""
	cfg.ImageAPIKey = ""
	cfg.HasAPIKey = secret != ""
	cfg.HasImageAPIKey = imageSecret != ""
	cfg.Configured = cfg.BaseURL != "" && cfg.Model != "" && cfg.HasAPIKey
	if secrets && secret != "" {
		cfg.APIKey, err = s.assistantSecret(secret, owner, true)
		if err != nil {
			return cfg, err
		}
	}
	if secrets && imageSecret != "" {
		cfg.ImageAPIKey, err = s.assistantSecret(imageSecret, owner, true)
	}
	return cfg, err
}

// assistantSettingsLockKey 平台配置为单例而固定锁名，租户配置按归属分别串行化。
func assistantSettingsLockKey(source, tenant string) string {
	if source == "platform" {
		return "assistant-settings:platform"
	}
	return "assistant-settings:tenant:" + tenant
}

// assistantSettings 由调用路由决定平台或租户范围，响应始终省略原始密钥。
func (s *Service) assistantSettings(w http.ResponseWriter, r *http.Request) {
	source := "tenant"
	if strings.HasPrefix(r.URL.Path, "/api/v1/platform/") {
		source = "platform"
	}
	p := platform.Current(r)
	action := func(tx pgx.Tx) (any, error) {
		if r.Method != http.MethodGet {
			if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", assistantSettingsLockKey(source, p.TenantID)); err != nil {
				return nil, err
			}
		}
		current, err := s.loadAssistantConfiguration(r.Context(), tx, source, p.TenantID, false)
		if err != nil {
			return nil, err
		}
		if r.Method == http.MethodGet {
			return current, nil
		}
		var input assistantConfiguration
		if err = platform.Decode(w, r, &input); err != nil {
			return nil, err
		}
		if input.Version != current.Version {
			return nil, conflict()
		}
		input.BaseURL = strings.TrimSpace(input.BaseURL)
		input.Model = strings.TrimSpace(input.Model)
		input.ImageBaseURL = strings.TrimSpace(input.ImageBaseURL)
		input.ImageModel = strings.TrimSpace(input.ImageModel)
		if len(input.Model) > 200 || len(input.ImageModel) > 200 || len(input.APIKey) > 4096 || len(input.ImageAPIKey) > 4096 {
			return nil, invalidDesign("模型名称或密钥超过长度限制")
		}
		if input.BaseURL != "" {
			if _, err = assistantEndpoint(input.BaseURL, "chat/completions"); err != nil {
				return nil, err
			}
		}
		if input.ImageBaseURL != "" {
			if _, err = assistantEndpoint(input.ImageBaseURL, "images/generations"); err != nil {
				return nil, err
			}
		}
		if input.Enabled && (input.BaseURL == "" || input.Model == "" || input.APIKey == "" && !current.HasAPIKey) {
			return nil, invalidDesign("启用AI前请填写模型地址、名称及密钥")
		}
		owner := "tenant:" + p.TenantID
		if source == "platform" {
			owner = "platform"
		}
		var secret, imageSecret string
		if input.APIKey != "" {
			secret, err = s.assistantSecret(input.APIKey, owner, false)
			if err != nil {
				return nil, err
			}
		}
		if input.ImageAPIKey != "" {
			imageSecret, err = s.assistantSecret(input.ImageAPIKey, owner, false)
			if err != nil {
				return nil, err
			}
		}
		input.APIKey = ""
		input.ImageAPIKey = ""
		input.HasAPIKey = false
		input.HasImageAPIKey = false
		input.Version = 0
		input.Configured = false
		raw, _ := json.Marshal(input)
		if source == "platform" {
			_, err = tx.Exec(r.Context(), `INSERT INTO assistant_platform_ai_settings(config,secret_ciphertext,image_secret_ciphertext) VALUES($1,$2,$3) ON CONFLICT(singleton) DO UPDATE SET config=$1,secret_ciphertext=CASE WHEN $2='' THEN assistant_platform_ai_settings.secret_ciphertext ELSE $2 END,image_secret_ciphertext=CASE WHEN $3='' THEN assistant_platform_ai_settings.image_secret_ciphertext ELSE $3 END,version=assistant_platform_ai_settings.version+1,updated_at=now()`, raw, secret, imageSecret)
		} else {
			_, err = tx.Exec(r.Context(), `INSERT INTO assistant_tenant_ai_settings(tenant_id,config,secret_ciphertext,image_secret_ciphertext) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id) DO UPDATE SET config=$2,secret_ciphertext=CASE WHEN $3='' THEN assistant_tenant_ai_settings.secret_ciphertext ELSE $3 END,image_secret_ciphertext=CASE WHEN $4='' THEN assistant_tenant_ai_settings.image_secret_ciphertext ELSE $4 END,version=assistant_tenant_ai_settings.version+1,updated_at=now()`, p.TenantID, raw, secret, imageSecret)
		}
		if err != nil {
			return nil, err
		}
		return s.loadAssistantConfiguration(r.Context(), tx, source, p.TenantID, false)
	}
	s.assistantScope(w, r, source, action)
}

// assistantScope 在租户事务或经过平台鉴权的普通事务内统一返回结果。
func (s *Service) assistantScope(w http.ResponseWriter, r *http.Request, source string, action func(pgx.Tx) (any, error)) {
	if source != "platform" {
		s.tenant(w, r, action)
		return
	}
	var value any
	err := pgx.BeginTxFunc(r.Context(), s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error { var e error; value, e = action(tx); return e })
	respond(w, r, value, err)
}

// currentAssistantFoundation 返回最新不可变平台约束，首次部署使用代码内经过验证的默认版本。
func (s *Service) currentAssistantFoundation(ctx context.Context, tx pgx.Tx) (assistantFoundation, error) {
	var value assistantFoundation
	var scripts []byte
	err := tx.QueryRow(ctx, "SELECT version,prompt,scripts FROM assistant_contract_versions ORDER BY version DESC LIMIT 1").Scan(&value.Version, &value.Prompt, &scripts)
	if err == pgx.ErrNoRows {
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(718430261030)"); err != nil {
			return value, err
		}
		err = tx.QueryRow(ctx, "SELECT version,prompt,scripts FROM assistant_contract_versions ORDER BY version DESC LIMIT 1").Scan(&value.Version, &value.Prompt, &scripts)
		if err == pgx.ErrNoRows {
			value = assistantFoundation{Prompt: templateSystemContract(), Scripts: defaultTemplateScripts()}
			raw, _ := json.Marshal(value.Scripts)
			err = tx.QueryRow(ctx, "INSERT INTO assistant_contract_versions(prompt,scripts) VALUES($1,$2) RETURNING version", value.Prompt, raw).Scan(&value.Version)
			return value, err
		}
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(scripts, &value.Scripts)
	return value, err
}

// currentTemplateScripts 为行业模板复制和生成打包提供同一平台核心脚本快照。
func (s *Service) currentTemplateScripts(ctx context.Context, tx pgx.Tx) (TemplateScripts, error) {
	value, err := s.currentAssistantFoundation(ctx, tx)
	return value.Scripts, err
}

// assistantFoundationVersions 返回可用于显式恢复的历史版本，恢复通过现有PUT复制为新版本，不修改任何旧快照。
func (s *Service) assistantFoundationVersions(w http.ResponseWriter, r *http.Request) {
	s.assistantScope(w, r, "platform", func(tx pgx.Tx) (any, error) {
		rows, err := tx.Query(r.Context(), "SELECT to_jsonb(v) FROM assistant_contract_versions v ORDER BY version DESC LIMIT 100")
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(rows)
		if err != nil {
			return nil, err
		}
		initial, _ := json.Marshal(assistantFoundation{Version: 0, Prompt: templateSystemContract(), Scripts: defaultTemplateScripts()})
		items = append(items, initial)
		return map[string]any{"items": items}, nil
	})
}

// assistantFoundationSettings 创建平台约束新版本，既有模板继续持有原脚本字节。
func (s *Service) assistantFoundationSettings(w http.ResponseWriter, r *http.Request) {
	s.assistantScope(w, r, "platform", func(tx pgx.Tx) (any, error) {
		if r.Method == http.MethodPut {
			if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(718430261030)"); err != nil {
				return nil, err
			}
		}
		current, err := s.currentAssistantFoundation(r.Context(), tx)
		if err != nil || r.Method == http.MethodGet {
			return current, err
		}
		var input assistantFoundation
		if err = decodeAssistantJSON(w, r, &input, 2<<20); err != nil {
			return nil, err
		}
		if input.Version != current.Version {
			return nil, conflict()
		}
		if len(input.Prompt) < 20 || len(input.Prompt) > 32000 {
			return nil, invalidDesign("系统提示词须为20至32000字节")
		}
		if err = validateTemplateScripts(input.Scripts); err != nil {
			return nil, err
		}
		if input.Scripts.Version == current.Scripts.Version && (input.Scripts.SDK != current.Scripts.SDK || input.Scripts.ChatUI != current.Scripts.ChatUI) {
			return nil, invalidDesign("修改核心脚本时请同时修改脚本版本名称")
		}
		initial := defaultTemplateScripts()
		if input.Scripts.Version == initial.Version && (input.Scripts.SDK != initial.SDK || input.Scripts.ChatUI != initial.ChatUI) {
			return nil, invalidDesign("内置核心脚本版本不可改写，请使用新版本名称")
		}
		rows, err := tx.Query(r.Context(), "SELECT scripts FROM assistant_contract_versions WHERE scripts->>'version'=$1", input.Scripts.Version)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var registered TemplateScripts
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			if json.Unmarshal(raw, &registered) != nil {
				rows.Close()
				return nil, invalidDesign("历史脚本版本无法读取")
			}
			if registered.SDK != input.Scripts.SDK || registered.ChatUI != input.Scripts.ChatUI {
				rows.Close()
				return nil, invalidDesign("该核心脚本版本名已登记其他内容，请更换版本名称")
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(input.Scripts)
		if _, err = tx.Exec(r.Context(), "INSERT INTO assistant_contract_versions(prompt,scripts,created_by) VALUES($1,$2,$3)", input.Prompt, raw, platform.Current(r).UserID); err != nil {
			return nil, err
		}
		return s.currentAssistantFoundation(r.Context(), tx)
	})
}

// decodeAssistantJSON 严格限制请求预算并拒绝未知字段和多份JSON。
func decodeAssistantJSON(w http.ResponseWriter, r *http.Request, input any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		return platform.Fail(400, "invalid_request", "请求必须为单个有效JSON对象")
	}
	return nil
}
