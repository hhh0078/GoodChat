// Package database 管理经过版本和摘要校验的PostgreSQL迁移，使用独立的迁移连接。
package database

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate 按序执行尚未应用的迁移，已有迁移摘要变化时停止。
func Migrate(ctx context.Context, url string) error {
	connection, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("连接迁移数据库: %w", err)
	}
	defer connection.Close(ctx)
	if _, err = connection.Exec(ctx, "SELECT pg_advisory_lock(718420261026)"); err != nil {
		return err
	}
	defer connection.Exec(ctx, "SELECT pg_advisory_unlock(718420261026)")
	if _, err = connection.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now()); COMMENT ON TABLE schema_migrations IS '数据库迁移执行记录'; COMMENT ON COLUMN schema_migrations.version IS '迁移文件名称'; COMMENT ON COLUMN schema_migrations.checksum IS '迁移文件摘要'; COMMENT ON COLUMN schema_migrations.applied_at IS '迁移完成时间';`); err != nil {
		return err
	}
	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, name := range files {
		body, readErr := migrationFiles.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(body))
		var stored string
		lookupErr := connection.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", name).Scan(&stored)
		if lookupErr == nil {
			if stored != digest {
				return fmt.Errorf("迁移已变更，拒绝继续: %s", name)
			}
			continue
		}
		if lookupErr != pgx.ErrNoRows {
			return lookupErr
		}
		if err = pgx.BeginTxFunc(ctx, connection, pgx.TxOptions{}, func(tx pgx.Tx) error {
			if _, executeErr := tx.Exec(ctx, string(body)); executeErr != nil {
				return executeErr
			}
			_, insertErr := tx.Exec(ctx, "INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", name, digest)
			return insertErr
		}); err != nil {
			return fmt.Errorf("执行迁移 %s: %w", name, err)
		}
	}
	return nil
}
