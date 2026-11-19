// 文件作用说明：校验快捷回复表单定义与访客提交，冻结历史内容并以一次性消息实现幂等保存。
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"supportdesk/backend/internal/platform"
)

type formField struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Type     string `json:"type"`
}

// validateFormFields 限制字段数量、稳定编号和文本长度，表单不允许脚本或任意业务动作。
func validateFormFields(fields []formField) error {
	if len(fields) < 1 || len(fields) > 20 {
		return platform.Fail(400, "invalid_form", "表单需要1至20个字段")
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if !uuidPattern.MatchString(f.ID) || seen[f.ID] || strings.TrimSpace(f.Label) == "" || len([]rune(f.Label)) > 80 || !inStrings(f.Type, "text", "textarea") {
			return platform.Fail(400, "invalid_form", "请检查表单字段名称、类型与唯一编号")
		}
		seen[f.ID] = true
	}
	return nil
}

// prepareFormSubmission 从当前可见表单消息读取快照，拒绝未知字段、漏填和客服代填。
func prepareFormSubmission(ctx context.Context, tx pgx.Tx, a actor, conversation string, in *messageInput) (map[string]any, error) {
	if in.Type != "form_result" {
		if in.FormRequestID != "" || len(in.FormValues) > 0 {
			return nil, platform.Fail(400, "invalid_form", "表单数据必须使用表单提交消息")
		}
		return nil, nil
	}
	if a.ContactID == "" || in.Audience != "public" || in.QuickReplyID != "" || in.AttachmentID != "" || !uuidPattern.MatchString(in.FormRequestID) {
		return nil, platform.Fail(403, "form_submission_forbidden", "只有当前访客可以填写会话中的表单")
	}
	var raw []byte
	var title string
	e := tx.QueryRow(ctx, "SELECT form_fields,content FROM messages WHERE id=$1 AND conversation_id=$2 AND message_type='form' AND audience='public' AND withdrawn_at IS NULL FOR SHARE", in.FormRequestID, conversation).Scan(&raw, &title)
	if e == pgx.ErrNoRows {
		return nil, platform.Fail(404, "form_unavailable", "表单不存在或已撤回")
	}
	if e != nil {
		return nil, e
	}
	visible, e := messageVisible(ctx, tx, a, in.FormRequestID)
	if e != nil {
		return nil, e
	}
	if !visible {
		return nil, platform.Fail(403, "form_unavailable", "无权填写此表单")
	}
	var fields []formField
	if e = json.Unmarshal(raw, &fields); e != nil {
		return nil, e
	}
	if e = validateFormFields(fields); e != nil {
		return nil, e
	}
	if in.FormValues == nil {
		in.FormValues = map[string]string{}
	}
	known := map[string]bool{}
	lines := []string{title}
	total := 0
	for _, f := range fields {
		known[f.ID] = true
		value := in.FormValues[f.ID]
		if f.Required && strings.TrimSpace(value) == "" {
			return nil, platform.Fail(400, "form_required", "请填写所有必填项")
		}
		if len([]rune(value)) > 2000 {
			return nil, platform.Fail(400, "form_value_too_long", "每个表单字段最多2000字")
		}
		total += len([]rune(value))
		lines = append(lines, fmt.Sprintf("%s：%s", f.Label, value))
	}
	for key := range in.FormValues {
		if !known[key] {
			return nil, platform.Fail(400, "form_field_unknown", "提交包含不属于此表单的字段")
		}
	}
	if total > 10000 {
		return nil, platform.Fail(400, "form_value_too_long", "表单内容总长度不能超过10000字")
	}
	existing, e := txJSON(ctx, tx, "SELECT "+messageProjection+" FROM messages m WHERE form_request_id=$1", in.FormRequestID)
	if e == nil {
		var same bool
		e = tx.QueryRow(ctx, "SELECT form_values=$2::jsonb AND sender_id=$3 FROM messages WHERE form_request_id=$1", in.FormRequestID, jsonValue(in.FormValues), a.ContactID).Scan(&same)
		if e != nil {
			return nil, e
		}
		if !same {
			return nil, platform.Fail(409, "form_already_submitted", "此表单已提交，不能重复修改")
		}
		return existing, nil
	}
	if e != pgx.ErrNoRows {
		return nil, e
	}
	in.FormFields = fields
	in.Content = strings.Join(lines, "\n")
	return nil, nil
}

// validateKeywordReply 验证关键词仅引用当前成员可使用的快捷回复；公共规则只能引用公共话术。
func validateKeywordReply(ctx context.Context, tx pgx.Tx, member string, public bool, values map[string]any) error {
	raw, provided := values["quick_reply_id"]
	if !provided {
		return nil
	}
	if raw == nil || raw == "" {
		values["quick_reply_id"] = nil
		return nil
	}
	id, ok := raw.(string)
	if !ok || !uuidPattern.MatchString(id) {
		return platform.Fail(400, "invalid_quick_reply", "请选择有效快捷回复")
	}
	q, e := loadQuickReply(ctx, tx, id, "FOR SHARE")
	if e != nil || !q.Enabled || !quickReplyVisible(q, member) || (public && q.Owner != "") {
		return platform.Fail(403, "quick_reply_forbidden", "此规则不能使用所选快捷回复")
	}
	return nil
}

// formFieldsJSON 将缺失的普通消息字段规范为空数组，保持旧消息写入兼容。
func formFieldsJSON(value any) []byte {
	if value == nil {
		return jsonValue([]any{})
	}
	raw := jsonValue(value)
	if string(raw) == "null" {
		return jsonValue([]any{})
	}
	return raw
}
