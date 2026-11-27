// 文件作用说明：保存成员的独立机器人、授权目录、业务时段与租户默认接待策略。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// aiBotSettings 给自动回复设置明确的输出、轮数、时段及每日人民币微元预算。
type aiBotSettings struct {
	Offline     bool   `json:"offline"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Weekdays    []int  `json:"weekdays"`
	DailyBudget int64  `json:"daily_budget_micro"`
	MaxOutput   int    `json:"max_output_tokens"`
	MaxTurns    int    `json:"max_turns"`
}

// aiBot 保存属于当前租户成员的接待设置和明确授权资源编号。
type aiBot struct {
	AvatarRef    string        `json:"avatar_ref"`
	ID           string        `json:"id"`
	Owner        string        `json:"owner_membership_id"`
	ModelID      string        `json:"model_id"`
	Name         string        `json:"name"`
	Mode         string        `json:"mode"`
	Prompt       string        `json:"prompt"`
	Language     string        `json:"language"`
	KnowledgeIDs []string      `json:"knowledge_ids"`
	ScriptIDs    []string      `json:"script_ids"`
	Settings     aiBotSettings `json:"settings"`
	Version      int64         `json:"version"`
}

// defaultAIBot 给首次进入设置的客服返回关闭状态，保存并不会自动开启接待。
func defaultAIBot() aiBot {
	return aiBot{Name: "AI", AvatarRef: "goodchat", Mode: "off", Language: "auto", KnowledgeIDs: []string{}, ScriptIDs: []string{}, Settings: aiBotSettings{Offline: true, Start: "00:00", End: "23:59", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, DailyBudget: 0, MaxOutput: 1024, MaxTurns: 8}}
}

// memberAIBotID 校验明确选择的本人机器人；旧调用优先租户默认配置，再按创建时间与编号稳定选择原配置。
func memberAIBotID(ctx context.Context, tx pgx.Tx, member, selected string) (string, error) {
	if selected != "" && !uuidPattern.MatchString(selected) {
		return "", platform.Fail(400, "ai_bot_invalid", "机器人编号不正确")
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT b.id::text FROM ai_bots b WHERE b.owner_membership_id=$1 AND ($2='' OR b.id=NULLIF($2,'')::uuid) ORDER BY CASE WHEN b.id=(SELECT default_bot_id FROM ai_tenant_settings) THEN 0 ELSE 1 END,b.created_at,b.id LIMIT 1`, member, selected).Scan(&id)
	if err == pgx.ErrNoRows && selected != "" {
		return "", platform.Fail(404, "ai_bot_not_found", "机器人不存在或不属于当前成员")
	}
	return id, err
}

// insertMemberAIBot 只按认证成员创建独立配置，编号由服务端生成，不修改租户默认接待或入口绑定。
func insertMemberAIBot(ctx context.Context, tx pgx.Tx, a actor, b aiBot) (string, error) {
	id := newID()
	_, err := tx.Exec(ctx, `INSERT INTO ai_bots(id,tenant_id,owner_membership_id,model_id,name,mode,prompt,language,knowledge_ids,script_ids,settings,avatar_ref) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12)`, id, a.TenantID, a.MemberID, b.ModelID, b.Name, b.Mode, b.Prompt, b.Language, b.KnowledgeIDs, b.ScriptIDs, jsonValue(b.Settings), b.AvatarRef)
	return id, err
}

// aiCreateBot 为当前有效成员新增关闭状态的机器人，所有授权、模型与接待设置均从安全默认值开始。
func (s *Service) aiCreateBot(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var in struct {
		Name string `json:"name"`
	}
	if err := platform.Decode(w, r, &in); err != nil {
		replyError(w, r, err)
		return
	}
	b := defaultAIBot()
	b.Name = strings.TrimSpace(in.Name)
	if err := validateAIBot(b); err != nil {
		replyError(w, r, err)
		return
	}
	if strings.ContainsAny(b.Name, "\x00\r\n") {
		returnError(w, r, "ai_bot_invalid", "请检查机器人设置")
		return
	}
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		// 与首次保存共享成员锁，锁后重查权限，停用成员不能在等待期间继续创建。
		if _, err := tx.Exec(ctx, "SELECT id FROM tenant_memberships WHERE id=$1 AND tenant_id=$2 FOR UPDATE", a.MemberID, a.TenantID); err != nil {
			return err
		}
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		id, err := insertMemberAIBot(ctx, tx, a, b)
		if err != nil {
			return err
		}
		result, err = txJSON(ctx, tx, "SELECT to_jsonb(b) FROM ai_bots b WHERE id=$1", id)
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 201, result)
}

// aiBot 仅从认证上下文推导机器人归属，不允许管理员借此修改别人的个人设置。
func (s *Service) aiBot(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	in := defaultAIBot()
	if r.Method == "PUT" {
		if e := platform.Decode(w, r, &in); e != nil {
			replyError(w, r, e)
			return
		}
		if in.AvatarRef == "" {
			in.AvatarRef = "goodchat"
		}
		if e := validateAIBot(in); e != nil {
			replyError(w, r, e)
			return
		}
	}
	var result any
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := liveMember(ctx, tx, a); e != nil {
			return e
		}
		if r.Method == "GET" {
			id, e := memberAIBotID(ctx, tx, a.MemberID, r.URL.Query().Get("bot_id"))
			if e == pgx.ErrNoRows {
				result = in
				return nil
			}
			if e != nil {
				return e
			}
			result, e = txJSON(ctx, tx, "SELECT to_jsonb(b) FROM ai_bots b WHERE id=$1", id)
			return e
		}
		// 成员行锁统一首次创建和后续保存，防止并发首次创建绕过版本检查。
		if _, e := tx.Exec(ctx, "SELECT id FROM tenant_memberships WHERE id=$1 FOR UPDATE", a.MemberID); e != nil {
			return e
		}
		id, e := memberAIBotID(ctx, tx, a.MemberID, in.ID)
		if e != nil && e != pgx.ErrNoRows {
			return e
		}
		exists := e == nil
		var old int64
		if exists {
			if e = tx.QueryRow(ctx, "SELECT version FROM ai_bots WHERE id=$1", id).Scan(&old); e != nil {
				return e
			}
		}
		if old != in.Version {
			return platform.Fail(409, "ai_version_conflict", "内容已更新，请刷新后重试")
		}
		if in.Mode != "off" && !a.CanServe {
			return platform.Fail(403, "cannot_serve", "当前成员没有接待权限")
		}
		if e := validateAIBindings(ctx, tx, in); e != nil {
			return e
		}
		if exists {
			_, e = tx.Exec(ctx, `UPDATE ai_bots SET model_id=NULLIF($3,'')::uuid,name=$4,mode=$5,prompt=$6,language=$7,knowledge_ids=$8,script_ids=$9,settings=$10,avatar_ref=$11,version=version+1,updated_at=now() WHERE id=$1 AND owner_membership_id=$2`, id, a.MemberID, in.ModelID, in.Name, in.Mode, in.Prompt, in.Language, in.KnowledgeIDs, in.ScriptIDs, jsonValue(in.Settings), in.AvatarRef)
		} else {
			id, e = insertMemberAIBot(ctx, tx, a, in)
		}
		if e != nil {
			return e
		}
		result, e = txJSON(ctx, tx, "SELECT to_jsonb(b) FROM ai_bots b WHERE id=$1", id)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, result)
}

// validateAIBot 拒绝无效时段、语言、目录数量及超额预算配置。
func validateAIBot(b aiBot) error {
	if (!strings.HasPrefix(b.AvatarRef, "default-") && b.AvatarRef != "goodchat") || platform.AvatarURL(b.AvatarRef) == "" || strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 60 || len([]byte(b.Prompt)) > 4000 || !inStrings(b.Mode, "off", "assistant", "auto") || !aiLanguage(b.Language, true) || b.ModelID != "" && !uuidPattern.MatchString(b.ModelID) {
		return platform.Fail(400, "ai_bot_invalid", "请检查机器人设置")
	}
	st := b.Settings
	if _, e := time.Parse("15:04", st.Start); e != nil {
		return platform.Fail(400, "ai_bot_invalid", "请检查机器人设置")
	}
	if _, e := time.Parse("15:04", st.End); e != nil {
		return platform.Fail(400, "ai_bot_invalid", "请检查机器人设置")
	}
	if len(st.Weekdays) == 0 || len(st.Weekdays) > 7 || st.DailyBudget < 0 || st.DailyBudget > 1000000000000 || st.MaxOutput < 128 || st.MaxOutput > 4096 || st.MaxTurns < 1 || st.MaxTurns > 50 || len(b.KnowledgeIDs) > 30 || len(b.ScriptIDs) > 50 {
		return platform.Fail(400, "ai_bot_invalid", "请检查机器人设置")
	}
	for _, d := range st.Weekdays {
		if d < 0 || d > 6 {
			return platform.Fail(400, "ai_bot_invalid", "请检查机器人设置")
		}
	}
	for _, ids := range [][]string{b.KnowledgeIDs, b.ScriptIDs} {
		seen := map[string]bool{}
		for _, id := range ids {
			if !uuidPattern.MatchString(id) || seen[id] {
				return platform.Fail(400, "ai_bindings_invalid", "请检查勾选的知识库和话术")
			}
			seen[id] = true
		}
	}
	return nil
}

// validateAIBindings 在绑定时检查租户范围与目录大小，不静默截断已选资源。
func validateAIBindings(ctx context.Context, tx pgx.Tx, b aiBot) error {
	total := 0
	for _, set := range []struct {
		table string
		ids   []string
	}{{"ai_knowledge_bases", b.KnowledgeIDs}, {"ai_scripts", b.ScriptIDs}} {
		for _, id := range set.ids {
			var size int
			if e := tx.QueryRow(ctx, "SELECT octet_length(title)+octet_length(description)+100 FROM "+set.table+" WHERE id=$1 AND enabled AND NOT archived", id).Scan(&size); e != nil {
				return platform.Fail(400, "ai_bindings_invalid", "请检查勾选的知识库和话术")
			}
			total += size
		}
	}
	if total > 12000 {
		return platform.Fail(400, "ai_catalog_limit", "已选目录过长，请减少勾选数量")
	}
	if b.ModelID != "" {
		var exists bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_chat_models WHERE id=$1 AND enabled)", b.ModelID).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return platform.Fail(400, "ai_model_unavailable", "AI模型暂不可用")
		}
	}
	return nil
}

// aiBots 让管理员查看团队机器人，普通客服只能查看自己的配置。
func (s *Service) aiBots(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var items []map[string]any
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		rows, e := tx.Query(r.Context(), `SELECT to_jsonb(b) FROM ai_bots b WHERE $1 OR owner_membership_id=$2 ORDER BY name`, isAdmin(a), a.MemberID)
		if e != nil {
			return e
		}
		items, e = jsonRows(rows)
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, map[string]any{"items": items})
}

// aiSettings 保存默认接待机器人，要求它当前为自动模式且所属成员有效。
func (s *Service) aiSettings(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var in struct {
		DefaultBot string `json:"default_bot_id"`
		Mode       string `json:"reception_mode"`
	}
	if r.Method == "PUT" {
		if e := platform.Decode(w, r, &in); e != nil {
			replyError(w, r, e)
			return
		}
		if !inStrings(in.Mode, "human_first", "ai_first") || in.DefaultBot != "" && !uuidPattern.MatchString(in.DefaultBot) {
			returnError(w, r, "ai_bot_invalid", "请检查机器人设置")
			return
		}
	}
	var result any
	e := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if r.Method == "PUT" {
			if in.DefaultBot != "" {
				var valid bool
				if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_bots b JOIN tenant_memberships m ON m.id=b.owner_membership_id WHERE b.id=$1 AND b.mode='auto' AND NOT m.disabled AND m.removed_at IS NULL AND m.can_serve)`, in.DefaultBot).Scan(&valid); e != nil {
					return e
				}
				if !valid {
					return platform.Fail(400, "ai_bot_invalid", "请检查机器人设置")
				}
			}
			if _, e := tx.Exec(ctx, `INSERT INTO ai_tenant_settings(tenant_id,default_bot_id,reception_mode) VALUES($1,NULLIF($2,'')::uuid,$3) ON CONFLICT(tenant_id) DO UPDATE SET default_bot_id=excluded.default_bot_id,reception_mode=excluded.reception_mode,updated_at=now()`, a.TenantID, in.DefaultBot, in.Mode); e != nil {
				return e
			}
		}
		var e error
		result, e = txJSON(ctx, tx, "SELECT to_jsonb(s) FROM ai_tenant_settings s")
		if e == pgx.ErrNoRows {
			result = map[string]any{"default_bot_id": "", "reception_mode": "human_first"}
			return nil
		}
		return e
	})
	if e != nil {
		replyError(w, r, e)
		return
	}
	platform.JSON(w, 200, result)
}

// aiWithinHours 按租户业务时区解析跨午夜服务时段。
func aiWithinHours(st aiBotSettings, now time.Time) bool {
	day := int(now.Weekday())
	clock := now.Format("15:04")
	if st.Start > st.End && clock <= st.End {
		day = (day + 6) % 7
	}
	allowed := false
	for _, d := range st.Weekdays {
		allowed = allowed || d == day
	}
	if !allowed {
		return false
	}
	if st.Start <= st.End {
		return clock >= st.Start && clock <= st.End
	}
	return clock >= st.Start || clock <= st.End
}

// loadAIBot 只读取当前租户的机器人并解码明确的配置结构。
func loadAIBot(ctx context.Context, tx pgx.Tx, id string) (aiBot, error) {
	var b aiBot
	var raw []byte
	e := tx.QueryRow(ctx, "SELECT to_jsonb(b) FROM ai_bots b WHERE id=$1", id).Scan(&raw)
	if e != nil {
		return b, e
	}
	e = json.Unmarshal(raw, &b)
	return b, e
}
