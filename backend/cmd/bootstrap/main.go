// 文件作用说明：用显式迁移权限一次性初始化平台管理员，可选创建浏览器验收租户，随机口令仅写私有文件。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportdesk/backend/internal/auth"
	"supportdesk/backend/internal/billing"
	"supportdesk/backend/internal/platform"
)

// main 解析初始化目标，拒绝覆盖既有账号或重复创建平台管理员。
func main() {
	email := flag.String("email", "", "平台管理员邮箱")
	name := flag.String("name", "平台管理员", "账号显示名称")
	tenant := flag.String("test-tenant", "", "可选的本地验收租户名称")
	output := flag.String("credentials-file", "", "保存随机口令的私有JSON文件")
	flag.Parse()
	parsed, err := mail.ParseAddress(*email)
	if err != nil || parsed.Address != *email || len([]rune(*name)) < 1 || len([]rune(*name)) > 80 {
		log.Fatal("请提供有效邮箱与显示名称")
	}
	cfg, err := platform.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if cfg.MigrationURL == "" {
		log.Fatal("首次初始化需要独立迁移账号")
	}
	if cfg.Production && *tenant != "" {
		log.Fatal("生产环境不创建验收租户")
	}
	if *output == "" {
		*output = filepath.Join(cfg.DataDir, "bootstrap-account.json")
	}
	if _, err = os.Stat(*output); !os.IsNotExist(err) {
		log.Fatal("凭证文件已存在或不可访问，不会覆盖")
	}
	secret, err := platform.Token()
	if err != nil {
		log.Fatal("随机口令生成失败")
	}
	hash, err := auth.HashPassword(secret)
	if err != nil {
		log.Fatal("口令保护失败")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.MigrationURL)
	if err != nil {
		log.Fatal("初始化连接配置失败")
	}
	defer pool.Close()
	var userID, tenantID string
	err = pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(761130009)"); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE is_platform_admin OR email=$1)", strings.ToLower(*email)).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return errors.New("平台管理员或目标邮箱已存在，拒绝覆盖；请使用现有账号")
		}
		if err := tx.QueryRow(ctx, "INSERT INTO users(email,display_name,password_hash,is_platform_admin)VALUES($1,$2,$3,true)RETURNING id::text", strings.ToLower(*email), *name, hash).Scan(&userID); err != nil {
			return err
		}
		if *tenant == "" {
			return nil
		}
		if len([]rune(*tenant)) > 100 {
			return errors.New("验收租户名称过长")
		}
		slug, err := platform.Token()
		if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, "INSERT INTO tenants(name,slug)VALUES($1,$2)RETURNING id::text", *tenant, "qa-"+strings.ToLower(slug[:12])).Scan(&tenantID); err != nil {
			return err
		}
		plans := billing.New(pool, nil, cfg)
		if err = plans.EnsureTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		if err = plans.CheckSeats(ctx, tx, tenantID, "", "", 1); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id,role,display_name,can_serve)VALUES($1,$2,'owner',$3,true)", tenantID, userID, *name); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "SELECT ensure_chat_defaults($1::uuid,$2::uuid)", tenantID, userID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO identity_audit_logs(tenant_id,actor_id,action,target_id)VALUES($1,$2,'platform_bootstrap',$2)", tenantID, userID)
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
	body, err := json.MarshalIndent(map[string]string{"email": strings.ToLower(*email), "password": secret, "user_id": userID, "tenant_id": tenantID}, "", "  ")
	if err != nil {
		log.Fatal("凭证序列化失败")
	}
	if err = os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
		log.Fatal("私有目录不可写")
	}
	file, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal("账号已创建，但私有文件不可写；需管理员处理账号恢复")
	}
	_, err = file.Write(body)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		log.Fatal("账号已创建，但私有文件写入失败；需管理员处理账号恢复")
	}
	log.Print("平台管理员已初始化，随机口令保存在指定私有文件，未发送邮件")
}
