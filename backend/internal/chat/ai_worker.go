// 文件作用说明：以数据库租约恢复AI任务，异步生成知识描述、辅助草稿及自动回复。
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// startAIWorkers 限制本进程并发为二，任务事实及租约完全保存在数据库。
func (s *Service) startAIWorkers(ctx context.Context) {
	for n := 0; n < 2; n++ {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if e := s.aiTick(ctx); e != nil && ctx.Err() == nil {
						slog.Error("AI工作器暂未完成", "error_code", "ai_worker_failed")
					}
				}
			}
		}()
	}
}

// aiTick 逐租户进入RLS事务，恢复过期任务后最多执行一个可用任务。
func (s *Service) aiTick(ctx context.Context) error {
	rows, e := s.pool.Query(ctx, "SELECT id::text FROM tenants WHERE status='active' ORDER BY created_at")
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, tenant := range ids {
		var j aiJobData
		found := false
		e = platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
			if e := s.recoverAIJobs(ctx, tx, tenant); e != nil {
				return e
			}
			var raw []byte
			e := tx.QueryRow(ctx, `SELECT id::text,kind,coalesce(bot_id::text,''),coalesce(knowledge_id::text,''),coalesce(conversation_id::text,''),coalesce(trigger_message_id::text,''),owner_membership_id::text,snapshot FROM ai_jobs WHERE status='queued' AND run_after<=now() ORDER BY run_after,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&j.ID, &j.Kind, &j.BotID, &j.KnowledgeID, &j.Conversation, &j.Trigger, &j.Owner, &raw)
			if e == pgx.ErrNoRows {
				return nil
			}
			if e != nil {
				return e
			}
			j.Tenant = tenant
			if e = json.Unmarshal(raw, &j.Snapshot); e != nil {
				return e
			}
			found = true
			_, e = tx.Exec(ctx, "UPDATE ai_jobs SET status='running',lease_until=now()+interval '5 minutes' WHERE id=$1", j.ID)
			return e
		})
		if e != nil {
			return e
		}
		if found {
			s.executeAIJob(ctx, &j)
			return nil
		}
	}
	return nil
}

// recoverAIJobs 不重放可能已经收费的调用，未知任务保留预占等待平台处理。
func (s *Service) recoverAIJobs(ctx context.Context, tx pgx.Tx, tenant string) error {
	rows, e := tx.Query(ctx, "SELECT id::text FROM ai_jobs WHERE status='running' AND lease_until<now() FOR UPDATE SKIP LOCKED")
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	for _, id := range ids {
		var called bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_call_attempts WHERE job_id=$1)", id).Scan(&called); e != nil {
			return e
		}
		status := "failed"
		billing := "not_called"
		if called {
			status = "uncertain"
			billing = "uncertain"
		} else {
			j := aiJobData{ID: id, Tenant: tenant}
			if e = settleAIWallet(ctx, tx, &j, false); e != nil {
				return e
			}
		}
		if _, e = tx.Exec(ctx, `UPDATE ai_pending_items p SET status='pending',updated_at=now() FROM ai_jobs j WHERE j.id=$1 AND j.kind='summary' AND p.knowledge_id=j.knowledge_id AND p.knowledge_version<=(j.snapshot->>'knowledge_version')::bigint AND p.status='supplementing'`, id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE ai_knowledge_bases k SET status='failed',error_code='ai_interrupted' FROM ai_jobs j WHERE j.id=$1 AND j.kind='summary' AND k.id=j.knowledge_id AND k.version=(j.snapshot->>'knowledge_version')::bigint`, id); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE ai_jobs SET status=$2,billing_status=$3,error_code='ai_interrupted',completed_at=now() WHERE id=$1", id, status, billing); e != nil {
			return e
		}
	}
	return nil
}

// executeAIJob 将网络调用置于数据库事务之外，最终统一校验并提交结果。
func (s *Service) executeAIJob(parent context.Context, j *aiJobData) {
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	err := platform.TenantTx(ctx, s.pool, j.Tenant, func(tx pgx.Tx) error { return s.prepareAIJob(ctx, tx, j) })
	if err != nil {
		s.failAIJob(context.WithoutCancel(parent), j, err)
		return
	}
	plan, err := s.aiRun(ctx, j)
	if err != nil {
		s.failAIJob(context.WithoutCancel(parent), j, err)
		return
	}
	err = platform.TenantTx(ctx, s.pool, j.Tenant, func(tx pgx.Tx) error { return s.completeAIJob(ctx, tx, j, plan) })
	if err != nil {
		s.failAIJob(context.WithoutCancel(parent), j, err)
	}
}

// prepareAIJob 固定实际模型版本、授权目录、聊天上下文和调用预算。
func (s *Service) prepareAIJob(ctx context.Context, tx pgx.Tx, j *aiJobData) error {
	if e := tenantActive(ctx, tx, j.Tenant); e != nil {
		return e
	}
	var active bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE id=$1 AND NOT disabled AND removed_at IS NULL)", j.Owner).Scan(&active); e != nil {
		return e
	}
	if !active {
		return errors.New("ai_member_unavailable")
	}
	if j.BotID != "" {
		b, e := loadAIBot(ctx, tx, j.BotID)
		if e != nil {
			return e
		}
		if b.Version != j.Snapshot.Bot.Version || b.Mode == "off" && j.Kind != "test" {
			return errors.New("ai_stale")
		}
		j.Snapshot.Bot = b
		if j.Kind == "reply" {
			var inputType string
			if e = tx.QueryRow(ctx, "SELECT message_type FROM messages WHERE id=$1", j.Trigger).Scan(&inputType); e != nil {
				return e
			}
			if !inStrings(inputType, "text", "image", "form_result") {
				return errors.New("ai_unsupported_input")
			}
			if e = s.aiReplyValid(ctx, tx, j, false); e != nil {
				return e
			}
			var zone string
			if e = tx.QueryRow(ctx, "SELECT timezone FROM tenants WHERE id=$1", j.Tenant).Scan(&zone); e != nil {
				return e
			}
			loc, e := time.LoadLocation(zone)
			if e != nil {
				return e
			}
			if !aiWithinHours(b.Settings, time.Now().In(loc)) {
				return errors.New("ai_outside_hours")
			}
			if !b.Settings.Offline {
				var online bool
				if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agent_runtime WHERE membership_id=$1 AND available AND online_until>now())", j.Owner).Scan(&online); e != nil {
					return e
				}
				if !online {
					return errors.New("ai_member_offline")
				}
			}
			var turns int
			if e = tx.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id=$1 AND ai_job_id IS NOT NULL AND event_seq>coalesce((SELECT max(event_seq) FROM messages WHERE conversation_id=$1 AND sender_kind='agent' AND ai_job_id IS NULL),0)`, j.Conversation).Scan(&turns); e != nil {
				return e
			}
			if turns >= b.Settings.MaxTurns {
				return errors.New("ai_turn_limit")
			}
		}
		if e = s.aiCatalog(ctx, tx, j); e != nil {
			return e
		}
	}
	m, e := s.loadAIModel(ctx, tx, j.Snapshot.Bot.ModelID, 0)
	if e != nil {
		return e
	}
	m.APIKey = ""
	// 摘要同样可能包含供应商计费的思考Token，使用平台总输出预算，正文长度另行严格校验。
	if j.BotID != "" && m.MaxOutput > j.Snapshot.Bot.Settings.MaxOutput {
		m.MaxOutput = j.Snapshot.Bot.Settings.MaxOutput
	}
	j.Snapshot.Model = m
	if j.Kind == "summary" {
		var valid bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_knowledge_bases WHERE id=$1 AND version=$2 AND NOT archived)", j.KnowledgeID, j.Snapshot.KnowledgeVersion).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return errors.New("ai_stale")
		}
	}
	if j.Conversation != "" {
		rows, e := tx.Query(ctx, `SELECT CASE WHEN sender_kind='visitor' THEN 'user' ELSE 'assistant' END,left(content,2500),id::text,CASE WHEN sender_kind='visitor' AND message_type='image' THEN coalesce(attachment_id::text,'') ELSE '' END FROM messages WHERE conversation_id=$1 AND audience='public' AND withdrawn_at IS NULL AND (content<>'' OR sender_kind='visitor' AND message_type='image') ORDER BY event_seq DESC LIMIT 12`, j.Conversation)
		if e != nil {
			return e
		}
		history := []*schema.Message{}
		images := []aiImageReference{}
		for rows.Next() {
			var role, text, messageID, attachment string
			if e = rows.Scan(&role, &text, &messageID, &attachment); e != nil {
				rows.Close()
				return e
			}
			if attachment != "" && len(images) < 3 {
				images = append(images, aiImageReference{Index: len(history), MessageID: messageID, AttachmentID: attachment})
			}
			history = append(history, &schema.Message{Role: schema.RoleType(role), Content: text})
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return e
		}
		for left, right := 0, len(history)-1; left < right; left, right = left+1, right-1 {
			history[left], history[right] = history[right], history[left]
		}
		for i := range images {
			images[i].Index = len(history) - 1 - images[i].Index
		}
		j.Snapshot.Images = images

		j.Snapshot.History = history
	}
	return reserveAIWallet(ctx, tx, j)
}

// aiCatalog 只载入绑定且当前可用的发布内容，目录超限时明确失败。
func (s *Service) aiCatalog(ctx context.Context, tx pgx.Tx, j *aiJobData) error {
	j.Snapshot.Knowledge = []aiKnowledge{}
	j.Snapshot.Scripts = []aiScript{}
	size := 0
	for _, id := range j.Snapshot.Bot.KnowledgeIDs {
		var k aiKnowledge
		e := tx.QueryRow(ctx, `SELECT id::text,title,description,published_body,published_version,coalesce(folder_id::text,'') FROM ai_knowledge_bases WHERE id=$1 AND enabled AND NOT archived AND published_version>0`, id).Scan(&k.ID, &k.Title, &k.Description, &k.Body, &k.Version, &k.FolderID)
		if e == pgx.ErrNoRows {
			continue
		}
		if e != nil {
			return e
		}
		size += len(k.Title) + len(k.Description) + 100
		j.Snapshot.Knowledge = append(j.Snapshot.Knowledge, k)
	}
	var directoryErr error
	j.Snapshot.Directory, directoryErr = aiBuildDirectory(ctx, tx, j.Snapshot.Knowledge)
	if directoryErr != nil {
		return directoryErr
	}
	size = len(jsonValue(j.Snapshot.Directory))
	for _, id := range j.Snapshot.Bot.ScriptIDs {
		var script aiScript
		e := tx.QueryRow(ctx, `SELECT id::text,title,description,language,version FROM ai_scripts WHERE id=$1 AND enabled AND NOT archived`, id).Scan(&script.ID, &script.Title, &script.Description, &script.Language, &script.Version)
		if e == pgx.ErrNoRows {
			continue
		}
		if e != nil {
			return e
		}
		size += len(script.Title) + len(script.Description) + 100
		j.Snapshot.Scripts = append(j.Snapshot.Scripts, script)
	}
	rows, e := tx.Query(ctx, "SELECT id::text,title,left(content,200),version,message_type FROM quick_replies WHERE enabled AND (owner_membership_id IS NULL OR owner_membership_id=$1) ORDER BY id", j.Owner)
	if e != nil {
		return e
	}
	for rows.Next() {
		var script aiScript
		var kind string
		if e = rows.Scan(&script.ID, &script.Title, &script.Description, &script.Version, &kind); e != nil {
			rows.Close()
			return e
		}
		script.QuickReply = true
		script.Language = "auto"
		script.Description = kind + ": " + script.Description
		size += len(script.Title) + len(script.Description) + 100
		j.Snapshot.Scripts = append(j.Snapshot.Scripts, script)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return e
	}
	if size > 12000 {
		return errors.New("ai_catalog_limit")
	}
	return nil
}

// failAIJob 只公开稳定分类，有未报告调用时保留预算及待核对状态。
func (s *Service) failAIJob(ctx context.Context, j *aiJobData, cause error) {
	code := cause.Error()
	var typed *platform.APIError
	if errors.As(cause, &typed) {
		code = typed.Code
	}
	if !strings.HasPrefix(code, "ai_") || len(code) > 80 {
		code = "ai_task_failed"
	}
	_ = platform.TenantTx(ctx, s.pool, j.Tenant, func(tx pgx.Tx) error {
		var status string
		if e := tx.QueryRow(ctx, "SELECT status FROM ai_jobs WHERE id=$1 FOR UPDATE", j.ID).Scan(&status); e != nil {
			return e
		}
		if status != "running" {
			return nil
		}
		var unknown bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ai_call_attempts WHERE job_id=$1 AND status IN('started','uncertain'))", j.ID).Scan(&unknown); e != nil {
			return e
		}
		terminal := "failed"
		if code == "ai_stale" {
			terminal = "cancelled"
		}
		if unknown {
			terminal = "uncertain"
		} else {
			if e := settleAIWallet(ctx, tx, j, false); e != nil {
				return e
			}
		}
		if _, e := tx.Exec(ctx, "UPDATE ai_jobs SET status=$2,error_code=$3,billing_status=CASE WHEN $2='uncertain' THEN 'uncertain' ELSE billing_status END,completed_at=now() WHERE id=$1", j.ID, terminal, code); e != nil {
			return e
		}
		if e := s.aiThinking(ctx, tx, j, false); e != nil {
			return e
		}
		if e := aiTraceTx(ctx, tx, j, "failed", code, map[string]any{"status": terminal, "error_code": code}); e != nil {
			return e
		}
		if j.Kind == "summary" {
			if _, e := tx.Exec(ctx, "UPDATE ai_pending_items SET status='pending',updated_at=now() WHERE knowledge_id=$1 AND knowledge_version<=$2 AND status='supplementing'", j.KnowledgeID, j.Snapshot.KnowledgeVersion); e != nil {
				return e
			}
			_, e := tx.Exec(ctx, "UPDATE ai_knowledge_bases SET status='failed',error_code=$3 WHERE id=$1 AND version=$2", j.KnowledgeID, j.Snapshot.KnowledgeVersion, code)
			return e
		}
		if j.Conversation != "" && code != "ai_stale" {
			return s.aiHandoff(ctx, tx, j, code)
		}
		return nil
	})
}
