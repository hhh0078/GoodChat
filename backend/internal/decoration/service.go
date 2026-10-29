// Package decoration 实现租户装修草稿、隔离发布、模板和持久AI设计任务。
package decoration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// Service 保存装修数据库、鉴权及服务端模型配置。
type Service struct {
	pool             *pgxpool.Pool
	auth             platform.Authorizer
	cfg              platform.Config
	client           *http.Client
	featureChecker   func(context.Context, pgx.Tx, string, string) error
	packageSlots     chan struct{}
	assistantClient  *http.Client
	assistantCancels sync.Map
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// New 创建装修模块，并拒绝代码站点与可信站点同源或生产同站配置。
func New(pool *pgxpool.Pool, auth platform.Authorizer, cfg platform.Config) (*Service, error) {
	if err := validateOrigins(cfg.PublicURL, cfg.CodeOrigin, cfg.Production); err != nil {
		return nil, err
	}
	return &Service{pool: pool, auth: auth, cfg: cfg, client: &http.Client{Timeout: 100 * time.Second, CheckRedirect: rejectRedirect}, packageSlots: make(chan struct{}, 2)}, nil
}

// SetFeatureChecker 注入套餐权益校验，调用方在同一租户事务内执行。
func (s *Service) SetFeatureChecker(checker func(context.Context, pgx.Tx, string, string) error) {
	s.featureChecker = checker
}

// checkFeature 使用已装配的套餐规则检查装修操作。
func (s *Service) checkFeature(ctx context.Context, tx pgx.Tx, tenant, feature string) error {
	if s.featureChecker != nil {
		return s.featureChecker(ctx, tx, tenant, feature)
	}
	return nil
}

// Register 注册租户与平台管理接口，所有写操作要求管理员身份。
func (s *Service) Register(mux *http.ServeMux) {
	s.registerAssistant(mux)
	s.registerPackages(mux)
	s.registerTemplateLifecycle(mux)
	s.registerPlatformTemplatePackages(mux)
	mux.HandleFunc("GET /api/v1/tenant/templates", s.auth.RequireTenant(s.listTemplates))
	mux.HandleFunc("POST /api/v1/tenant/templates", s.auth.RequireAdmin(s.saveTemplate))
	mux.HandleFunc("PATCH /api/v1/tenant/templates/{id}", s.auth.RequireAdmin(s.saveTemplate))
	mux.HandleFunc("POST /api/v1/tenant/templates/{id}/duplicate", s.auth.RequireAdmin(s.duplicateTemplate))
	mux.HandleFunc("GET /api/v1/tenant/pages", s.auth.RequireTenant(s.listPages))
	mux.HandleFunc("POST /api/v1/tenant/pages", s.auth.RequireAdmin(s.createPage))
	mux.HandleFunc("GET /api/v1/tenant/pages/{id}", s.auth.RequireTenant(s.getPage))
	mux.HandleFunc("PATCH /api/v1/tenant/pages/{id}", s.auth.RequireAdmin(s.savePage))
	mux.HandleFunc("POST /api/v1/tenant/pages/{id}/preview", s.auth.RequireAdmin(s.previewPage))
	mux.HandleFunc("POST /api/v1/tenant/pages/{id}/publish", s.auth.RequireAdmin(s.publishPage))
	mux.HandleFunc("GET /api/v1/tenant/pages/{id}/releases", s.auth.RequireTenant(s.listReleases))
	mux.HandleFunc("POST /api/v1/tenant/pages/{id}/rollback", s.auth.RequireAdmin(s.rollbackPage))
	mux.HandleFunc("GET /api/v1/platform/templates", s.auth.RequirePlatform(s.platformTemplates))
	mux.HandleFunc("POST /api/v1/platform/templates", s.auth.RequirePlatform(s.platformTemplates))
	mux.HandleFunc("PATCH /api/v1/platform/templates/{id}", s.auth.RequirePlatform(s.platformTemplates))
	mux.HandleFunc("GET /api/v1/tenant/ai-configuration", s.auth.RequireTenant(s.aiConfiguration))
	mux.HandleFunc("POST /api/v1/tenant/ai-design-jobs", s.auth.RequireAdmin(s.createAIJob))
	mux.HandleFunc("GET /api/v1/tenant/ai-design-jobs", s.auth.RequireTenant(s.listAIJobs))
	mux.HandleFunc("GET /api/v1/tenant/ai-design-jobs/{id}", s.auth.RequireTenant(s.getAIJob))
	mux.HandleFunc("POST /api/v1/tenant/ai-design-jobs/{id}/cancel", s.auth.RequireAdmin(s.cancelAIJob))
}

// tenant 在认证租户范围执行事务并统一处理错误。
func (s *Service) tenant(w http.ResponseWriter, r *http.Request, action func(pgx.Tx) (any, error)) {
	var result any
	p := platform.Current(r)
	err := platform.TenantTx(r.Context(), s.pool, p.TenantID, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(r.Context(), "SELECT status='active' FROM tenants WHERE id=$1", p.TenantID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return platform.Fail(403, "tenant_disabled", "当前租户已停用")
		}
		var err error
		result, err = action(tx)
		return err
	})
	respond(w, r, result, err)
}

// respond 将资源不存在转换为稳定错误，并输出统一响应。
func respond(w http.ResponseWriter, r *http.Request, result any, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		err = platform.Fail(404, "not_found", "没有找到当前租户的这项资源")
	}
	if err != nil {
		platform.ErrorJSON(w, r, err)
		return
	}
	platform.JSON(w, http.StatusOK, result)
}

// jsonRows 读取JSON对象查询结果，空集合保持空数组。
func jsonRows(rows pgx.Rows) ([]json.RawMessage, error) {
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var item json.RawMessage
		if err := rows.Scan(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// record 读取单行JSON对象，保留字段的JSON类型。
func record(ctx context.Context, tx pgx.Tx, query string, args ...any) (json.RawMessage, error) {
	var item json.RawMessage
	err := tx.QueryRow(ctx, query, args...).Scan(&item)
	return item, err
}

// idValue 校验公开资源编号，拒绝数据库类型转换错误。
func idValue(value string) error {
	if !uuidPattern.MatchString(value) {
		return platform.Fail(400, "invalid_id", "资源编号格式不正确")
	}
	return nil
}

// newID 生成安全随机UUID，不依赖额外第三方库。
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// digest 计算设计、令牌与幂等请求的SHA256摘要。
func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

// conflict 返回不会覆盖新草稿或新线上版本的并发冲突。
func conflict() error {
	return platform.Fail(409, "version_conflict", "内容已被其他操作更新，请保留当前编辑并刷新后重试")
}

// validName 校验名称并限制长度，避免空白资源进入列表。
func validName(name string) error {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 100 {
		return platform.Fail(400, "invalid_name", "名称不能为空且不能超过100个字")
	}
	return nil
}

// validateOrigins 检查独立渲染来源，生产保守拒绝共享末两段域名的部署。
func validateOrigins(publicOrigin, codeOrigin string, production bool) error {
	p, err := url.Parse(publicOrigin)
	if err != nil || p.Host == "" || p.User != nil {
		return errors.New("可信站点地址配置无效")
	}
	c, err := url.Parse(codeOrigin)
	if err != nil || c.Host == "" || c.User != nil || c.RawQuery != "" || c.Fragment != "" || strings.Trim(c.Path, "/") != "" {
		return errors.New("独立代码站点地址配置无效")
	}
	if hostIsIP(c.Hostname()) || strings.Contains(c.Hostname(), "*") {
		return errors.New("代码站点必须使用可分配版本子域的域名，开发可使用localhost")
	}
	if (p.Scheme != "http" && p.Scheme != "https") || (c.Scheme != "http" && c.Scheme != "https") {
		return errors.New("装修站点必须使用HTTP或HTTPS")
	}
	if strings.EqualFold(p.Host, c.Host) && p.Scheme == c.Scheme {
		return errors.New("装修代码站点必须与可信站点跨来源")
	}
	if production {
		ph, ch := strings.TrimSuffix(strings.ToLower(p.Hostname()), "."), strings.TrimSuffix(strings.ToLower(c.Hostname()), ".")
		a, b := strings.Split(ph, "."), strings.Split(ch, ".")
		if c.Scheme != "https" || p.Scheme != "https" || len(a) < 2 || len(b) < 2 || hostIsIP(ph) || hostIsIP(ch) || strings.HasSuffix(ch, "localhost") || strings.HasSuffix(ph, "localhost") || strings.Join(a[len(a)-2:], ".") == strings.Join(b[len(b)-2:], ".") {
			return errors.New("生产装修代码站点必须使用HTTPS与独立可注册域名")
		}
	}
	return nil
}

// rejectRedirect 阻止模型供应商重定向把服务端鉴权发送给另一个地址。
func rejectRedirect(request *http.Request, previous []*http.Request) error {
	return http.ErrUseLastResponse
}

// renderURL 为每个静态快照分配独立子域来源，避免租户之间共享浏览器存储。
func (s *Service) renderURL(id, token string) string {
	u, _ := url.Parse(s.cfg.CodeOrigin)
	host := id + "." + u.Hostname()
	if u.Port() != "" {
		host += ":" + u.Port()
	}
	u.Host = host
	u.Path = "/render/" + id
	if token != "" {
		q := url.Values{}
		q.Set("preview", token)
		u.RawQuery = q.Encode()
	}
	return u.String()
}
