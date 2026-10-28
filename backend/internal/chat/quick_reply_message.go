// 文件作用说明：在消息事务内验证快捷回复版本、冻结内容和选项，并原子累计当前成员首次成功发送次数。
package chat

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// messageInput 定义真实发送请求，快捷回复只允许传入来源编号与版本，内容由服务端加载。
type messageInput struct {
	ClientID          string          `json:"client_message_id"`
	Content           string          `json:"content"`
	Type              string          `json:"message_type"`
	Audience          string          `json:"audience"`
	AttachmentID      string          `json:"attachment_id"`
	ReplyID           string          `json:"reply_to_id"`
	QuickReplyID      string          `json:"quick_reply_id"`
	QuickReplyVersion string          `json:"quick_reply_version"`
	Options           []welcomeOption `json:"-"`
}

// prepareQuickReplyMessage 在会话授权与幂等恢复之后读取当前可见版本，不允许客户端替换公共媒体。
func prepareQuickReplyMessage(ctx context.Context, tx pgx.Tx, a actor, input *messageInput) error {
	if input.QuickReplyID == "" {
		return nil
	}
	if a.ContactID != "" || input.Audience != "public" {
		return platform.Fail(403, "quick_reply_forbidden", "只有接待客服可以发送公开快捷回复")
	}
	item, err := loadQuickReply(ctx, tx, input.QuickReplyID, "FOR SHARE")
	if err != nil {
		return err
	}
	if !item.Enabled || !quickReplyVisible(item, a.MemberID) {
		return platform.Fail(404, "quick_reply_not_found", "话术已停用或当前无权使用")
	}
	if input.QuickReplyVersion != strconv.FormatInt(item.Version, 10) {
		return platform.Fail(409, "quick_reply_version_conflict", "话术已修改，本次尚未提交；请重新选择最新话术发送")
	}
	input.Content = item.Content
	input.Type = item.Type
	input.AttachmentID = item.AttachmentID
	input.Options = append([]welcomeOption{}, item.Options...)
	for index := range input.Options {
		input.Options[index].ClientID = newID()
	}
	return nil
}

// recordQuickReplyUse 仅在首次消息落库事务内累计使用，回滚或原编号重复返回不会增加次数。
func recordQuickReplyUse(ctx context.Context, tx pgx.Tx, a actor, id string) error {
	if id == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO quick_reply_usage(tenant_id,membership_id,quick_reply_id) VALUES($1,$2,$3) ON CONFLICT(tenant_id,membership_id,quick_reply_id) DO UPDATE SET use_count=quick_reply_usage.use_count+1,last_used_at=now()`, a.TenantID, a.MemberID, id)
	return err
}
