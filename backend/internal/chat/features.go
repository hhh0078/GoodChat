// 文件作用说明：将套餐权益注入新咨询、入口和标签写入校验，保留历史读取与现有咨询的消息通道。
package chat

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// SetFeatureChecker 设置服务端套餐权益检查器，应在HTTP服务启动前完成装配。
func (s *Service) SetFeatureChecker(checker func(context.Context, pgx.Tx, string, string) error) {
	s.featureChecker = checker
}

// checkFeature 在当前租户事务内校验新业务权益，单独使用聊天模块时保留显式装配能力。
func (s *Service) checkFeature(ctx context.Context, tx pgx.Tx, tenantID, feature string) error {
	if s.featureChecker != nil {
		return s.featureChecker(ctx, tx, tenantID, feature)
	}
	return nil
}
