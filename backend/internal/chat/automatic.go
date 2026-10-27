// 文件作用说明：以持久任务执行欢迎语和关键词自动回复，防止刷新重复和规则自触发。
package chat

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// enqueueWelcome 为一次独立咨询仅创建一个欢迎任务。
func (s *Service) enqueueWelcome(ctx context.Context, tx pgx.Tx, tenant, conversation string) error {
	settings, err := ensureSettings(ctx, tx, tenant)
	if err != nil {
		return err
	}
	if enabled, _ := settings["welcome_enabled"].(bool); !enabled {
		return nil
	}
	if matched, err := s.enqueueRichWelcome(ctx, tx, tenant, conversation); matched || err != nil {
		return err
	}
	text, _ := settings["welcome_text"].(string)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	delay, _ := settings["welcome_delay_seconds"].(float64)
	config, err := loadAutomationConfig(ctx, tx, tenant)
	if err != nil {
		return err
	}
	payload := map[string]any{"content": text, "settings_hash": hash(string(jsonValue(settings))), "stop_on_agent": config.StopWelcomeOnAgent, "policy_hash": welcomePolicyHash(config)}
	_, err = tx.Exec(ctx, "INSERT INTO chat_operation_jobs(tenant_id,conversation_id,job_type,trigger_key,payload,run_after) SELECT $1,$2,'welcome',$3,$4,created_at+$5*interval '1 second' FROM conversations WHERE id=$2 ON CONFLICT(tenant_id,trigger_key) DO NOTHING", tenant, conversation, "welcome:"+conversation, jsonValue(payload), delay)
	return err
}

// enqueueKeyword 只为访客新文字选择一条最高优先级规则并检查冷却。
func (s *Service) enqueueKeyword(ctx context.Context, tx pgx.Tx, tenant, conversation, message, content string) error {
	settings, err := ensureSettings(ctx, tx, tenant)
	if err != nil {
		return err
	}
	if enabled, _ := settings["keywords_enabled"].(bool); !enabled {
		return nil
	}
	rows, err := tx.Query(ctx, "SELECT id::text,match_mode,pattern,reply,cooldown_seconds,updated_at FROM keyword_rules WHERE enabled ORDER BY priority DESC,id")
	if err != nil {
		return err
	}
	type rule struct {
		id, mode, pattern, reply string
		cooldown                 int
		updated                  time.Time
	}
	rules := []rule{}
	for rows.Next() {
		var item rule
		if err := rows.Scan(&item.id, &item.mode, &item.pattern, &item.reply, &item.cooldown, &item.updated); err != nil {
			rows.Close()
			return err
		}
		rules = append(rules, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	text := strings.ToLower(content)
	for _, rule := range rules {
		pattern := strings.ToLower(rule.pattern)
		if rule.mode == "exact" && text != pattern || rule.mode == "contains" && !strings.Contains(text, pattern) {
			continue
		}
		var cooling bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM chat_operation_jobs WHERE conversation_id=$1 AND rule_id=$2 AND status IN('queued','completed') AND created_at>now()-$3*interval '1 second')", conversation, rule.id, rule.cooldown).Scan(&cooling); err != nil {
			return err
		}
		if cooling {
			return nil
		}
		payload := map[string]any{"content": rule.reply, "rule_updated_at": rule.updated.UTC().Format(time.RFC3339Nano)}
		_, err := tx.Exec(ctx, "INSERT INTO chat_operation_jobs(tenant_id,conversation_id,job_type,trigger_key,rule_id,payload) VALUES($1,$2,'keyword',$3,$4,$5) ON CONFLICT(tenant_id,trigger_key) DO NOTHING", tenant, conversation, "keyword:"+message, rule.id, jsonValue(payload))
		return err
	}
	return nil
}

// runAutomatic 在同一数据库事务中保存系统消息并完成任务，崩溃不会形成半份回复。
func (s *Service) runAutomatic(ctx context.Context, tenant string) error {
	return platform.TenantTx(ctx, s.pool, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT id::text FROM chat_operation_jobs WHERE status='queued' AND run_after<=now() ORDER BY run_after,sort_order,id LIMIT 20 FOR UPDATE SKIP LOCKED")
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range ids {
			if err := s.executeAutomatic(ctx, tx, tenant, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// executeAutomatic 重查咨询、封禁和规则版本后执行一条任务。
func (s *Service) executeAutomatic(ctx context.Context, tx pgx.Tx, tenant, job string) error {
	var conversation, kind string
	var rule *string
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT conversation_id::text,job_type,rule_id::text,payload FROM chat_operation_jobs WHERE id=$1", job).Scan(&conversation, &kind, &rule, &raw); err != nil {
		return err
	}
	var status, contact string
	if err := tx.QueryRow(ctx, "SELECT status,contact_id::text FROM conversations WHERE id=$1 FOR UPDATE", conversation).Scan(&status, &contact); err != nil {
		return err
	}
	payload := decodeJSONMap(raw)
	cancel := status == "closed"
	if err := blocked(ctx, tx, contact); err != nil {
		if _, ok := err.(*platform.APIError); ok {
			cancel = true
		} else {
			return err
		}
	}
	settings, err := ensureSettings(ctx, tx, tenant)
	if err != nil {
		return err
	}
	if kind == "welcome" {
		enabled, _ := settings["welcome_enabled"].(bool)
		cancel = cancel || !enabled
		if _, rich := payload["welcome_rule_id"].(string); rich {
			invalid, err := richWelcomeCancelled(ctx, tx, tenant, conversation, payload)
			if err != nil {
				return err
			}
			cancel = cancel || invalid
		} else {
			cancel = cancel || payload["settings_hash"] != hash(string(jsonValue(settings)))
			config, err := loadAutomationConfig(ctx, tx, tenant)
			if err != nil {
				return err
			}
			if policy, exists := payload["policy_hash"]; exists {
				cancel = cancel || policy != welcomePolicyHash(config)
			}
			if config.StopWelcomeOnAgent {
				var accepted bool
				if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM conversation_assignments WHERE conversation_id=$1 AND accepted_at IS NOT NULL)", conversation).Scan(&accepted); err != nil {
					return err
				}
				cancel = cancel || accepted
			}
		}
	} else {
		enabled, _ := settings["keywords_enabled"].(bool)
		cancel = cancel || !enabled
		var current time.Time
		var ruleEnabled bool
		if rule == nil {
			cancel = true
		} else if err := tx.QueryRow(ctx, "SELECT enabled,updated_at FROM keyword_rules WHERE id=$1", *rule).Scan(&ruleEnabled, &current); err != nil {
			if err == pgx.ErrNoRows {
				cancel = true
			} else {
				return err
			}
		} else {
			cancel = cancel || !ruleEnabled || payload["rule_updated_at"] != current.UTC().Format(time.RFC3339Nano)
		}
	}
	if cancel {
		_, err := tx.Exec(ctx, "UPDATE chat_operation_jobs SET status='cancelled',completed_at=now(),attempts=attempts+1 WHERE id=$1", job)
		return err
	}
	if kind == "welcome" {
		var earlier bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM chat_operation_jobs earlier JOIN chat_operation_jobs current ON current.id=$2 WHERE earlier.conversation_id=$1 AND earlier.job_type='welcome' AND earlier.status='queued' AND (earlier.run_after,earlier.sort_order,earlier.id)<(current.run_after,current.sort_order,current.id))", conversation, job).Scan(&earlier); err != nil {
			return err
		}
		if earlier {
			return nil
		}
	}
	message := newID()
	content, _ := payload["content"].(string)
	messageType, _ := payload["message_type"].(string)
	if messageType != "image" {
		messageType = "text"
	}
	attachment, _ := payload["attachment_id"].(string)
	options := payload["options"]
	if options == nil {
		options = []any{}
	}
	sourceRule := ""
	sourceVersion, _ := payload["settings_hash"].(string)
	if id, ok := payload["welcome_rule_id"].(string); ok {
		sourceRule = id
		if version, ok := payload["welcome_rule_version"].(float64); ok {
			sourceVersion = strconv.FormatFloat(version, 'f', 0, 64)
		}
	} else if rule != nil {
		sourceRule = *rule
		sourceVersion, _ = payload["rule_updated_at"].(string)
	}
	event := map[string]any{"id": message, "client_message_id": job, "sender_kind": "system", "sender_id": tenant, "sender_name": "系统", "message_type": messageType, "content": content, "audience": "public", "source": kind, "created_at": nowUTC()}
	seq, err := s.emit(ctx, tx, tenant, conversation, "message.created", "public", event)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO messages(id,tenant_id,conversation_id,sender_id,sender_kind,client_message_id,message_type,content,audience,event_seq,attachment_id,options,source,source_rule_id,source_version) VALUES($1,$2,$3,$2,'system',$4,$5,$6,'public',$7,NULLIF($8,'')::uuid,$9,$10,NULLIF($11,'')::uuid,$12)", message, tenant, conversation, job, messageType, content, seq, attachment, jsonValue(options), kind, sourceRule, sourceVersion); err != nil {
		return err
	}
	projection, err := txJSON(ctx, tx, "SELECT "+messageProjection+" FROM messages m WHERE id=$1", message)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE conversation_events SET payload=$2 WHERE conversation_id=$1 AND seq=$3", conversation, jsonValue(projection), seq); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE chat_operation_jobs SET status='completed',completed_at=now(),attempts=attempts+1 WHERE id=$1", job)
	return err
}

// publicSettings 仅公开访客渲染确实需要的能力开关。
func publicSettings(ctx context.Context, tx pgx.Tx, tenant string) (map[string]any, error) {
	settings, err := ensureSettings(ctx, tx, tenant)
	if err != nil {
		return nil, err
	}
	return map[string]any{"typing_enabled": settings["typing_enabled"]}, nil
}

// decodeRulePayload 读取可追踪的任务JSON，保留扩展点而不执行模型或脚本。
func decodeRulePayload(raw []byte) (map[string]any, error) {
	var data map[string]any
	err := json.Unmarshal(raw, &data)
	return data, err
}
