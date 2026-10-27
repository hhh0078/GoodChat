// 文件作用说明：使用独立迁移连接执行版本化SQL，生产数据库变更由本命令显式触发。
package main

import (
	"context"
	"log"
	"time"

	"supportdesk/backend/internal/database"
	"supportdesk/backend/internal/platform"
)

// main 读取本项目配置并执行迁移，不打印连接凭证。
func main() {
	cfg, err := platform.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if cfg.MigrationURL == "" {
		log.Fatal("请提供MIGRATION_DATABASE_URL，不使用应用账号自动提权")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err = database.Migrate(ctx, cfg.MigrationURL); err != nil {
		log.Fatal(err)
	}
	log.Print("数据库迁移已完成")
}
