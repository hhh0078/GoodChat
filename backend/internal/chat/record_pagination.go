// 文件作用说明：为管理入口、AI用量和账本提供绑定身份与资源的稳定创建时间游标分页。
package chat

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"supportdesk/backend/internal/platform"
)

type recordPage struct {
	limit  int
	scope  string
	before *time.Time
	id     string
}

// recordPageOptions 复用既有签名游标，使用独立资源指纹阻止跨租户、成员或列表复用。
func (s *Service) recordPageOptions(r *http.Request, a actor, fallback int) (recordPage, error) {
	p := recordPage{limit: fallback}
	if value := r.URL.Query().Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 {
			return p, platform.Fail(400, "invalid_limit", "每页记录数需为1至100")
		}
		p.limit = n
	}
	hash := sha256.Sum256(jsonValue([]any{"records:v1", r.URL.Path, a.TenantID, a.MemberID, a.Role, p.limit}))
	p.scope = base64.RawURLEncoding.EncodeToString(hash[:])
	var err error
	p.before, p.id, err = s.decodeConversationCursor(r.URL.Query().Get("cursor"), p.scope)
	if err != nil {
		return p, platform.Fail(400, "invalid_cursor", "分页位置无效，请刷新后重试。")
	}
	return p, nil
}

// query 为受控静态表别名追加参数绑定，时间相同时按编号升序保证无重复和遗漏。
func (p recordPage) query(sql string, args []any, alias string) (string, []any) {
	n := len(args)
	sql += fmt.Sprintf(" AND ($%d::timestamptz IS NULL OR %s.created_at<$%d OR (%s.created_at=$%d AND %s.id>NULLIF($%d,'')::uuid)) ORDER BY %s.created_at DESC,%s.id LIMIT $%d", n+1, alias, n+1, alias, n+1, alias, n+2, alias, alias, n+3)
	return sql, append(args, p.before, p.id, p.limit+1)
}

// recordPageResult 截去探测用多余记录，仅在存在下一页时签发末条真实位置。
func (s *Service) recordPageResult(items []map[string]any, p recordPage) (map[string]any, error) {
	more := len(items) > p.limit
	next := ""
	if more {
		items = items[:p.limit]
		last := items[len(items)-1]
		var err error
		next, err = s.encodeConversationCursor(map[string]any{"id": last["id"], "created_at": last["created_at"]}, p.scope)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"items": items, "has_more": more, "next_cursor": next}, nil
}
