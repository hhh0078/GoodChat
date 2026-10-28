// 文件作用说明：管理富快捷回复、可见媒体授权及当前成员的成功使用次数，发送时冻结话术版本。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

const quickReplyProjection = `to_jsonb(q)||jsonb_build_object('version',q.version::text,'scope',CASE WHEN q.owner_membership_id IS NULL THEN 'public' ELSE 'personal' END,'use_count',coalesce(usage.use_count,0),'attachment',(SELECT to_jsonb(f)-'storage_key'-'sha256' FROM attachments f WHERE f.id=q.attachment_id AND f.tenant_id=q.tenant_id))`

// quickReply 保存可版本化的已授权话术，选项发送编号不写回配置。
type quickReply struct {
	ID, Owner, Title, Content, Type, AttachmentID, Shortcut, Category string
	Options                                                           []welcomeOption
	Enabled                                                           bool
	Sort                                                              int32
	Version                                                           int64
}

// quickReplyInput 区分局部更新与空值，版本作为十进制字符串避免浏览器精度损失。
type quickReplyInput struct {
	Title        *string          `json:"title"`
	Content      *string          `json:"content"`
	Type         *string          `json:"message_type"`
	AttachmentID *string          `json:"attachment_id"`
	Options      *[]welcomeOption `json:"options"`
	Scope        *string          `json:"scope"`
	Shortcut     *string          `json:"shortcut"`
	Category     *string          `json:"category"`
	Enabled      *bool            `json:"enabled"`
	Sort         *int32           `json:"sort"`
	Version      *string          `json:"version"`
}

// loadQuickReply 在当前租户事务内读取配置，锁参数仅由固定服务端调用点选择。
func loadQuickReply(ctx context.Context, tx pgx.Tx, id, lock string) (quickReply, error) {
	item := quickReply{}
	var options []byte
	err := tx.QueryRow(ctx, `SELECT id::text,coalesce(owner_membership_id::text,''),title,content,message_type,coalesce(attachment_id::text,''),options,enabled,sort,version,shortcut,category FROM quick_replies WHERE id=$1 `+lock, id).Scan(&item.ID, &item.Owner, &item.Title, &item.Content, &item.Type, &item.AttachmentID, &options, &item.Enabled, &item.Sort, &item.Version, &item.Shortcut, &item.Category)
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal(options, &item.Options); err != nil {
		return item, err
	}
	return item, nil
}

// quickReplyVisible 仅允许公共话术或当前成员自己的话术，不把管理员权限扩大为读取其他个人话术。
func quickReplyVisible(item quickReply, member string) bool {
	return item.Owner == "" || item.Owner == member
}

// validateQuickReply 校验真实媒体类型和选项，并限制新增媒体为本人上传或原话术已经授权的附件。
func validateQuickReply(ctx context.Context, tx pgx.Tx, a actor, item, previous quickReply) error {
	if strings.TrimSpace(item.Title) == "" || len([]rune(item.Title)) > 100 || len([]rune(item.Content)) > 10000 || !inStrings(item.Type, "text", "image", "audio", "file") || len([]rune(item.Shortcut)) > 100 || len([]rune(item.Category)) > 100 {
		return platform.Fail(400, "invalid_quick_reply", "请检查100字内标题、10000字内正文及消息类型")
	}
	if item.Type == "text" {
		if item.AttachmentID != "" || strings.TrimSpace(item.Content) == "" && len(item.Options) == 0 {
			return platform.Fail(400, "invalid_quick_reply", "文字回复需填写内容或选项，不能携带媒体附件")
		}
	} else {
		if !uuidPattern.MatchString(item.AttachmentID) || len(item.Options) > 0 {
			return platform.Fail(400, "invalid_quick_reply", "媒体回复需选择有效附件，不能携带文字选项")
		}
		var mime, uploader, kind string
		if err := tx.QueryRow(ctx, "SELECT content_type,uploader_id::text,uploader_kind FROM attachments WHERE id=$1", item.AttachmentID).Scan(&mime, &uploader, &kind); err != nil {
			return platform.Fail(403, "quick_reply_attachment_forbidden", "附件不存在或不属于当前租户")
		}
		if kind != "agent" || uploader != a.MemberID && !(previous.ID != "" && previous.AttachmentID == item.AttachmentID && quickReplyVisible(previous, a.MemberID)) {
			return platform.Fail(403, "quick_reply_attachment_forbidden", "只能选择本人上传或原话术已授权的附件")
		}
		if item.Type == "image" && !strings.HasPrefix(mime, "image/") || item.Type == "audio" && !strings.HasPrefix(mime, "audio/") && mime != "video/webm" {
			return platform.Fail(400, "attachment_type_mismatch", "附件实际类型与快捷回复类型不符")
		}
	}
	if len(item.Options) > 8 {
		return platform.Fail(400, "invalid_quick_reply_options", "最多配置8个选项")
	}
	seen := map[string]bool{}
	for _, option := range item.Options {
		if len(option.ID) < 1 || len(option.ID) > 64 || seen[option.ID] || strings.TrimSpace(option.Label) == "" || len([]rune(option.Label)) > 80 || strings.TrimSpace(option.Value) == "" || len([]rune(option.Value)) > 1000 || option.ClientID != "" {
			return platform.Fail(400, "invalid_quick_reply_options", "选项编号须唯一，标题最多80字、发送值最多1000字，消息编号由服务端生成")
		}
		seen[option.ID] = true
	}
	return nil
}

// quickReplies 复用既有路由处理富话术增删改查，使用统计始终只返回当前成员自己的值。
func (s *Service) quickReplies(w http.ResponseWriter, r *http.Request) {
	a := memberActor(r)
	var input quickReplyInput
	if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete && r.ContentLength != 0 {
		if err := platform.Decode(w, r, &input); err != nil {
			replyError(w, r, err)
			return
		}
	}
	var result any
	err := platform.TenantTx(r.Context(), s.pool, a.TenantID, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := liveMember(ctx, tx, a); err != nil {
			return err
		}
		if r.Method == http.MethodGet {
			rows, err := tx.Query(ctx, "SELECT "+quickReplyProjection+" FROM quick_replies q LEFT JOIN quick_reply_usage usage ON usage.tenant_id=q.tenant_id AND usage.quick_reply_id=q.id AND usage.membership_id=$1 WHERE q.owner_membership_id IS NULL OR q.owner_membership_id=$1 ORDER BY (q.owner_membership_id IS NULL),coalesce(usage.use_count,0) DESC,q.created_at DESC,q.id", a.MemberID)
			if err != nil {
				return err
			}
			items, err := jsonRows(rows)
			result = map[string]any{"items": items}
			return err
		}
		id := r.PathValue("id")
		item := quickReply{ID: newID(), Owner: a.MemberID, Type: "text", Enabled: true, Version: 1, Options: []welcomeOption{}}
		previous := quickReply{}
		if r.Method != http.MethodPost {
			if !uuidPattern.MatchString(id) {
				return platform.Fail(400, "invalid_id", "话术编号格式不正确")
			}
			var err error
			item, err = loadQuickReply(ctx, tx, id, "FOR UPDATE")
			if err != nil {
				return err
			}
			previous = item
			if !quickReplyVisible(item, a.MemberID) || item.Owner == "" && !isAdmin(a) {
				return platform.Fail(403, "reply_not_owned", "无权修改该话术")
			}
			if input.Version != nil && *input.Version != strconv.FormatInt(item.Version, 10) {
				return platform.Fail(409, "quick_reply_version_conflict", "话术已被修改，请重新读取后保存")
			}
		}
		if r.Method == http.MethodDelete {
			if _, err := tx.Exec(ctx, "UPDATE quick_replies SET enabled=false,version=version+1,updated_at=now() WHERE id=$1", id); err != nil {
				return err
			}
			result = map[string]any{"id": id, "enabled": false}
			return audit(ctx, tx, a, "quick-replies.disabled", id, nil)
		}
		if input.Title != nil {
			item.Title = *input.Title
		}
		if input.Content != nil {
			item.Content = *input.Content
		}
		if input.Type != nil {
			item.Type = *input.Type
		}
		if input.AttachmentID != nil {
			item.AttachmentID = *input.AttachmentID
		}
		if input.Options != nil {
			item.Options = *input.Options
		}
		if input.Shortcut != nil {
			item.Shortcut = *input.Shortcut
		}
		if input.Category != nil {
			item.Category = *input.Category
		}
		if input.Enabled != nil {
			item.Enabled = *input.Enabled
		}
		if input.Sort != nil {
			item.Sort = *input.Sort
		}
		if input.Scope != nil {
			if !inStrings(*input.Scope, "personal", "public") {
				return platform.Fail(400, "invalid_scope", "话术范围应为个人或公共")
			}
			if *input.Scope == "public" {
				if !isAdmin(a) {
					return platform.Fail(403, "public_reply_forbidden", "公共话术需要管理员维护")
				}
				item.Owner = ""
			} else {
				item.Owner = a.MemberID
			}
		}
		if err := validateQuickReply(ctx, tx, a, item, previous); err != nil {
			return err
		}
		if item.Options == nil {
			item.Options = []welcomeOption{}
		}
		if r.Method == http.MethodPost {
			_, err := tx.Exec(ctx, `INSERT INTO quick_replies(id,tenant_id,owner_membership_id,title,content,message_type,attachment_id,options,shortcut,category,enabled,sort) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,NULLIF($7,'')::uuid,$8,$9,$10,$11,$12)`, item.ID, a.TenantID, item.Owner, item.Title, item.Content, item.Type, item.AttachmentID, jsonValue(item.Options), item.Shortcut, item.Category, item.Enabled, item.Sort)
			if err != nil {
				return err
			}
		} else {
			_, err := tx.Exec(ctx, `UPDATE quick_replies SET owner_membership_id=NULLIF($2,'')::uuid,title=$3,content=$4,message_type=$5,attachment_id=NULLIF($6,'')::uuid,options=$7,shortcut=$8,category=$9,enabled=$10,sort=$11,version=version+1,updated_at=now() WHERE id=$1`, item.ID, item.Owner, item.Title, item.Content, item.Type, item.AttachmentID, jsonValue(item.Options), item.Shortcut, item.Category, item.Enabled, item.Sort)
			if err != nil {
				return err
			}
		}
		var err error
		result, err = txJSON(ctx, tx, "SELECT "+quickReplyProjection+" FROM quick_replies q LEFT JOIN quick_reply_usage usage ON usage.tenant_id=q.tenant_id AND usage.quick_reply_id=q.id AND usage.membership_id=$2 WHERE q.id=$1", item.ID, a.MemberID)
		if err != nil {
			return err
		}
		return audit(ctx, tx, a, "quick-replies.saved", item.ID, map[string]any{"message_type": item.Type})
	})
	if err != nil {
		replyError(w, r, err)
		return
	}
	platform.JSON(w, 200, result)
}
