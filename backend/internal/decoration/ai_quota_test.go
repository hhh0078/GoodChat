// 文件作用说明：使用真实PostgreSQL验证AI自然日提交上限、取消不返额度和最后一次额度的并发竞争。
package decoration

import (
	"context"
	"sync"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestPostgresDailyAIQuotaConcurrency 验证同租户争用最后一次提交仅成功一个，取消和失败不恢复当日次数。
func TestPostgresDailyAIQuotaConcurrency(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{AIEndpoint: "http://127.0.0.1:1", AIKey: "test-only", AIModel: "not-called-test"})
	f.service.cfg.AIDailyLimit = 1
	type response struct {
		status int
		data   map[string]any
	}
	results := make(chan response, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			status, data := f.request("POST", "/tenant/test-legacy-ai-design-jobs", f.member, map[string]any{"name": "最后一次额度", "prompt": "创建一个自然绿色客服页面", "render_mode": "schema"})
			results <- response{status, data}
		}()
	}
	group.Wait()
	close(results)
	created, limited, jobID := 0, 0, ""
	for result := range results {
		if result.status == 200 {
			created++
			jobID = result.data["data"].(map[string]any)["id"].(string)
		} else if result.status == 429 && result.data["code"] == "ai_daily_limit" {
			limited++
		} else {
			t.Fatalf("每日额度竞争意外响应：%d %v", result.status, result.data)
		}
	}
	if created != 1 || limited != 1 {
		t.Fatal("最后一次额度必须恰好成功一个")
	}
	configuration := f.mustRequest("GET", "/tenant/ai-configuration", f.member, nil)
	if configuration["daily_limit"] != float64(1) || configuration["used_today"] != float64(1) || configuration["remaining_today"] != float64(0) {
		t.Fatal("配置应返回真实当日已用和剩余提交次数")
	}
	f.mustRequest("POST", "/tenant/ai-design-jobs/"+jobID+"/cancel", f.member, nil)
	status, result := f.request("POST", "/tenant/test-legacy-ai-design-jobs", f.member, map[string]any{"name": "取消后再提交", "prompt": "创建一个客服页面", "render_mode": "schema"})
	if status != 429 || result["code"] != "ai_daily_limit" {
		t.Fatal("取消不应返还每日提交次数")
	}
	other := f.mustRequest("GET", "/tenant/ai-configuration", f.otherMember, nil)
	if other["used_today"] != float64(0) || other["remaining_today"] != float64(1) {
		t.Fatal("其他租户额度不能被当前租户占用")
	}
}

// TestPostgresDailyAIQuotaTimezone 使用租户时区的前一日尾部和本日边界验证计数，未来任务不应提前占用今日。
func TestPostgresDailyAIQuotaTimezone(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{AIEndpoint: "http://127.0.0.1:1", AIKey: "test-only", AIModel: "not-called-test"})
	job := f.mustRequest("POST", "/tenant/test-legacy-ai-design-jobs", f.member, map[string]any{"name": "自然日边界", "prompt": "设计简洁客服页面", "render_mode": "schema"})
	id := job["id"].(string)
	f.mustRequest("POST", "/tenant/ai-design-jobs/"+id+"/cancel", f.member, nil)
	for _, timezone := range []string{"Pacific/Kiritimati", "America/Los_Angeles"} {
		if _, err := f.admin.Exec(context.Background(), "UPDATE tenants SET timezone=$2 WHERE id=$1", f.tenant, timezone); err != nil {
			t.Fatal(err)
		}
		for _, point := range []struct {
			offset   string
			expected float64
		}{{"- interval '1 microsecond'", 0}, {"+ interval '1 microsecond'", 1}, {"+ interval '1 day'", 0}} {
			query := "UPDATE ai_design_jobs SET created_at=((now() AT TIME ZONE $2)::date::timestamp AT TIME ZONE $2) " + point.offset + " WHERE id=$1"
			if _, err := f.admin.Exec(context.Background(), query, id, timezone); err != nil {
				t.Fatal(err)
			}
			configuration := f.mustRequest("GET", "/tenant/ai-configuration", f.member, nil)
			if configuration["timezone"] != timezone || configuration["used_today"] != point.expected {
				t.Fatalf("租户自然日边界计数错误：%s %s，收到%v", timezone, point.offset, configuration)
			}
		}
	}
}

// TestPostgresZeroDailyQuotaDisablesAI 验证零表示禁用，并且拒绝前不创建私有模板或持久任务。
func TestPostgresZeroDailyQuotaDisablesAI(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{AIEndpoint: "http://127.0.0.1:1", AIKey: "test-only", AIModel: "not-called-test"})
	f.service.cfg.AIDailyLimit = 0
	status, result := f.request("POST", "/tenant/test-legacy-ai-design-jobs", f.member, map[string]any{"name": "已禁用", "prompt": "创建客服页面", "render_mode": "schema"})
	if status != 503 || result["code"] != "ai_daily_disabled" {
		t.Fatal("零额度必须明确禁用，不能视作无限")
	}
	configuration := f.mustRequest("GET", "/tenant/ai-configuration", f.member, nil)
	if configuration["enabled"] != false || configuration["daily_limit"] != float64(0) || configuration["used_today"] != float64(0) || configuration["remaining_today"] != float64(0) {
		t.Fatal("零额度应展示禁用且没有提交记录")
	}
	var count int
	if err := f.admin.QueryRow(context.Background(), "SELECT count(*) FROM tenant_templates WHERE tenant_id=$1", f.tenant).Scan(&count); err != nil || count != 0 {
		t.Fatal("禁用提交不应创建空模板")
	}
}
