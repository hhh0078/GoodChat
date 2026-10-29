// 文件作用说明：按最近公开消息活动提供绑定租户、身份与筛选条件的会话游标分页，兼容历史创建时间筛选。
package chat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"supportdesk/backend/internal/platform"
)

const conversationActivity = "coalesce(c.last_message_at,c.created_at)"
const conversationActivityWhere = " AND ($11::timestamptz IS NULL OR " + conversationActivity + ">=$11) AND ($12::timestamptz IS NULL OR " + conversationActivity + "<$12)"
const conversationPageWhere = " AND ($13::timestamptz IS NULL OR " + conversationActivity + "<$13 OR (" + conversationActivity + "=$13 AND c.id>NULLIF($14,'')::uuid))"

// conversationListCursor 保存稳定排序的末条位置，授权指纹防止更换租户、身份或筛选后复用游标。
type conversationListCursor struct {
	Scope string    `json:"scope"`
	At    time.Time `json:"at"`
	ID    string    `json:"id"`
}

// conversationPageOptions 校验页大小与活动日期，并生成当前请求专属的授权筛选指纹。
func conversationPageOptions(r *http.Request, a actor, timezone string) (int, *time.Time, *time.Time, string, error) {
	q := r.URL.Query()
	limit := 200
	if text := q.Get("limit"); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 100 {
			return 0, nil, nil, "", platform.Fail(400, "invalid_limit", "每页会话数需为1至100")
		}
		limit = n
	}
	from, err := filterTime(q.Get("activity_from"), timezone, false)
	if err != nil {
		return 0, nil, nil, "", err
	}
	to, err := filterTime(q.Get("activity_to"), timezone, true)
	if err != nil {
		return 0, nil, nil, "", err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return 0, nil, nil, "", platform.Fail(400, "invalid_date_range", "开始时间必须早于结束时间")
	}
	values := []string{a.TenantID, actorKind(a), actorID(a), a.Role, timezone, strconv.Itoa(limit)}
	for _, key := range []string{"status", "agent_id", "contact_id", "channel_code", "query", "from", "to", "activity_from", "activity_to"} {
		values = append(values, q.Get(key))
	}
	fingerprint := sha256.Sum256(jsonValue(values))
	return limit, from, to, base64.RawURLEncoding.EncodeToString(fingerprint[:]), nil
}

// decodeConversationCursor 验证列表专用签名与筛选授权范围，拒绝伪造及跨范围游标。
func (s *Service) decodeConversationCursor(text, scope string) (*time.Time, string, error) {
	if text == "" {
		return nil, "", nil
	}
	invalid := platform.Fail(400, "invalid_cursor", "会话分页位置无效或筛选已变化，请重新读取列表")
	if len(text) > 2048 {
		return nil, "", invalid
	}
	parts := strings.Split(text, ".")
	if len(parts) != 2 {
		return nil, "", invalid
	}
	raw, rawErr := base64.RawURLEncoding.DecodeString(parts[0])
	signature, sigErr := base64.RawURLEncoding.DecodeString(parts[1])
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write([]byte("conversation-list:v1:"))
	mac.Write(raw)
	var cursor conversationListCursor
	if rawErr != nil || sigErr != nil || !hmac.Equal(signature, mac.Sum(nil)) || json.Unmarshal(raw, &cursor) != nil || cursor.Scope != scope || cursor.At.IsZero() || !uuidPattern.MatchString(cursor.ID) {
		return nil, "", invalid
	}
	return &cursor.At, cursor.ID, nil
}

// encodeConversationCursor 用本页末条真实活动位置签名，避免时间并列或活动移动时采用不稳定的偏移分页。
func (s *Service) encodeConversationCursor(item map[string]any, scope string) (string, error) {
	value, _ := item["last_message_at"].(string)
	if value == "" {
		value, _ = item["created_at"].(string)
	}
	at, err := time.Parse(time.RFC3339Nano, value)
	id, ok := item["id"].(string)
	if err != nil || !ok || !uuidPattern.MatchString(id) {
		return "", fmt.Errorf("会话分页记录缺少有效活动位置")
	}
	raw := jsonValue(conversationListCursor{Scope: scope, At: at, ID: id})
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write([]byte("conversation-list:v1:"))
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
