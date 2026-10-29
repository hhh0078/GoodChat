// 文件作用说明：在首次正式接待时追加个人问候，并在执行前撤销归属或规则已失效的个人自动回复。
package chat

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// enqueuePersonalWelcome 只为首次正式接待创建个人欢迎，重试和后续转接均不重复追加。
func (s *Service) enqueuePersonalWelcome(ctx context.Context, tx pgx.Tx, tenant, conversation string) error {
	var member, assignment string
	var accepted time.Time
	err := tx.QueryRow(ctx, `SELECT c.current_agent_id::text,a.id::text,a.accepted_at FROM conversations c JOIN conversation_assignments a ON a.id=c.current_assignment_id JOIN tenant_memberships m ON m.id=c.current_agent_id WHERE c.id=$1 AND c.status='active' AND a.accepted_at IS NOT NULL AND NOT m.disabled AND m.removed_at IS NULL AND (SELECT count(*) FROM conversation_assignments first WHERE first.conversation_id=c.id AND first.accepted_at IS NOT NULL)=1`, conversation).Scan(&member, &assignment, &accepted)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	rule, err := personalWelcomeRule(ctx, tx, tenant, member)
	if err != nil || rule == nil {
		return err
	}
	for index, content := range rule.Contents {
		options := append([]welcomeOption(nil), content.Options...)
		for i := range options {
			options[i].ClientID = newID()
		}
		payload := map[string]any{"content": content.Content, "message_type": content.Type, "attachment_id": content.AttachmentID, "options": options, "welcome_rule_id": rule.ID, "welcome_rule_version": rule.Version, "personal_member_id": member, "personal_assignment_id": assignment}
		_, err = tx.Exec(ctx, `INSERT INTO chat_operation_jobs(tenant_id,conversation_id,job_type,trigger_key,payload,run_after,sort_order) VALUES($1,$2,'welcome',$3,$4,$5,$6) ON CONFLICT(tenant_id,trigger_key) DO NOTHING`, tenant, conversation, "personal-welcome:"+conversation+":"+strconv.Itoa(index), jsonValue(payload), accepted.Add(time.Duration(content.DelaySeconds)*time.Second), index)
		if err != nil {
			return err
		}
	}
	return nil
}

// personalWelcomeCancelled 重查正式接待归属和当前个人规则版本，取消转接或修改后迟到的任务。
func personalWelcomeCancelled(ctx context.Context, tx pgx.Tx, tenant, conversation string, payload map[string]any) (bool, error) {
	member, _ := payload["personal_member_id"].(string)
	assignment, _ := payload["personal_assignment_id"].(string)
	valid, err := personalAutomaticMember(ctx, tx, conversation, member, assignment)
	if err != nil || !valid {
		return true, err
	}
	rule, err := personalWelcomeRule(ctx, tx, tenant, member)
	if err != nil {
		return false, err
	}
	return rule == nil || rule.ID != payload["welcome_rule_id"] || float64(rule.Version) != payload["welcome_rule_version"], nil
}

// personalAutomaticMember 仅允许未停用且仍正式接待当前会话的成员执行个人自动回复。
func personalAutomaticMember(ctx context.Context, tx pgx.Tx, conversation, member, assignment string) (bool, error) {
	if !uuidPattern.MatchString(member) || !uuidPattern.MatchString(assignment) {
		return false, nil
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations c JOIN tenant_memberships m ON m.id=c.current_agent_id AND m.tenant_id=c.tenant_id WHERE c.id=$1 AND c.status='active' AND c.current_agent_id=$2::uuid AND c.current_assignment_id=$3::uuid AND m.can_serve AND NOT m.disabled AND m.removed_at IS NULL)`, conversation, member, assignment).Scan(&valid)
	return valid, err
}
