// 文件作用说明：把AI意图在同一会话及结算事务中提交，迟到、越权或已被人工接管的结果不可发送。
package chat

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/localization"
)

// aiBotGroupScopeSQL 要求默认机器人所属客服仍在入口业务组与分配池内，离线AI也不能扩大成员数据权限。
const aiBotGroupScopeSQL = `EXISTS(SELECT 1 FROM routing_pools p JOIN agent_groups g ON g.id=p.service_group_id AND g.tenant_id=p.tenant_id
 JOIN agent_group_members gm ON gm.group_id=g.id AND gm.tenant_id=g.tenant_id
 WHERE p.id=c.routing_pool_id AND p.tenant_id=c.tenant_id AND g.id=c.service_group_id
 AND NOT p.archived AND NOT g.archived AND gm.membership_id=b.owner_membership_id
 AND (p.members_mode='group' OR EXISTS(SELECT 1 FROM routing_pool_members pm WHERE pm.routing_pool_id=p.id AND pm.tenant_id=p.tenant_id AND pm.membership_id=b.owner_membership_id)))`

// memberEntryAIBotID 为人工接待选择本人入口配置或稳定默认助手，创建和提交任务使用同一规则。
func memberEntryAIBotID(ctx context.Context, tx pgx.Tx, member, entry string) (string, error) {
	var selected string
	if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT l.ai_bot_id::text FROM access_links l JOIN ai_bots b ON b.id=l.ai_bot_id WHERE l.id=$1 AND b.owner_membership_id=$2),'')`, entry, member).Scan(&selected); err != nil {
		return "", err
	}
	return memberAIBotID(ctx, tx, member, selected)
}

// enqueueAIReply 仅在没有命中关键词规则时为真实访客新消息创建一次可靠任务。
func (s *Service) enqueueAIReply(ctx context.Context, tx pgx.Tx, tenant, conversation, message, locale string) error {
	var agent, assignment, entry string
	var epoch int64
	var paused bool
	var status string
	if e := tx.QueryRow(ctx, "SELECT coalesce(current_agent_id::text,''),coalesce(current_assignment_id::text,''),created_entry_id::text,ai_epoch,ai_paused,status FROM conversations WHERE id=$1", conversation).Scan(&agent, &assignment, &entry, &epoch, &paused, &status); e != nil {
		return e
	}
	if paused || status == "closed" {
		return nil
	}
	var id string
	if agent != "" && status == "active" {
		var e error
		id, e = memberEntryAIBotID(ctx, tx, agent, entry)
		if e == pgx.ErrNoRows {
			return nil
		}
		if e != nil {
			return e
		}
	} else if agent == "" && status == "queued" {
		var e error
		id, e = aiReceptionBot(ctx, tx, conversation, false)
		if e != nil {
			return e
		}
		if id == "" {
			return nil
		}
	} else {
		return nil
	}
	b, e := loadAIBot(ctx, tx, id)
	if e != nil {
		return e
	}
	if b.Mode != "assistant" && b.Mode != "auto" {
		return nil
	}
	if agent == "" {
		if e := tx.QueryRow(ctx, `SELECT name,avatar_ref FROM ai_service_agents WHERE enabled AND NOT EXISTS(SELECT 1 FROM access_links WHERE id=$1 AND ai_bot_id IS NOT NULL)`, entry).Scan(&b.Name, &b.AvatarRef); e != nil && e != pgx.ErrNoRows {
			return e
		}
	}
	snapshot := aiSnapshot{Bot: b, Locale: locale, Epoch: epoch, Agent: agent, Assignment: assignment, Entry: entry}
	jobID := newID()
	tag, e := tx.Exec(ctx, `INSERT INTO ai_jobs(id,tenant_id,kind,bot_id,conversation_id,trigger_message_id,owner_membership_id,snapshot,run_after) VALUES($1,$2,'reply',$3,$4,$5,$6,$7,now()+interval '1 second') ON CONFLICT(tenant_id,trigger_message_id) DO NOTHING`, jobID, tenant, id, conversation, message, b.Owner, jsonValue(snapshot))
	if e != nil || tag.RowsAffected() == 0 {
		return e
	}
	return s.aiThinking(ctx, tx, &aiJobData{ID: jobID, Tenant: tenant, Kind: "reply", Conversation: conversation, Snapshot: snapshot}, true)
}

// aiReplyValid 重新检查控制权、最新访客消息和机器人版本，最终提交时锁定会话行。
func (s *Service) aiReplyValid(ctx context.Context, tx pgx.Tx, j *aiJobData, lock bool) error {
	query := "SELECT status,coalesce(current_agent_id::text,''),coalesce(current_assignment_id::text,''),ai_epoch,ai_paused,contact_id::text,created_entry_id::text FROM conversations WHERE id=$1"
	if lock {
		query += " FOR UPDATE"
	}
	var status, agent, assignment, contact, entry string
	var epoch int64
	var paused bool
	if e := tx.QueryRow(ctx, query, j.Conversation).Scan(&status, &agent, &assignment, &epoch, &paused, &contact, &entry); e != nil {
		return e
	}
	if status == "closed" || paused || epoch != j.Snapshot.Epoch || agent != j.Snapshot.Agent || assignment != j.Snapshot.Assignment {
		return errors.New("ai_stale")
	}
	if agent != "" && status != "active" {
		return errors.New("ai_stale")
	}
	if e := blocked(ctx, tx, contact); e != nil {
		return errors.New("ai_stale")
	}
	var latest string
	if e := tx.QueryRow(ctx, "SELECT id::text FROM messages WHERE conversation_id=$1 AND sender_kind='visitor' AND audience='public' AND withdrawn_at IS NULL ORDER BY event_seq DESC LIMIT 1", j.Conversation).Scan(&latest); e != nil {
		return e
	}
	if latest != j.Trigger {
		return errors.New("ai_stale")
	}
	var valid bool
	e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_bots b JOIN tenant_memberships m ON m.id=b.owner_membership_id WHERE b.id=$1 AND b.version=$2 AND b.mode<>'off' AND NOT m.disabled AND m.removed_at IS NULL AND (m.can_serve OR $3))", j.BotID, j.Snapshot.Bot.Version, agent == "").Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return errors.New("ai_stale")
	}
	var current string
	if agent == "" {
		current, e = aiReceptionBot(ctx, tx, j.Conversation, false)
	} else {
		current, e = memberEntryAIBotID(ctx, tx, agent, entry)
	}
	if e == pgx.ErrNoRows || e == nil && current != j.BotID {
		return errors.New("ai_stale")
	}
	if e != nil {
		return e
	}

	return nil
}

// aiCheckReadVersions 禁用或更新已引用的知识时拒绝发送旧事实。
func aiCheckReadVersions(ctx context.Context, tx pgx.Tx, j *aiJobData, plan aiPlan) error {
	for _, id := range plan.ReadIDs {
		var version int64
		for _, k := range j.Snapshot.Knowledge {
			if k.ID == id {
				version = k.Version
				break
			}
		}
		var current int64
		if e := tx.QueryRow(ctx, "SELECT published_version FROM ai_knowledge_bases WHERE id=$1 AND published_version=$2 AND enabled AND NOT archived FOR SHARE", id, version).Scan(&current); e != nil {
			if e == pgx.ErrNoRows {
				return errors.New("ai_stale")
			}
			return e
		}
	}
	return nil
}

// completeAIJob 在提交前重查授权，知识发布、草稿结算或正式消息只产生一次。
func (s *Service) completeAIJob(ctx context.Context, tx pgx.Tx, j *aiJobData, plan aiPlan) error {
	var status string
	if e := tx.QueryRow(ctx, "SELECT status FROM ai_jobs WHERE id=$1 FOR UPDATE", j.ID).Scan(&status); e != nil {
		return e
	}
	if status != "running" {
		return nil
	}
	if e := tenantActive(ctx, tx, j.Tenant); e != nil {
		return e
	}
	var modelEnabled bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_chat_models WHERE id=$1 AND enabled)", j.Snapshot.Model.ID).Scan(&modelEnabled); e != nil {
		return e
	}
	if !modelEnabled {
		return errors.New("ai_stale")
	}
	if j.Kind == "summary" {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,47))", j.Tenant); e != nil {
			return e
		}
		tag, e := tx.Exec(ctx, `UPDATE ai_knowledge_bases SET published_body=$3,description=$4,published_version=version,status='ready',error_code='',updated_at=now() WHERE id=$1 AND version=$2 AND NOT archived`, j.KnowledgeID, j.Snapshot.KnowledgeVersion, j.Snapshot.Body, plan.Description)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return errors.New("ai_stale")
		}
		if e := aiResolveGaps(ctx, tx, j); e != nil {
			return e
		}
	} else if j.Kind == "reply" {
		if e := s.aiReplyValid(ctx, tx, j, true); e != nil {
			return e
		}
		for _, ref := range j.Snapshot.Images {
			var exists bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM messages WHERE id=$1 AND conversation_id=$2 AND withdrawn_at IS NULL)", ref.MessageID, j.Conversation).Scan(&exists); e != nil {
				return e
			}
			if !exists {
				return errors.New("ai_stale")
			}
		}
		if e := aiCheckReadVersions(ctx, tx, j, plan); e != nil {
			return e
		}
	}
	if j.Kind != "summary" {
		if _, e := s.expandAIPlan(ctx, tx, j, plan); e != nil {
			return e
		}
	}
	if e := settleAIWallet(ctx, tx, j, true); e != nil {
		return e
	}
	terminal := "succeeded"
	if j.Kind == "reply" && j.Snapshot.Bot.Mode == "assistant" && !plan.Handoff {
		terminal = "draft"
	} else if j.Kind == "reply" {
		if plan.Handoff {
			if e := s.aiHandoff(ctx, tx, j, "requested"); e != nil {
				return e
			}
		} else {
			if e := s.sendAIPlan(ctx, tx, j, plan); e != nil {
				return e
			}
		}
	}
	if _, e := tx.Exec(ctx, "UPDATE ai_jobs SET status=$2,result=$3,completed_at=now(),lease_until=NULL WHERE id=$1", j.ID, terminal, jsonValue(plan)); e != nil {
		return e
	}
	if e := aiTraceTx(ctx, tx, j, "completed", terminal, plan); e != nil {
		return e
	}
	return s.aiThinking(ctx, tx, j, false)
}

// expandAIPlan 在当前租户和绑定内读取话术真实内容，限制组合后的消息数量。
func (s *Service) expandAIPlan(ctx context.Context, tx pgx.Tx, j *aiJobData, plan aiPlan) ([]aiBlock, error) {
	if plan.Handoff {
		return nil, nil
	}
	if e := validateAIPlan(plan, j.Snapshot.Scripts); e != nil {
		return nil, e
	}
	blocks := []aiBlock{}
	for _, item := range plan.Items {
		if item.Type == "text" {
			blocks = append(blocks, aiBlock{Type: "text", Text: item.Text})
			continue
		}
		quick := false
		for _, ref := range j.Snapshot.Scripts {
			if ref.ID == item.ScriptID && ref.Version == item.Version {
				quick = ref.QuickReply
				break
			}
		}
		if quick {
			q, e := loadQuickReply(ctx, tx, item.ScriptID, "FOR SHARE")
			if e != nil || !q.Enabled || q.Version != item.Version || !quickReplyVisible(q, j.Owner) {
				return nil, errors.New("ai_stale")
			}
			blocks = append(blocks, aiBlock{Type: q.Type, Text: q.Content, MediaID: q.AttachmentID, FormFields: q.FormFields, Options: q.Options})
			continue
		}
		var raw []byte
		if e := tx.QueryRow(ctx, "SELECT blocks FROM ai_scripts WHERE id=$1 AND version=$2 AND enabled AND NOT archived FOR SHARE", item.ScriptID, item.Version).Scan(&raw); e != nil {
			return nil, errors.New("ai_stale")
		}
		parts, e := decodeAIBlocks(raw)
		if e != nil {
			return nil, e
		}
		if e = validateAIBlocks(ctx, tx, parts); e != nil {
			return nil, e
		}
		blocks = append(blocks, parts...)
	}
	if len(blocks) > 12 {
		return nil, errors.New("ai_reply_limit")
	}
	return blocks, nil
}

// sendAIPlan 将有序回复内容通过既有事件及消息持久层发送，媒体消息使用已有类型。
func (s *Service) sendAIPlan(ctx context.Context, tx pgx.Tx, j *aiJobData, plan aiPlan) error {
	blocks, e := s.expandAIPlan(ctx, tx, j, plan)
	if e != nil {
		return e
	}
	for _, block := range blocks {
		if e = s.insertAIMessage(ctx, tx, j, block); e != nil {
			return e
		}
	}
	return nil
}

// insertAIMessage 保留AI来源与机器人身份，不伪造人工发送或人工首响统计。
func (s *Service) insertAIMessage(ctx context.Context, tx pgx.Tx, j *aiJobData, b aiBlock) error {
	id, client := newID(), newID()
	options := append([]welcomeOption{}, b.Options...)
	for i := range options {
		options[i].ClientID = newID()
	}
	kind := b.Type
	if kind == "video" {
		kind = "file"
	}
	seq, e := s.emit(ctx, tx, j.Tenant, j.Conversation, "message.created", "public", map[string]any{"id": id})
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO messages(id,tenant_id,conversation_id,sender_id,sender_kind,client_message_id,message_type,content,audience,event_seq,attachment_id,source,ai_job_id,ai_bot_id,form_fields,options) VALUES($1,$2,$3,$2,'system',$4,$5,$6,'public',$7,NULLIF($8,'')::uuid,'ai',$9,$10,$11,$12)`, id, j.Tenant, j.Conversation, client, kind, b.Text, seq, b.MediaID, j.ID, j.BotID, formFieldsJSON(b.FormFields), jsonValue(options)); e != nil {
		return e
	}
	item, e := txJSON(ctx, tx, "SELECT "+messageProjection+" FROM messages m WHERE id=$1", id)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "UPDATE conversation_events SET payload=$2 WHERE conversation_id=$1 AND seq=$3", j.Conversation, jsonValue(item), seq)
	return e
}

// aiHandoff 暂停机器人后进入真实人工分配，排队时只承诺等待而非已接通。
func (s *Service) aiHandoff(ctx context.Context, tx pgx.Tx, j *aiJobData, reason string) error {
	if e := s.aiReplyValid(ctx, tx, j, true); e != nil {
		return nil
	}
	if _, e := tx.Exec(ctx, `UPDATE conversations SET ai_paused=true,ai_epoch=ai_epoch+1,context=context-'ai_receiving'-'ai_thinking' WHERE id=$1`, j.Conversation); e != nil {
		return e
	}
	if e := s.assignLocked(ctx, tx, j.Tenant, j.Conversation, "", false); e != nil {
		return e
	}
	text := localization.Text(j.Snapshot.Locale, "请稍等，正在为您转接人工客服。您可以继续补充问题，客服接入后会为您处理。")
	if reason == "ai_unsupported_input" {
		text = localization.Text(j.Snapshot.Locale, "当前AI无法识别此类附件，请用文字或图片描述，或等待人工客服处理。")
	}
	if e := s.insertAIMessage(ctx, tx, j, aiBlock{Type: "text", Text: text}); e != nil {
		return e
	}
	_, e := s.emit(ctx, tx, j.Tenant, j.Conversation, "conversation.updated", "public", map[string]any{"id": j.Conversation, "ai_paused": true, "ai_reason": reason})
	return e
}

// aiJobFromRow 恢复权限校验后的任务，草稿发送与工作器共用同一结构。
func aiJobFromRow(ctx context.Context, tx pgx.Tx, id string) (aiJobData, error) {
	var j aiJobData
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT id::text,tenant_id::text,kind,coalesce(bot_id::text,''),coalesce(knowledge_id::text,''),coalesce(conversation_id::text,''),coalesce(trigger_message_id::text,''),owner_membership_id::text,snapshot,reserved_micro FROM ai_jobs WHERE id=$1`, id).Scan(&j.ID, &j.Tenant, &j.Kind, &j.BotID, &j.KnowledgeID, &j.Conversation, &j.Trigger, &j.Owner, &raw, &j.Reserved)
	if e == nil {
		e = json.Unmarshal(raw, &j.Snapshot)
	}
	return j, e
}
