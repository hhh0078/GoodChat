// 文件作用说明：在独立空库验证内置历史脚本版本不能因缺少登记历史而被管理员以篡改字节重新声明。
package decoration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// TestPostgresFreshFoundationRejectsTampered102 确认没有1.0.2历史的新库仍拒绝伪造该内置版本，失败不留下新合同记录。
func TestPostgresFreshFoundationRejectsTampered102(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	ctx := context.Background()
	var oldVersions int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM assistant_contract_versions WHERE scripts->>'version'='1.0.2'").Scan(&oldVersions); err != nil {
		t.Fatal(err)
	}
	if oldVersions != 0 {
		t.Fatal("本回归要求独立新库没有1.0.2历史，不能依赖同名登记规则拒绝伪造")
	}
	var current assistantFoundation
	if err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		var err error
		current, err = f.service.currentAssistantFoundation(ctx, tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if current.Scripts.Version == "1.0.2" {
		t.Fatal("新库必须从当前内置版本初始化")
	}
	// 若此回归在修复前执行，仍只清理本测试成员误写的合同，不影响其他测试版本。
	t.Cleanup(func() {
		if err := pgx.BeginFunc(context.Background(), f.admin, func(tx pgx.Tx) error {
			if _, err := tx.Exec(context.Background(), "SET LOCAL session_replication_role=replica"); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), "DELETE FROM assistant_contract_versions WHERE created_by=$1", f.user)
			return err
		}); err != nil {
			t.Error("无法清理本回归创建的测试合同")
		}
	})
	var beforeCount int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM assistant_contract_versions").Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	forged := current
	forged.Scripts.Version = "1.0.2"
	forged.Scripts.SDK += "\n/* 新库回归：合法语法也不能冒充已冻结的内置1.0.2。 */"
	if err := validateTemplateScripts(forged.Scripts); err != nil {
		t.Fatal("测试输入须先通过普通脚本语法与方法检查，才能验证内置版本保护", err)
	}
	raw, _ := json.Marshal(forged)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/platform/assistant/foundation", bytes.NewReader(raw))
	principal := f.principals[f.member]
	principal.PlatformAdmin = true
	request = request.WithContext(platform.WithPrincipal(request.Context(), principal))
	response := httptest.NewRecorder()
	f.service.assistantFoundationSettings(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "内置核心脚本版本不可改写") {
		t.Fatalf("没有历史的内置1.0.2仍必须拒绝伪造，收到HTTP %d", response.Code)
	}
	var afterCount, afterOldVersions int
	if err := f.pool.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE scripts->>'version'='1.0.2') FROM assistant_contract_versions").Scan(&afterCount, &afterOldVersions); err != nil {
		t.Fatal(err)
	}
	if afterCount != beforeCount || afterOldVersions != 0 {
		t.Fatal("拒绝篡改必须回滚事务，不能登记另一份1.0.2字节")
	}
}
