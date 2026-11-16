// Package platform 提供服务模块共享的请求响应、错误和租户事务工具。
package platform

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config 保存各业务模块共用的运行配置，密钥从环境读取。
type Config struct {
	Address            string
	DatabaseURL        string
	MigrationURL       string
	PublicURL          string
	DataDir            string
	Production         bool
	SMTPHost           string
	SMTPPort           string
	SMTPUser           string
	SMTPPassword       string
	SMTPFrom           string
	SMTPAllowPlain     bool
	AIEndpoint         string
	AIKey              string
	AIModel            string
	AIDailyLimit       int // 每租户每自然日的AI提交上限，零表示禁用。
	CodeOrigin         string
	AuthSecret         string
	CallbackAllowLocal bool
	RedisURL           string
	GatewaySecret      string
}

// Principal 表示经过验证的账号及当前租户成员身份。
type Principal struct {
	UserID        string `json:"user_id"`
	Email         string `json:"email"`
	DisplayName   string `json:"display_name"`
	TenantID      string `json:"tenant_id"`
	MembershipID  string `json:"membership_id"`
	TenantName    string `json:"tenant_name"`
	Role          string `json:"role"`
	CanServe      bool   `json:"can_serve"`
	PlatformAdmin bool   `json:"is_platform_admin"`
}

type principalKey struct{}

// Authorizer 定义模块共享的账号租户和平台权限校验。
type Authorizer interface {
	RequireUser(http.HandlerFunc) http.HandlerFunc
	RequireTenant(http.HandlerFunc) http.HandlerFunc
	RequireAdmin(http.HandlerFunc) http.HandlerFunc
	RequirePlatform(http.HandlerFunc) http.HandlerFunc
}

// WithPrincipal 为请求上下文保存已验证身份。
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// Current 读取请求身份；未认证时返回零值。
func Current(r *http.Request) Principal {
	principal, _ := r.Context().Value(principalKey{}).(Principal)
	return principal
}

// APIError 保存可以安全展示给用户的业务错误。
type APIError struct {
	Status  int
	Code    string
	Message string
}

// Error 返回业务错误文本。
func (e *APIError) Error() string { return e.Message }

// Fail 创建携带HTTP状态的业务错误。
func Fail(status int, code, message string) error {
	return &APIError{Status: status, Code: code, Message: message}
}

// JSON 返回统一成功结构；data为可序列化业务结果。
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// ErrorJSON 输出安全错误，隐藏数据库及内部密钥细节。
func ErrorJSON(w http.ResponseWriter, r *http.Request, err error) {
	apiError := &APIError{Status: 500, Code: "internal_error", Message: "暂时没有处理成功，请稍后重试"}
	var typed *APIError
	if errors.As(err, &typed) {
		apiError = typed
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(apiError.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": apiError.Code, "message": apiError.Message, "request_id": r.Header.Get("X-Request-ID")})
}

// Decode 严格读取最大1MiB的JSON请求，拒绝未知字段及多份JSON。
func Decode(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return Fail(400, "invalid_request", "请检查填写内容和数据格式")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Fail(400, "invalid_request", "请求内容格式不正确")
	}
	return nil
}

// TenantTx 在事务内设置租户上下文并执行操作，连接返回池前自动清除上下文。
func TenantTx(ctx context.Context, pool *pgxpool.Pool, tenantID string, action func(pgx.Tx) error) error {
	if tenantID == "" {
		return Fail(403, "tenant_required", "请先选择工作租户")
	}
	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
			return err
		}
		return action(tx)
	})
}

// Token 返回安全随机且可用于URL的令牌，失败时返回错误。
func Token() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
