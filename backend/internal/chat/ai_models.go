// 文件作用说明：管理客服AI的Eino模型配置、不可变价格版本及用途隔离的加密密钥。
package chat

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
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiModel 保存人民币每百万Token费率，整数单位为微元；密钥不属于配置JSON。
type aiModel struct {
	SupportsImages bool   `json:"supports_images"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	APIKey         string `json:"api_key,omitempty"`
	HasAPIKey      bool   `json:"has_api_key"`
	Enabled        bool   `json:"enabled"`
	Version        int64  `json:"version"`
	CostInput      int64  `json:"cost_input"`
	CostOutput     int64  `json:"cost_output"`
	SaleInput      int64  `json:"sale_input"`
	SaleOutput     int64  `json:"sale_output"`
	MaxInput       int    `json:"max_input_tokens"`
	MaxOutput      int    `json:"max_output_tokens"`
}

// registerAI 注册所有客服AI管理接口，保持平台权限与租户权限分开。
func (s *Service) registerAI(mux *http.ServeMux) {
	for _, method := range []string{"GET", "POST"} {
		mux.HandleFunc(method+" /api/v1/platform/ai/models", s.auth.RequirePlatform(s.aiModels))
	}
	mux.HandleFunc("PATCH /api/v1/platform/ai/models/{id}", s.auth.RequirePlatform(s.aiModels))
	mux.HandleFunc("POST /api/v1/platform/ai/models/{id}/test", s.auth.RequirePlatform(s.aiModelTest))
	mux.HandleFunc("GET /api/v1/platform/ai/tenants/{id}/jobs", s.auth.RequirePlatform(s.aiPlatformJobs))
	mux.HandleFunc("POST /api/v1/platform/ai/tenants/{id}/jobs/{job}/reconcile", s.auth.RequirePlatform(s.aiReconcile))
	mux.HandleFunc("GET /api/v1/platform/ai/usage", s.auth.RequirePlatform(s.aiPlatformUsage))
	mux.HandleFunc("POST /api/v1/platform/ai/wallets/{id}/credit", s.auth.RequirePlatform(s.aiCredit))
	mux.HandleFunc("PUT /api/v1/platform/ai/wallets/{id}/mode", s.auth.RequirePlatform(s.aiWalletMode))
	for _, resource := range []string{"knowledge", "scripts", "media"} {
		mux.HandleFunc("GET /api/v1/tenant/ai/"+resource, s.auth.RequireTenant(s.aiResources(resource)))
		mux.HandleFunc("POST /api/v1/tenant/ai/"+resource, s.auth.RequireTenant(s.aiResources(resource)))
		mux.HandleFunc("PATCH /api/v1/tenant/ai/"+resource+"/{id}", s.auth.RequireTenant(s.aiResources(resource)))
		mux.HandleFunc("DELETE /api/v1/tenant/ai/"+resource+"/{id}", s.auth.RequireTenant(s.aiResources(resource)))
	}
	mux.HandleFunc("GET /api/v1/tenant/ai/knowledge/{id}/versions", s.auth.RequireTenant(s.aiKnowledgeVersions))
	mux.HandleFunc("POST /api/v1/tenant/ai/knowledge/{id}/restore", s.auth.RequireTenant(s.aiKnowledgeRestore))
	mux.HandleFunc("GET /api/v1/tenant/ai/models", s.auth.RequireTenant(s.aiAvailableModels))
	mux.HandleFunc("GET /api/v1/tenant/ai/bots", s.auth.RequireTenant(s.aiBots))
	mux.HandleFunc("GET /api/v1/me/ai/bot", s.auth.RequireTenant(s.aiBot))
	mux.HandleFunc("PUT /api/v1/me/ai/bot", s.auth.RequireTenant(s.aiBot))
	mux.HandleFunc("POST /api/v1/me/ai/test", s.auth.RequireTenant(s.aiTest))
	mux.HandleFunc("GET /api/v1/tenant/ai/settings", s.auth.RequireTenant(s.aiSettings))
	mux.HandleFunc("PUT /api/v1/tenant/ai/settings", s.auth.RequireAdmin(s.aiSettings))
	mux.HandleFunc("GET /api/v1/tenant/ai/usage", s.auth.RequireTenant(s.aiUsage))
	mux.HandleFunc("GET /api/v1/tenant/ai/jobs/{id}", s.auth.RequireTenant(s.aiJob))
	mux.HandleFunc("GET /api/v1/conversations/{id}/ai", s.withActor(s.aiConversation))
	mux.HandleFunc("POST /api/v1/conversations/{id}/ai", s.withActor(s.aiConversation))
	mux.HandleFunc("POST /api/v1/conversations/{id}/ai/drafts/{job}/send", s.withActor(s.aiDraftSend))
}

// aiSecret 将模型编号作为加密附加数据，阻止跨用途挪用模型密钥。
func (s *Service) aiSecret(value, id string, decrypt bool) (string, error) {
	if len(s.cfg.AuthSecret) < 32 {
		return "", platform.Fail(503, "ai_secret_unavailable", "AI配置不可用")
	}
	key := sha256.Sum256([]byte(s.cfg.AuthSecret + "\x00goodchat-customer-ai"))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if decrypt {
		b, e := base64.RawStdEncoding.DecodeString(value)
		if e != nil || len(b) < a.NonceSize() {
			return "", platform.Fail(503, "ai_secret_invalid", "AI配置不可用")
		}
		plain, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(id))
		return string(plain), e
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(a.Seal(nonce, nonce, []byte(value), []byte(id))), nil
}

// validateAIModel 对端点、模型和整数价格做边界校验，禁止把密钥放进地址。
func (s *Service) validateAIModel(m *aiModel) error {
	u, e := url.Parse(m.BaseURL)
	local := s.cfg.CallbackAllowLocal && !s.cfg.Production
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(m.BaseURL) > 2048 || (u.Scheme != "https" && !(local && u.Scheme == "http")) {
		return platform.Fail(400, "ai_invalid_endpoint", "请填写有效的HTTPS模型接口地址")
	}
	m.BaseURL = strings.TrimRight(m.BaseURL, "/")
	m.Name = strings.TrimSpace(m.Name)
	m.Model = strings.TrimSpace(m.Model)
	if m.Name == "" || len(m.Name) > 160 || m.Model == "" || len(m.Model) > 200 || m.MaxInput < 4096 || m.MaxInput > 64000 || m.MaxOutput < 128 || m.MaxOutput > 4096 || m.MaxOutput >= m.MaxInput {
		return platform.Fail(400, "ai_invalid_model", "请检查模型名称和调用上限")
	}
	for _, v := range []int64{m.CostInput, m.CostOutput, m.SaleInput, m.SaleOutput} {
		if v < 0 || v > 1000000000000 {
			return platform.Fail(400, "ai_invalid_price", "请检查AI价格")
		}
	}
	return nil
}

// aiModels 以乐观锁保存新配置，历史价格及加密密钥版本保持不变。
func (s *Service) aiModels(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		rows, e := s.pool.Query(r.Context(), `SELECT v.config||jsonb_build_object('id',m.id,'name',m.name,'enabled',m.enabled,'version',m.version,'has_api_key',v.secret_ciphertext<>'') FROM ai_chat_models m JOIN ai_chat_model_versions v ON v.model_id=m.id AND v.version=m.version ORDER BY m.created_at`)
		if e != nil {
			replyError(w, r, e)
			return
		}
		items, e := jsonRows(rows)
		if e != nil {
			replyError(w, r, e)
			return
		}
		platform.JSON(w, 200, map[string]any{"items": items})
		return
	}
	var in aiModel
	if e := platform.Decode(w, r, &in); e != nil {
		replyError(w, r, e)
		return
	}
	if e := s.validateAIModel(&in); e != nil {
		replyError(w, r, e)
		return
	}
	id := r.PathValue("id")
	creating := id == ""
	if creating {
		id = newID()
	}
	if !uuidPattern.MatchString(id) {
		returnError(w, r, "invalid_id", "记录不存在或无权访问")
		return
	}
	e := pgx.BeginFunc(r.Context(), s.pool, func(tx pgx.Tx) error {
		secret := ""
		version := int64(1)
		if !creating {
			var old int64
			if e := tx.QueryRow(r.Context(), `SELECT m.version,v.secret_ciphertext FROM ai_chat_models m JOIN ai_chat_model_versions v ON v.model_id=m.id AND v.version=m.version WHERE m.id=$1 FOR UPDATE OF m`, id).Scan(&old, &secret); e != nil {
				return e
			}
			if old != in.Version {
				return platform.Fail(409, "ai_version_conflict", "内容已更新，请刷新后重试")
			}
			version = old + 1
		}
		if in.APIKey != "" {
			var e error
			secret, e = s.aiSecret(in.APIKey, id, false)
			if e != nil {
				return e
			}
		}
		if secret == "" {
			return platform.Fail(400, "ai_key_required", "请配置模型密钥")
		}
		in.APIKey = ""
		in.ID = id
		in.Version = version
		in.HasAPIKey = true
		if _, e := tx.Exec(r.Context(), `INSERT INTO ai_chat_models(id,name,enabled,version) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET name=$2,enabled=$3,version=$4,updated_at=now()`, id, in.Name, in.Enabled, version); e != nil {
			return e
		}
		_, e := tx.Exec(r.Context(), `INSERT INTO ai_chat_model_versions(model_id,version,config,secret_ciphertext) VALUES($1,$2,$3,$4)`, id, version, jsonValue(in), secret)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, in)
}

// loadAIModel 只为明确版本解密密钥，仍要求平台当前允许新调用。
func (s *Service) loadAIModel(ctx context.Context, tx pgx.Tx, id string, version int64) (aiModel, error) {
	var m aiModel
	var raw []byte
	var secret string
	e := tx.QueryRow(ctx, `SELECT v.config,v.secret_ciphertext FROM ai_chat_models m JOIN ai_chat_model_versions v ON v.model_id=m.id AND v.version=CASE WHEN $2::bigint=0 THEN m.version ELSE $2 END WHERE m.enabled AND ($1='' OR m.id=NULLIF($1,'')::uuid) ORDER BY m.created_at LIMIT 1`, id, version).Scan(&raw, &secret)
	if e != nil {
		return m, platform.Fail(503, "ai_model_unavailable", "AI模型暂不可用")
	}
	if e = json.Unmarshal(raw, &m); e != nil {
		return m, e
	}
	m.APIKey, e = s.aiSecret(secret, m.ID, true)
	return m, e
}

// aiAvailableModels 向租户提供可选模型和售价，隐藏供应商地址与成本。
func (s *Service) aiAvailableModels(w http.ResponseWriter, r *http.Request) {
	rows, e := s.pool.Query(r.Context(), `SELECT jsonb_build_object('id',m.id,'name',m.name,'sale_input',v.config->'sale_input','sale_output',v.config->'sale_output','max_output_tokens',v.config->'max_output_tokens','supports_images',v.config->'supports_images') FROM ai_chat_models m JOIN ai_chat_model_versions v ON v.model_id=m.id AND v.version=m.version WHERE m.enabled ORDER BY m.created_at`)
	if e != nil {
		replyError(w, r, e)
		return
	}
	items, e := jsonRows(rows)
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items})
}
