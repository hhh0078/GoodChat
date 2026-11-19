// 文件作用说明：向平台文章任务提供既有已启用模型的运行快照，不建立额外模型密钥配置。
package chat

import (
	"context"
	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// PlatformTextModel 只在服务端解密当前启用模型，禁用后新文章任务不能继续调用。
func (s *Service) PlatformTextModel(ctx context.Context) (platform.TextModel, error) {
	var result platform.TextModel
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		model, err := s.loadAIModel(ctx, tx, "", 0)
		if err != nil {
			return err
		}
		result = platform.TextModel{BaseURL: model.BaseURL, Model: model.Model, APIKey: model.APIKey, MaxInput: model.MaxInput, MaxOutput: model.MaxOutput}
		return nil
	})
	return result, err
}
