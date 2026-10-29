// 文件作用说明：只创建带明确演示前缀的新租户、账号、Cookie及访客夹具，无邮件、无现有业务资源修改。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type account struct {
	UserID   string `json:"user_id"`
	MemberID string `json:"member_id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Cookie   string `json:"cookie"`
}
type visitor struct {
	ContactID      string `json:"contact_id"`
	ConversationID string `json:"conversation_id"`
	Token          string `json:"token"`
	Agent          int    `json:"agent"`
}
type fixture struct {
	TenantID    string                    `json:"tenant_id"`
	Name        string                    `json:"name"`
	Base        string                    `json:"base_url"`
	Created     string                    `json:"created_at"`
	PlanID      string                    `json:"plan_id"`
	Accounts    []account                 `json:"accounts"`
	Visitors    []visitor                 `json:"visitors"`
	Links       map[string]map[string]any `json:"links"`
	PoolID      string                    `json:"pool_id"`
	FixturePath string                    `json:"fixture_path"`
}
type databaseConfig struct {
	Database  string `json:"database_url"`
	Migration string `json:"migration_url"`
}

// database 只接受显式本机goodchat_saas配置；连接错误由上层脱敏，不修改数据库角色或密码。
func database(ctx context.Context, path string) (*pgx.Conn, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var c databaseConfig
	if e = json.Unmarshal(b, &c); e != nil {
		return nil, e
	}
	p, e := pgx.ParseConfig(c.Migration)
	if e != nil {
		return nil, e
	}
	if p.Database != "goodchat_saas" || (p.Host != "127.0.0.1" && p.Host != "localhost") {
		return nil, errors.New("local_database_required")
	}
	p.RuntimeParams["application_name"] = "goodchat_realtime_load"
	return pgx.ConnectConfig(ctx, p)
}

// createFixture 用真实数据库创建独立身份，不替代运行时的Cookie鉴权、RLS、票据消费、HTTP消息或WS投递。
func createFixture(ctx context.Context, o options) error {
	u, e := url.Parse(o.base)
	if e != nil {
		return e
	}
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" || o.agents < 1 || o.agents > 8 || o.visitors < 0 || o.visitors > 400 {
		return errors.New("local_fixture_limits")
	}
	if _, e = os.Stat(o.fixture); e == nil {
		return errors.New("fixture_exists")
	}
	if e = os.MkdirAll(filepath.Dir(o.fixture), 0700); e != nil {
		return e
	}
	db, e := database(ctx, o.config)
	if e != nil {
		return e
	}
	defer db.Close(ctx)
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	f := fixture{TenantID: uuid(), Name: "演示数据·实时" + o.label, Base: o.base, Created: time.Now().UTC().Format(time.RFC3339), PlanID: uuid(), Accounts: []account{}, Visitors: []visitor{}, Links: map[string]map[string]any{}, FixturePath: o.fixture}
	if _, e = tx.Exec(ctx, "INSERT INTO tenants(id,name,slug) VALUES($1,$2,$3)", f.TenantID, f.Name, "load-"+f.TenantID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.TenantID); e != nil {
		return e
	}
	code := "load-" + strings.ReplaceAll(f.TenantID, "-", "")[:20]
	if _, e = tx.Exec(ctx, `INSERT INTO plans(id,code,name,description,price_amount,seat_limit,features,is_public) SELECT $1,$2,$3,'仅本次本机压测夹具，无真实付费或承载声明',0,$4,features,false FROM plans WHERE code='free'`, f.PlanID, code, f.Name, o.agents); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO tenant_subscriptions(tenant_id,plan_id,plan_snapshot) SELECT $1,id,to_jsonb(p) FROM plans p WHERE id=$2`, f.TenantID, f.PlanID); e != nil {
		return e
	}
	for i := 0; i < o.agents; i++ {
		a := account{UserID: uuid(), MemberID: uuid(), Role: "agent", Cookie: secret("")}
		if i == 0 {
			a.Role = "owner"
		}
		a.Email = "load-" + a.UserID + "@example.invalid"
		if _, e = tx.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,$2,$3,'load-fixture-password-login-disabled')`, a.UserID, a.Email, fmt.Sprintf("演示数据·%s·客服%d", o.label, i)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO tenant_memberships(id,tenant_id,user_id,role,can_serve,capacity) VALUES($1,$2,$3,$4,true,100)`, a.MemberID, f.TenantID, a.UserID, a.Role); e != nil {
			return e
		}
		sum := sha256.Sum256([]byte(a.Cookie))
		if _, e = tx.Exec(ctx, `INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, a.UserID, sum[:]); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `SELECT ensure_chat_defaults($1::uuid,$2::uuid)`, f.TenantID, a.UserID); e != nil {
			return e
		}
		f.Accounts = append(f.Accounts, a)
	}
	var link map[string]any
	var raw []byte
	if e = tx.QueryRow(ctx, `UPDATE access_links SET name=$2,reception_mode='manual',agent_mode='strict' WHERE tenant_id=$1 AND is_default RETURNING to_jsonb(access_links)`, f.TenantID, f.Name+"入口").Scan(&raw); e != nil {
		return e
	}
	if e = json.Unmarshal(raw, &link); e != nil {
		return e
	}
	f.Links["load"] = link
	f.PoolID = link["routing_pool_id"].(string)
	if _, e = tx.Exec(ctx, `UPDATE routing_pools SET algorithm='manual' WHERE tenant_id=$1`, f.TenantID); e != nil {
		return e
	}
	for i := 0; i < o.visitors; i++ {
		v := visitor{ContactID: uuid(), ConversationID: uuid(), Token: secret("v_"), Agent: i % o.agents}
		if _, e = tx.Exec(ctx, `INSERT INTO contacts(id,tenant_id,display_name) VALUES($1,$2,$3)`, v.ContactID, f.TenantID, fmt.Sprintf("演示数据·压测访客%03d", i)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO visitor_sessions(tenant_id,contact_id,access_link_id,token_hash) VALUES($1,$2,$3,$4)`, f.TenantID, v.ContactID, link["id"], digestHex(v.Token)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO conversations(id,tenant_id,service_space_id,contact_id,created_entry_id,service_group_id,routing_pool_id,desired_agent_id,agent_mode) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'strict')`, v.ConversationID, f.TenantID, link["service_space_id"], v.ContactID, link["id"], link["service_group_id"], f.PoolID, f.Accounts[v.Agent].MemberID); e != nil {
			return e
		}
		f.Visitors = append(f.Visitors, v)
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if e = writeJSON(o.fixture, f, true); e != nil {
		return e
	}
	state := map[string]any{"cookies": []any{map[string]any{"name": "support_session", "value": f.Accounts[0].Cookie, "domain": "127.0.0.1", "path": "/", "expires": -1, "httpOnly": true, "secure": false, "sameSite": "Lax"}}, "origins": []any{map[string]any{"origin": o.base, "localStorage": []any{map[string]any{"name": "goodchat.activeTenant", "value": f.TenantID}}}}}
	if e = writeJSON(filepath.Join(filepath.Dir(o.fixture), "owner-auth-state.json"), state, true); e != nil {
		return e
	}
	fmt.Printf("fixture_ready accounts=%d visitors=%d no_mail=true\n", len(f.Accounts), len(f.Visitors))
	return nil
}
