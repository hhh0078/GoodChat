// 文件作用说明：为AI任务维护可恢复、最长200秒的临时输入提示，不创建聊天消息或未读记录。
package chat

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// aiThinking 在任务入队和结束事务中同步提示，仅当前任务可以清除自身状态，避免覆盖更新的问题。
// active 为true时建立固定200秒期限；辅助模式只在客服界面展示。
func (s *Service) aiThinking(ctx context.Context, tx pgx.Tx, j *aiJobData, active bool) error {
	if j.Kind != "reply" || j.Conversation == "" {
		return nil
	}
	if active {
		_, e := tx.Exec(ctx, `UPDATE conversations SET context=context||jsonb_build_object('ai_thinking',jsonb_build_object('job_id',$2::text,'expires_at',statement_timestamp()+interval '200 seconds','mode',$3::text,'name',$4::text)) WHERE id=$1 AND NOT ai_paused AND status<>'closed'`, j.Conversation, j.ID, j.Snapshot.Bot.Mode, j.Snapshot.Bot.Name)
		if e != nil {
			return e
		}
	} else {
		result, e := tx.Exec(ctx, `UPDATE conversations SET context=context-'ai_thinking' WHERE id=$1 AND context#>>'{ai_thinking,job_id}'=$2`, j.Conversation, j.ID)
		if e != nil {
			return e
		}
		if result.RowsAffected() == 0 {
			return nil
		}
	}
	_, e := s.emit(ctx, tx, j.Tenant, j.Conversation, "conversation.updated", "public", map[string]any{"id": j.Conversation})
	return e
}
