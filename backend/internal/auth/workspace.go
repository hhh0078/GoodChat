// 文件作用说明：统一约束一个账号只能拥有一个有效工作台，保留已停用成员的历史审计关系。
package auth

import (
	"context"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// ensureSingleWorkspace 使用邮箱身份锁串行校验，禁止邀请、开通或启用账号到另一工作台。
// tenantID 为空时表示新开工作台；同一租户的幂等操作及已经停用的历史关系不受影响。
func ensureSingleWorkspace(ctx context.Context, tx pgx.Tx, email, tenantID string) error {
	if err := lockIdentity(ctx, tx, email); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE u.email=$1 AND NOT m.disabled AND m.removed_at IS NULL AND m.tenant_id::text<>$2)`, email, tenantID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return platform.Fail(409, "workspace_exists", "该账号已有工作台，请使用现有工作台并通过接入链接区分业务")
	}
	return nil
}
