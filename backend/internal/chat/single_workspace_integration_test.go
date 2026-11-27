// 文件作用说明：在真实PostgreSQL隔离schema核验唯一工作台、默认空间及升级失败不清理历史数据的约束。
package chat

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// applySingleWorkspaceMigration 使用实际051迁移在事务内建立约束，任一旧数据冲突均整体回滚。
func applySingleWorkspaceMigration(t *testing.T, f *fixture) error {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "database", "migrations", "051_single_workspace.sql"))
	if err != nil {
		t.Fatal(err)
	}
	return pgx.BeginTxFunc(t.Context(), f.admin, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), string(body))
		return err
	})
}

// requireConstraintCode 确认拒绝来自真实数据库约束，避免把其他SQL错误当成成功防护。
func requireConstraintCode(t *testing.T, err error, code string) {
	t.Helper()
	var problem *pgconn.PgError
	if !errors.As(err, &problem) || problem.Code != code {
		t.Fatalf("应由数据库约束%s拒绝，实际%v", code, err)
	}
}

// TestSingleWorkspaceDatabaseConstraints 验证API与数据库共同阻止第二工作台和非默认空间，停用历史身份仍可保留。
func TestSingleWorkspaceDatabaseConstraints(t *testing.T) {
	f := newFixture(t)
	if err := applySingleWorkspaceMigration(t, f); err != nil {
		t.Fatal(err)
	}
	var user string
	if err := f.admin.QueryRow(t.Context(), "SELECT user_id::text FROM tenant_memberships WHERE id=$1", f.owner).Scan(&user); err != nil {
		t.Fatal(err)
	}
	_, err := f.admin.Exec(t.Context(), "INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'agent')", f.otherTenant, user)
	requireConstraintCode(t, err, "23505")
	var oldMember string
	if err := f.admin.QueryRow(t.Context(), "INSERT INTO tenant_memberships(tenant_id,user_id,role,disabled) VALUES($1,$2,'agent',true) RETURNING id::text", f.otherTenant, user).Scan(&oldMember); err != nil {
		t.Fatal("停用历史身份应保留", err)
	}
	_, err = f.admin.Exec(t.Context(), "UPDATE tenant_memberships SET disabled=false WHERE id=$1", oldMember)
	requireConstraintCode(t, err, "23505")
	tenant := newID()
	if _, err := f.admin.Exec(t.Context(), "INSERT INTO tenants(id,name,slug) VALUES($1,'单空间约束测试',$2)", tenant, "single-space-"+tenant); err != nil {
		t.Fatal(err)
	}
	_, err = f.admin.Exec(t.Context(), "INSERT INTO service_spaces(tenant_id,name,is_default) VALUES($1,'非默认空间',false)", tenant)
	requireConstraintCode(t, err, "23514")
	if _, err := f.admin.Exec(t.Context(), "INSERT INTO service_spaces(tenant_id,name,is_default) VALUES($1,'默认服务空间',true)", tenant); err != nil {
		t.Fatal(err)
	}
	_, err = f.admin.Exec(t.Context(), "INSERT INTO service_spaces(tenant_id,name,is_default) VALUES($1,'第二空间',true)", tenant)
	requireConstraintCode(t, err, "23505")
	var defaultSpace, otherSpace string
	if err := f.admin.QueryRow(t.Context(), "SELECT id::text FROM service_spaces WHERE tenant_id=$1", f.tenant).Scan(&defaultSpace); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(t.Context(), "SELECT id::text FROM service_spaces WHERE tenant_id=$1", f.otherTenant).Scan(&otherSpace); err != nil {
		t.Fatal(err)
	}
	status, reply := f.request("POST", "/api/v1/tenant/access-links", f.owner, "", map[string]any{"name": "默认空间入口"})
	if status != 201 || reply["data"].(map[string]any)["service_space_id"] != defaultSpace {
		t.Fatalf("入口应自动绑定默认空间：%d %v", status, reply["code"])
	}
	link := reply["data"].(map[string]any)["id"].(string)
	status, reply = f.request("PATCH", "/api/v1/tenant/access-links/"+link, f.owner, "", map[string]any{"service_space_id": otherSpace})
	if status != 400 || reply["code"] != "single_service_space" {
		t.Fatal("入口不能通过修改切换空间")
	}
}

// TestSingleWorkspaceMigrationRejectsLegacyDuplicates 历史多成员或多空间使迁移安全失败，不得悄悄删除或停用数据。
func TestSingleWorkspaceMigrationRejectsLegacyDuplicates(t *testing.T) {
	for _, duplicate := range []string{"membership", "space"} {
		t.Run(duplicate, func(t *testing.T) {
			f := newFixture(t)
			if duplicate == "membership" {
				if _, err := f.admin.Exec(t.Context(), "INSERT INTO tenant_memberships(tenant_id,user_id,role) SELECT $1,user_id,'agent' FROM tenant_memberships WHERE id=$2", f.otherTenant, f.owner); err != nil {
					t.Fatal(err)
				}
			} else if _, err := f.admin.Exec(t.Context(), "INSERT INTO service_spaces(tenant_id,name) VALUES($1,'升级前历史空间')", f.tenant); err != nil {
				t.Fatal(err)
			}
			requireConstraintCode(t, applySingleWorkspaceMigration(t, f), "23505")
			var changed bool
			if err := f.admin.QueryRow(t.Context(), "SELECT to_regclass('memberships_one_workspace') IS NOT NULL").Scan(&changed); err != nil || changed {
				t.Fatal("迁移失败应回滚全部已执行的DDL")
			}
			var count int
			query := "SELECT count(*) FROM service_spaces WHERE tenant_id=$1"
			if duplicate == "membership" {
				query = "SELECT count(*) FROM tenant_memberships WHERE user_id=(SELECT user_id FROM tenant_memberships WHERE id=$1) AND NOT disabled AND removed_at IS NULL"
				if err := f.admin.QueryRow(t.Context(), query, f.owner).Scan(&count); err != nil || count != 2 {
					t.Fatal("失败迁移不能改写历史成员关系")
				}
			} else if err := f.admin.QueryRow(t.Context(), query, f.tenant).Scan(&count); err != nil || count != 2 {
				t.Fatal("失败迁移不能删除历史空间")
			}
		})
	}
}
