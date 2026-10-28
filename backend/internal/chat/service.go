// Package chat 提供租户客服接入、客户资料、会话分配与可靠消息服务。
package chat

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/platform"
)

// Service 保存聊天模块依赖及本进程实时连接。
type Service struct {
	pool            *pgxpool.Pool
	auth            platform.Authorizer
	cfg             platform.Config
	mu              sync.Mutex
	clients         map[*liveClient]struct{}
	cursorKey       []byte
	visitorHook     VisitorEnteredHook
	entryDecoration func(context.Context, pgx.Tx, string, string) (map[string]any, error)
	featureChecker  func(context.Context, pgx.Tx, string, string) error
	leadNotifier    func(context.Context, string, string, string) (string, string)
}

// VisitorEnteredHook 允许回调模块在访问事务内保存可靠通知任务，不在该事务内发送外部请求。
type VisitorEnteredHook func(context.Context, pgx.Tx, string, string, map[string]any) error

// SetVisitorEnteredHook 注册访问入队钩子，应在HTTP服务启动前设置。
func (s *Service) SetVisitorEnteredHook(hook VisitorEnteredHook) { s.visitorHook = hook }

// SetEntryDecoration 注入入口装修的已发布快照读取器，不向访客暴露草稿。
func (s *Service) SetEntryDecoration(reader func(context.Context, pgx.Tx, string, string) (map[string]any, error)) {
	s.entryDecoration = reader
}

// actor 表示独立访客或已经鉴权的租户成员。
type actor struct {
	TenantID, ContactID, MemberID, UserID, Role string
	CanServe                                    bool
}
type actorKey struct{}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// New 创建客服服务，不启动独立数据库或修改其他模块配置。
func New(pool *pgxpool.Pool, auth platform.Authorizer, cfg platform.Config) *Service {
	// 使用运行配置的稳定密钥派生独立游标密钥，使重启后仍可恢复；不得复用认证签名用途。
	key := sha256.Sum256([]byte("goodchat:event-cursor:v1:" + cfg.AuthSecret))
	if cfg.AuthSecret == "" {
		// 隔离测试等无认证配置场景仍保持不可预测；正常运行由LoadConfig提供私有稳定密钥。
		if _, err := rand.Read(key[:]); err != nil {
			panic("无法初始化安全游标密钥")
		}
	}
	return &Service{pool: pool, auth: auth, cfg: cfg, clients: map[*liveClient]struct{}{}, cursorKey: key[:]}
}

// Register 注册真实持久化资源、访客、会话及实时接口。
func (s *Service) Register(mux *http.ServeMux) {
	s.registerOperations(mux)
	s.registerAutomationRich(mux)
	s.registerAsyncExports(mux)
	s.registerAdvancedRouting(mux)
	for _, name := range []string{"groups", "routing-skills", "routing-pools", "access-links", "contacts", "tags", "categories", "tag-categories", "custom-fields"} {
		resource := name
		mux.HandleFunc("GET /api/v1/tenant/"+resource, s.auth.RequireTenant(s.resources(resource)))
		mux.HandleFunc("POST /api/v1/tenant/"+resource, s.auth.RequireAdmin(s.resources(resource)))
		mux.HandleFunc("PATCH /api/v1/tenant/"+resource+"/{id}", s.auth.RequireAdmin(s.resources(resource)))
		mux.HandleFunc("DELETE /api/v1/tenant/"+resource+"/{id}", s.auth.RequireAdmin(s.resources(resource)))
	}
	mux.HandleFunc("GET /api/v1/entries/{entry_key}", s.entry)
	mux.HandleFunc("POST /api/v1/visitor/session", s.visitorSession)
	mux.HandleFunc("GET /api/v1/agent/status", s.auth.RequireTenant(s.agentStatus))
	mux.HandleFunc("GET /api/v1/agent/colleagues", s.auth.RequireTenant(s.colleagues))
	mux.HandleFunc("PATCH /api/v1/agent/status", s.auth.RequireTenant(s.agentStatus))
	mux.HandleFunc("POST /api/v1/tenant/contacts/{id}/block", s.auth.RequireAdmin(s.blockContact))
	mux.HandleFunc("DELETE /api/v1/tenant/contacts/{id}/block", s.auth.RequireAdmin(s.blockContact))
	mux.HandleFunc("GET /api/v1/tenant/contacts/{id}/visits", s.auth.RequireTenant(s.contactVisits))
	mux.HandleFunc("GET /api/v1/tenant/contacts/{id}/notes", s.auth.RequireTenant(s.contactNotes))
	mux.HandleFunc("GET /api/v1/tenant/contacts/{id}/notes/{note_id}/attachment", s.auth.RequireTenant(s.contactNoteAttachment))
	mux.HandleFunc("POST /api/v1/tenant/contacts/{id}/notes", s.auth.RequireTenant(s.contactNotes))
	mux.HandleFunc("GET /api/v1/tenant/contacts/{id}", s.auth.RequireTenant(s.contactDetail))
	mux.HandleFunc("GET /api/v1/tenant/chat-records", s.auth.RequireTenant(s.chatRecords))
	mux.HandleFunc("GET /api/v1/tenant/chat-records/export", s.auth.RequireAdmin(s.exportRecords))
	mux.HandleFunc("GET /api/v1/conversations", s.withActor(s.conversations))
	mux.HandleFunc("POST /api/v1/conversations", s.withActor(s.conversations))
	mux.HandleFunc("GET /api/v1/conversations/{id}", s.withActor(s.conversationDetail))
	mux.HandleFunc("GET /api/v1/conversations/{id}/messages", s.withActor(s.messages))
	mux.HandleFunc("POST /api/v1/conversations/{id}/messages", s.withActor(s.messages))
	mux.HandleFunc("POST /api/v1/conversations/{id}/messages/{message_id}/withdraw", s.withActor(s.withdraw))
	mux.HandleFunc("GET /api/v1/conversations/{id}/events", s.withActor(s.events))
	mux.HandleFunc("POST /api/v1/conversations/{id}/read-cursor", s.withActor(s.readCursor))
	mux.HandleFunc("POST /api/v1/conversations/{id}/typing", s.withActor(s.typing))
	for _, name := range []string{"claim", "accept", "reject", "transfer", "close"} {
		mux.HandleFunc("POST /api/v1/conversations/{id}/assignments/"+name, s.withActor(s.assignmentAction(name)))
	}
	mux.HandleFunc("POST /api/v1/conversations/{id}/close", s.withActor(s.assignmentAction("close")))
	mux.HandleFunc("POST /api/v1/realtime/ticket", s.withActor(s.realtimeTicket))
	mux.HandleFunc("GET /ws/v1", s.websocket)
	mux.HandleFunc("POST /api/v1/attachments", s.withActor(s.upload))
	mux.HandleFunc("GET /api/v1/attachments/{id}", s.withActor(s.download))
}

// EnsureDefaults 确保租户拥有默认服务空间、业务组、分配池与公开链接。
func (s *Service) EnsureDefaults(ctx context.Context, tenantID string) error {
	return platform.TenantTx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT ensure_chat_defaults($1::uuid)", tenantID)
		return err
	})
}

// withActor 将账号鉴权与独立访客凭证解析合并，仍保留不同权限边界。
func (s *Service) withActor(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if strings.HasPrefix(token, "v_") {
			a, err := s.visitorActor(r.Context(), token)
			if err != nil {
				replyError(w, r, err)
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
			return
		}
		s.auth.RequireTenant(func(w http.ResponseWriter, r *http.Request) {
			p := platform.Current(r)
			a := actor{TenantID: p.TenantID, MemberID: p.MembershipID, UserID: p.UserID, Role: p.Role, CanServe: p.CanServe}
			next(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
		})(w, r)
	}
}

// visitorActor 仅用高熵凭证摘要恢复访客，不接受手机号或展示用户编号。
func (s *Service) visitorActor(ctx context.Context, token string) (actor, error) {
	var a actor
	var link string
	err := s.pool.QueryRow(ctx, "SELECT tenant_id::text,contact_id::text,access_link_id::text FROM chat_visitor_identity($1)", hash(token)).Scan(&a.TenantID, &a.ContactID, &link)
	if err != nil {
		return a, platform.Fail(401, "visitor_session_expired", "访客身份已过期，请重新进入")
	}
	return a, nil
}

// currentActor 读取本次请求已经验证的参与者。
func currentActor(r *http.Request) actor { a, _ := r.Context().Value(actorKey{}).(actor); return a }

// memberActor 从平台认证上下文建立成员参与者。
func memberActor(r *http.Request) actor {
	p := platform.Current(r)
	return actor{TenantID: p.TenantID, MemberID: p.MembershipID, UserID: p.UserID, Role: p.Role, CanServe: p.CanServe}
}

// actorID 返回当前身份的稳定参与者编号。
func actorID(a actor) string {
	if a.ContactID != "" {
		return a.ContactID
	}
	return a.MemberID
}

// actorKind 返回访客或客服类型。
func actorKind(a actor) string {
	if a.ContactID != "" {
		return "visitor"
	}
	return "agent"
}

// isAdmin 判断租户管理权限，平台身份不能代替租户授权。
func isAdmin(a actor) bool { return a.Role == "owner" || a.Role == "admin" }

// newID 使用安全随机数生成版本四UUID。
func newID() string {
	v := make([]byte, 16)
	if _, err := rand.Read(v); err != nil {
		panic("无法生成安全编号")
	}
	v[6] = (v[6] & 15) | 64
	v[8] = (v[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", v[0:4], v[4:6], v[6:8], v[8:10], v[10:16])
}

// hash 生成不泄露原凭证的SHA256摘要。
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// jsonValue 将受控结构转换为数据库JSON参数。
func jsonValue(value any) []byte { b, _ := json.Marshal(value); return b }

// replyError 把数据库约束错误转换为可操作的业务提示。
func replyError(w http.ResponseWriter, r *http.Request, err error) {
	var p *pgconn.PgError
	if errors.As(err, &p) {
		switch p.Code {
		case "23505":
			err = platform.Fail(409, "resource_conflict", "记录已存在或已被其他操作更新")
		case "23503", "22P02", "23514", "23502":
			err = platform.Fail(400, "invalid_fields", "请检查关联资源、字段格式和允许范围")
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = platform.Fail(404, "not_found", "记录不存在或无权访问")
	}
	platform.ErrorJSON(w, r, err)
}

// jsonRows 读取单列JSON查询并确保数据库结果集关闭。
func jsonRows(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// txJSON 读取单条JSON投影。
func txJSON(ctx context.Context, tx pgx.Tx, query string, args ...any) (map[string]any, error) {
	var b []byte
	if err := tx.QueryRow(ctx, query, args...).Scan(&b); err != nil {
		return nil, err
	}
	var v map[string]any
	err := json.Unmarshal(b, &v)
	return v, err
}

// blocked 检查有效封禁，历史数据不因封禁删除。
func blocked(ctx context.Context, tx pgx.Tx, contact string) error {
	var yes bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM block_rules WHERE contact_id=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now()))", contact).Scan(&yes)
	if err != nil {
		return err
	}
	if yes {
		return platform.Fail(403, "contact_blocked", "暂时无法提供服务，请联系商户")
	}
	return nil
}

// liveMember 在持久连接和事务中重新验证成员与租户状态。
func liveMember(ctx context.Context, tx pgx.Tx, a actor) error {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_memberships m JOIN tenants t ON t.id=m.tenant_id WHERE m.id=$1 AND m.tenant_id=$2 AND NOT m.disabled AND m.removed_at IS NULL AND m.role=$3 AND m.can_serve=$4 AND t.status='active')", a.MemberID, a.TenantID, a.Role, a.CanServe).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return platform.Fail(403, "membership_revoked", "当前成员权限已失效")
	}
	return nil
}

// tenantActive 检查租户当前是否接受业务操作。
func tenantActive(ctx context.Context, tx pgx.Tx, tenant string) error {
	var status string
	if err := tx.QueryRow(ctx, "SELECT status FROM tenants WHERE id=$1", tenant).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return platform.Fail(403, "tenant_inactive", "商户服务当前不可用")
	}
	return nil
}

// nowUTC 返回统一服务端事件时间。
func nowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }
