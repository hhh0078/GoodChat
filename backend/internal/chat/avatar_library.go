// 文件作用说明：定义50个平台头像、校验租户素材引用并在访客首次创建时从租户池随机选择稳定头像。
package chat

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// avatarAsset 只向浏览器公开展示元数据，不包含原始文件、租户或上传者内部字段。
type avatarAsset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	URL         string `json:"url"`
	Builtin     bool   `json:"builtin"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int    `json:"size_bytes,omitempty"`
}

// builtinAvatars 返回固定平台库，职业20张、卡通20张和萌趣插画10张，不生成模拟访客资料。
func builtinAvatars() []avatarAsset {
	items := make([]avatarAsset, 0, 50)
	for index := 1; index <= 50; index++ {
		category, name := "professional", "职业"
		if index > 20 {
			category, name = "cartoon", "卡通"
		}
		if index > 40 {
			category, name = "illustration", "萌趣插画"
		}
		id := fmt.Sprintf("default-%02d", index)
		items = append(items, avatarAsset{ID: id, Name: fmt.Sprintf("%s头像 %02d", name, index), Category: category, URL: platform.AvatarURL(id), Builtin: true})
	}
	return items
}

// validateAvatarRefs 校验平台编号或当前租户上传的素材，不能引用其他租户资源或任意外链。
// refs 为去重后的头像引用，允许空数组表示恢复平台默认库。
func validateAvatarRefs(ctx context.Context, tx pgx.Tx, refs []string) error {
	if len(refs) > 500 {
		return platform.Fail(400, "avatar_pool_too_large", "头像池最多选择500张图片")
	}
	for _, ref := range refs {
		if platform.AvatarURL(ref) == "" {
			return platform.Fail(400, "invalid_avatar_ref", "头像引用无效，请从平台库或本租户上传素材选择")
		}
		if len(ref) >= 8 && ref[:8] == "default-" {
			continue
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM avatar_uploads WHERE id=$1)", ref).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return platform.Fail(400, "avatar_not_available", "头像不存在或不属于当前租户")
		}
	}
	return nil
}

// avatarPoolRefs 读取当前租户已保存的头像池，未配置或显式清空时返回完整平台默认库。
func avatarPoolRefs(ctx context.Context, tx pgx.Tx, tenant string) ([]string, bool, error) {
	if _, err := ensureSettings(ctx, tx, tenant); err != nil {
		return nil, false, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT coalesce(settings->'visitor_avatar_pool','[]'::jsonb) FROM chat_settings WHERE tenant_id=$1", tenant).Scan(&raw); err != nil {
		return nil, false, err
	}
	refs := []string{}
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, false, platform.Fail(409, "invalid_avatar_pool", "租户头像池配置无效，请重新保存")
	}
	if len(refs) > 0 {
		return refs, false, nil
	}
	for _, item := range builtinAvatars() {
		refs = append(refs, item.ID)
	}
	return refs, true, nil
}

// defaultVisitorAvatar 随机选择一次头像地址，由客户创建事务持久化，不在刷新或心跳中重新分配。
func defaultVisitorAvatar(ctx context.Context, tx pgx.Tx, tenant string) (string, error) {
	refs, _, err := avatarPoolRefs(ctx, tx, tenant)
	if err != nil {
		return "", err
	}
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(refs))))
	if err != nil {
		return "", err
	}
	url := platform.AvatarURL(refs[index.Int64()])
	if url == "" {
		return "", platform.Fail(409, "invalid_avatar_pool", "租户头像池配置无效，请重新保存")
	}
	return url, nil
}
