// 文件作用说明：建立独立访客身份、记录去重访问，并按入口范围创建或恢复咨询。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// routingInput 定义访客可以申请但不能扩大权限的接待目标。
type routingInput struct {
	GroupID   string `json:"service_group_id"`
	PoolID    string `json:"routing_pool_id"`
	AgentID   string `json:"agent_id"`
	AgentMode string `json:"agent_mode"`
}

// visitorInput 定义统一初始化请求，未验证资料与身份凭证分别保存。
type visitorInput struct {
	EntryKey     string       `json:"entry_key"`
	VisitorToken string       `json:"visitor_token"`
	VisitID      string       `json:"visit_id"`
	HandoffCode  string       `json:"handoff_code"`
	Routing      routingInput `json:"routing"`
	Profile      struct {
		DisplayName    string `json:"display_name"`
		Phone          string `json:"phone"`
		AvatarURL      string `json:"avatar_url"`
		ExternalUserID string `json:"external_user_id"`
	} `json:"profile"`
	Context    map[string]any `json:"context"`
	Attributes map[string]any `json:"attributes"`
	Tracking   map[string]any `json:"tracking"`
}

// entry 只公开入口允许展示的配置，不凭公开链接返回聊天历史。
func (s *Service) entry(w http.ResponseWriter, r *http.Request) {
	tenant, err := s.entryTenant(r.Context(), r.PathValue("entry_key"))
	if err != nil {
		replyError(w, r, err)
		return
	}
	var result map[string]any
	err = platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		if err := tenantActive(r.Context(), tx, tenant); err != nil {
			return err
		}
		var err error
		result, err = entryJSON(r.Context(), tx, r.PathValue("entry_key"))
		if err == nil && s.entryDecoration != nil {
			linkID, _ := result["id"].(string)
			var design map[string]any
			design, err = s.entryDecoration(r.Context(), tx, tenant, linkID)
			for key, value := range design {
				result[key] = value
			}
		}
		return err
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// entryTenant 通过受限定位函数取得入口租户，不接受前端租户编号。
func (s *Service) entryTenant(ctx context.Context, key string) (string, error) {
	if len(key) < 8 || len(key) > 128 {
		return "", platform.Fail(404, "entry_not_found", "咨询入口不存在")
	}
	var tenant *string
	err := s.pool.QueryRow(ctx, "SELECT chat_entry_tenant($1)::text", key).Scan(&tenant)
	if err != nil {
		return "", err
	}
	if tenant == nil {
		return "", platform.Fail(404, "entry_not_found", "咨询入口不存在")
	}
	return *tenant, nil
}

// entryJSON 返回公开配置并确认入口未停用或过期。
func entryJSON(ctx context.Context, tx pgx.Tx, key string) (map[string]any, error) {
	return txJSON(ctx, tx, "SELECT jsonb_build_object('id',l.id,'entry_key',l.entry_key,'name',l.name,'tenant_name',t.name,'service_space_id',l.service_space_id,'service_group_id',l.service_group_id,'routing_pool_id',l.routing_pool_id,'agent_id',l.agent_id,'agent_mode',l.agent_mode,'channel_code',l.channel_code,'channel_tags',l.channel_tags,'reception_mode',l.reception_mode,'allow_routing_override',true,'page_id',l.page_id,'pinned_release_id',l.pinned_release_id,'binding_mode',l.binding_mode) FROM access_links l JOIN tenants t ON t.id=l.tenant_id WHERE l.entry_key=$1 AND l.status='active' AND (l.expires_at IS NULL OR l.expires_at>now())", key)
}

// visitorSession 只通过独立随机凭证恢复客户，手机号永远只作为资料。
func (s *Service) visitorSession(w http.ResponseWriter, r *http.Request) {
	var in visitorInput
	if err := platform.Decode(w, r, &in); err != nil {
		replyError(w, r, err)
		return
	}
	if in.HandoffCode != "" {
		replyError(w, r, platform.Fail(501, "handoff_unavailable", "可信商户身份交接尚未启用，请使用匿名接入"))
		return
	}
	if in.Context == nil {
		in.Context = map[string]any{}
	}
	if in.Attributes == nil {
		in.Attributes = map[string]any{}
	}
	if err := validateContext(in.Context); err != nil {
		replyError(w, r, err)
		return
	}
	if len([]rune(in.Profile.DisplayName)) > 80 || len(in.Profile.Phone) > 40 {
		replyError(w, r, platform.Fail(400, "invalid_profile", "姓名或联系方式过长"))
		return
	}
	if in.Profile.AvatarURL != "" {
		u, err := url.Parse(in.Profile.AvatarURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			replyError(w, r, platform.Fail(400, "invalid_avatar", "头像必须为有效HTTPS地址"))
			return
		}
	}
	tenant, err := s.entryTenant(r.Context(), in.EntryKey)
	if err != nil {
		replyError(w, r, err)
		return
	}
	token := in.VisitorToken
	if token == "" {
		header := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if strings.HasPrefix(header, "v_") {
			token = header
		}
	}
	a := actor{TenantID: tenant}
	newVisitor := token == ""
	if !newVisitor {
		a, err = s.visitorActor(r.Context(), token)
		if err != nil || a.TenantID != tenant {
			replyError(w, r, platform.Fail(401, "visitor_session_mismatch", "访客凭证不属于当前商户或已过期"))
			return
		}
	} else {
		value, e := platform.Token()
		if e != nil {
			replyError(w, r, e)
			return
		}
		token = "v_" + value
		a.ContactID = newID()
	}
	if in.VisitID == "" {
		in.VisitID = newID()
	}
	if !uuidPattern.MatchString(in.VisitID) {
		replyError(w, r, platform.Fail(400, "invalid_visit", "访问去重编号格式不正确"))
		return
	}
	var result map[string]any
	err = platform.TenantTx(r.Context(), s.pool, tenant, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tenantActive(ctx, tx, tenant); err != nil {
			return err
		}
		entry, err := entryJSON(ctx, tx, in.EntryKey)
		if err != nil {
			return err
		}
		if err := validateAttributes(ctx, tx, in.Attributes); err != nil {
			return err
		}
		tracking := cleanTracking(in.Tracking)
		tracking["channel_code"] = entry["channel_code"]
		tracking["channel_tags"] = entry["channel_tags"]
		if newVisitor {
			if in.Profile.AvatarURL == "" {
				in.Profile.AvatarURL, err = defaultVisitorAvatar(ctx, tx, tenant)
				if err != nil {
					return err
				}
			}
			name := strings.TrimSpace(in.Profile.DisplayName)
			if name == "" {
				name = "访客"
			}
			_, err = tx.Exec(ctx, "INSERT INTO contacts(id,tenant_id,display_name,phone,avatar_url,attributes,first_source,last_source,field_sources) VALUES($1,$2,$3,$4,$5,$6,$7,$7,'{\"display_name\":\"unverified_visitor\",\"phone\":\"unverified_visitor\"}')", a.ContactID, tenant, name, in.Profile.Phone, in.Profile.AvatarURL, jsonValue(in.Attributes), jsonValue(tracking))
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, "INSERT INTO visitor_sessions(tenant_id,contact_id,access_link_id,token_hash) VALUES($1,$2,$3,$4)", tenant, a.ContactID, entry["id"], hash(token))
			if err != nil {
				return err
			}
		} else {
			if err := blocked(ctx, tx, a.ContactID); err != nil {
				return err
			}
			if in.Profile.AvatarURL == "" {
				var previous string
				var locked bool
				if err := tx.QueryRow(ctx, "SELECT avatar_url,locked_fields ? 'avatar_url' FROM contacts WHERE id=$1 AND NOT archived FOR UPDATE", a.ContactID).Scan(&previous, &locked); err != nil {
					return err
				}
				// 旧客户首次回访只补未锁定空头像，行锁保证并发访问和后续刷新不会重新抽取。
				if previous == "" && !locked {
					in.Profile.AvatarURL, err = defaultVisitorAvatar(ctx, tx, tenant)
					if err != nil {
						return err
					}
				}
			}
			_, err = tx.Exec(ctx, "UPDATE contacts SET display_name=CASE WHEN $2!='' AND NOT locked_fields ? 'display_name' THEN $2 ELSE display_name END,phone=CASE WHEN $3!='' AND NOT locked_fields ? 'phone' THEN $3 ELSE phone END,avatar_url=CASE WHEN $4!='' AND NOT locked_fields ? 'avatar_url' THEN $4 ELSE avatar_url END,last_source=$5,updated_at=now() WHERE id=$1 AND NOT archived", a.ContactID, in.Profile.DisplayName, in.Profile.Phone, in.Profile.AvatarURL, jsonValue(tracking))
			if err != nil {
				return err
			}
		}
		visitTag, err := tx.Exec(ctx, "INSERT INTO contact_visits(id,tenant_id,contact_id,access_link_id,event_id,tracking,context) VALUES($1::uuid,$2,$3,$4,($1::uuid)::text,$5,$6) ON CONFLICT(tenant_id,event_id) DO NOTHING", in.VisitID, tenant, a.ContactID, entry["id"], jsonValue(tracking), jsonValue(in.Context))
		if err != nil {
			return err
		}
		if visitTag.RowsAffected() == 0 {
			var owner string
			if err := tx.QueryRow(ctx, "SELECT contact_id::text FROM contact_visits WHERE event_id=$1", in.VisitID).Scan(&owner); err != nil {
				return err
			}
			if owner != a.ContactID {
				return platform.Fail(409, "visit_conflict", "访问编号已经被其他初始化使用")
			}
		}
		if visitTag.RowsAffected() > 0 && s.visitorHook != nil {
			payload := map[string]any{"event_type": "visitor.entered", "event_id": in.VisitID, "visit_id": in.VisitID, "contact_id": a.ContactID, "access_link_id": entry["id"], "entry_key": in.EntryKey, "profile": map[string]any{"display_name": in.Profile.DisplayName, "phone": in.Profile.Phone, "avatar_url": in.Profile.AvatarURL, "verified": false}, "context": in.Context, "tracking": tracking}
			if err := s.visitorHook(ctx, tx, tenant, in.VisitID, payload); err != nil {
				return err
			}
		}
		var conversationID string
		err = tx.QueryRow(ctx, "SELECT id::text FROM conversations WHERE contact_id=$1 AND service_space_id=$2 AND status!='closed'", a.ContactID, entry["service_space_id"]).Scan(&conversationID)
		if err == pgx.ErrNoRows {
			if err := s.checkFeature(ctx, tx, tenant, "chat"); err != nil {
				return err
			}
			target, err := resolveRouting(ctx, tx, entry, in.Routing)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "SELECT id FROM routing_pools WHERE id=$1 FOR UPDATE", target.PoolID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", tenant+a.ContactID+entry["service_space_id"].(string)); err != nil {
				return err
			}
			err = tx.QueryRow(ctx, "SELECT id::text FROM conversations WHERE contact_id=$1 AND service_space_id=$2 AND status!='closed'", a.ContactID, entry["service_space_id"]).Scan(&conversationID)
			if err == pgx.ErrNoRows {
				conversationID = newID()
				_, err = tx.Exec(ctx, "INSERT INTO conversations(id,tenant_id,service_space_id,contact_id,created_entry_id,service_group_id,routing_pool_id,desired_agent_id,agent_mode,source_snapshot,context) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10,$11)", conversationID, tenant, entry["service_space_id"], a.ContactID, entry["id"], target.GroupID, target.PoolID, target.AgentID, target.AgentMode, jsonValue(map[string]any{"entry": entry, "tracking": tracking, "routing": target}), jsonValue(in.Context))
				if err != nil {
					return err
				}
				if _, err := s.emit(ctx, tx, tenant, conversationID, "conversation.created", "public", map[string]any{"status": "queued"}); err != nil {
					return err
				}
				if err := s.assignLocked(ctx, tx, tenant, conversationID, "", false); err != nil {
					return err
				}
				if err := s.enqueueWelcome(ctx, tx, tenant, conversationID); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		// 复访保留最初来源快照，只合并本次提供的业务资料；未提供字段保持原值。
		if visitTag.RowsAffected() > 0 && len(in.Context) > 0 {
			var previous []byte
			if err := tx.QueryRow(ctx, "SELECT context FROM conversations WHERE id=$1 FOR UPDATE", conversationID).Scan(&previous); err != nil {
				return err
			}
			merged := decodeJSONMap(previous)
			for key, value := range in.Context {
				merged[key] = value
			}
			if err := validateContext(merged); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE conversations SET context=$2 WHERE id=$1", conversationID, jsonValue(merged)); err != nil {
				return err
			}
		}
		conversation, err := conversationJSON(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		contact, err := txJSON(ctx, tx, "SELECT to_jsonb(c)-'field_sources'-'locked_fields'-'notes' FROM contacts c WHERE id=$1", a.ContactID)
		if err != nil {
			return err
		}
		result = map[string]any{"token": token, "contact": contact, "conversation": conversation, "entry": entry, "visit_id": in.VisitID}
		return nil
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}

// resolveRouting 先应用入口默认值，再检查动态组池客服的允许交集。
func resolveRouting(ctx context.Context, tx pgx.Tx, entry map[string]any, in routingInput) (routingInput, error) {
	t := routingInput{GroupID: entry["service_group_id"].(string), PoolID: entry["routing_pool_id"].(string), AgentMode: entry["agent_mode"].(string)}
	if v, ok := entry["agent_id"].(string); ok {
		t.AgentID = v
	}
	if in.GroupID != "" || in.PoolID != "" || in.AgentID != "" || in.AgentMode != "" {
		if in.GroupID != "" && in.GroupID != t.GroupID {
			return t, platform.Fail(400, "group_not_allowed", "该入口未授权其他客服组")
		}
		if in.PoolID != "" {
			t.PoolID = in.PoolID
		}
		if in.AgentID != "" {
			t.AgentID = in.AgentID
		}
		if in.AgentMode != "" {
			if t.AgentMode == "strict" && in.AgentMode != "strict" {
				return t, platform.Fail(400, "strict_mode_protected", "公开参数不能放宽入口的严格指定客服规则")
			}
			t.AgentMode = in.AgentMode
		}
	}
	if !uuidPattern.MatchString(t.GroupID) || !uuidPattern.MatchString(t.PoolID) || (t.AgentID != "" && !uuidPattern.MatchString(t.AgentID)) || !inStrings(t.AgentMode, "prefer", "strict") {
		return t, platform.Fail(400, "invalid_routing", "接待目标格式不正确")
	}
	var valid bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM routing_pools p JOIN agent_groups g ON g.id=p.service_group_id AND g.tenant_id=p.tenant_id WHERE p.id=$1 AND p.service_group_id=$2 AND NOT p.archived AND NOT g.archived AND ($3='' OR (EXISTS(SELECT 1 FROM agent_group_members gm WHERE gm.group_id=g.id AND gm.membership_id=NULLIF($3,'')::uuid) AND (p.members_mode='group' OR EXISTS(SELECT 1 FROM routing_pool_members pm WHERE pm.routing_pool_id=p.id AND pm.membership_id=NULLIF($3,'')::uuid)))))", t.PoolID, t.GroupID, t.AgentID).Scan(&valid)
	if err != nil {
		return t, err
	}
	if !valid {
		return t, platform.Fail(400, "routing_conflict", "指定客服、分配池与业务组不兼容")
	}
	return t, nil
}

// cleanTracking 只保存允许的来源字段，URL查询不会保留手机号或认证资料。
func cleanTracking(input map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"source", "campaign_id", "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "landing_url", "referrer"} {
		value, ok := input[key].(string)
		if !ok || len(value) > 2048 {
			continue
		}
		if key == "landing_url" || key == "referrer" {
			u, err := url.Parse(value)
			if err != nil || !inStrings(u.Scheme, "http", "https") {
				continue
			}
			q := url.Values{}
			for k, values := range u.Query() {
				if inStrings(k, "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content") {
					q[k] = values
				}
			}
			u.RawQuery = q.Encode()
			u.Fragment = ""
			u.User = nil
			value = u.String()
		}
		out[key] = value
	}
	return out
}

// decodeJSONMap 读取数据库JSON对象，空对象不会变为null。
func decodeJSONMap(raw []byte) map[string]any {
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return result
}
