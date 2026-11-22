// 文件作用说明：在隔离PostgreSQL与真实HTTP接口验证卡片分页、账单汇总和按角色显示的工作空间数据。
package chat

import (
	"context"
	"fmt"
	"net/url"
	"testing"
)

// TestConsoleRecordPagination 验证超过旧100条上限的账单、同时间排序和跨身份游标签名。
func TestConsoleRecordPagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := 0; i < 27; i++ {
		f.createRoutingResource("access-links", map[string]any{"name": fmt.Sprintf("分页入口%d", i)})
	}
	if _, err := f.admin.Exec(ctx, "UPDATE access_links SET created_at='2026-01-01' WHERE tenant_id=$1", f.tenant); err != nil {
		t.Fatal(err)
	}
	for _, seed := range []struct {
		member string
		count  int
		charge int
	}{{f.owner, 125, 7}, {f.agent, 3, 11}, {f.otherOwner, 1, 13}} {
		tenant := f.tenant
		if seed.member == f.otherOwner {
			tenant = f.otherTenant
		}
		_, err := f.admin.Exec(ctx, `INSERT INTO ai_jobs(tenant_id,kind,owner_membership_id,snapshot,status,input_tokens,output_tokens,created_at) SELECT $1,'test',$2,'{}','succeeded',100,10,'2026-01-01' FROM generate_series(1,$3::int)`, tenant, seed.member, seed.count)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.admin.Exec(ctx, `INSERT INTO ai_wallet_entries(tenant_id,job_id,kind,delta_micro,client_key,reason,created_at) SELECT tenant_id,id,'charge',-$2::bigint,id::text,'test',created_at FROM ai_jobs WHERE owner_membership_id=$1`, seed.member, seed.charge)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		path, member string
		count        int
	}{{"access-links", f.owner, 28}, {"ai/usage", f.owner, 128}, {"ai/ledger", f.owner, 128}, {"ai/usage", f.agent, 3}} {
		seen := map[string]bool{}
		cursor := ""
		pages := 0
		for {
			status, reply := f.request("GET", "/api/v1/tenant/"+test.path+"?limit=20&cursor="+url.QueryEscape(cursor), test.member, "", nil)
			data := mustData(t, status, reply)
			pages++
			items := data["items"].([]any)
			if len(items) > 20 {
				t.Fatal("每页上限失效")
			}
			for _, raw := range items {
				item := raw.(map[string]any)
				id := item["id"].(string)
				if seen[id] {
					t.Fatal("触底分页重复")
				}
				seen[id] = true
			}
			if test.path == "ai/usage" && pages == 1 {
				summary := data["summary"].(map[string]any)
				want := float64(908)
				if test.member == f.agent {
					want = 33
					if data["wallet"] != nil {
						t.Fatal("普通成员不能读取钱包")
					}
				}
				if summary["charged_micro"] != want || int(summary["calls"].(float64)) != test.count {
					t.Fatalf("概览不能只汇总本页: %v", summary)
				}
			}
			if !data["has_more"].(bool) {
				break
			}
			next := data["next_cursor"].(string)
			if next == "" || next == cursor || pages > 10 {
				t.Fatal("分页没有前进")
			}
			cursor = next
		}
		if len(seen) != test.count {
			t.Fatalf("%s 有遗漏，得到%d 预期%d", test.path, len(seen), test.count)
		}
	}
	status, reply := f.request("GET", "/api/v1/tenant/ai/usage?limit=20", f.owner, "", nil)
	cursor := mustData(t, status, reply)["next_cursor"].(string)
	for _, test := range []struct {
		path, member string
		want         int
	}{{"ai/usage?limit=20&cursor=" + url.QueryEscape(cursor+"x"), f.owner, 400}, {"ai/usage?limit=20&cursor=" + url.QueryEscape(cursor), f.otherOwner, 400}, {"ai/usage?limit=20&cursor=" + url.QueryEscape(cursor), f.agent, 400}, {"ai/ledger?limit=20&cursor=" + url.QueryEscape(cursor), f.owner, 400}, {"ai/ledger?limit=20", f.agent, 403}, {"access-links?limit=999", f.owner, 400}} {
		status, _ := f.request("GET", "/api/v1/tenant/"+test.path, test.member, "", nil)
		if status != test.want {
			t.Fatalf("边界状态不符: %s %d", test.path, status)
		}
	}
	status, reply = f.request("GET", "/api/v1/tenant/ai/usage", f.owner, "", nil)
	if len(mustData(t, status, reply)["jobs"].([]any)) != 100 {
		t.Fatal("旧客户端的jobs字段和100条读取应兼容")
	}
}

// TestRoleDashboard 验证管理员团队指标、客服本人接待、未读去重和权限撤销后拒绝读取。
func TestRoleDashboard(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	if _, err := f.admin.Exec(context.Background(), `INSERT INTO member_presence_intervals(tenant_id,membership_id,started_at,last_seen_at,online_until) VALUES($1,$2,now(),now(),now()+interval '30 seconds')`, f.tenant, f.agent); err != nil {
		t.Fatal(err)
	}
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	statisticsMessage(t, f, conversation, "", token, "需要帮助", "public")
	statisticsMessage(t, f, conversation, "", token, "补充说明", "public")
	f.visitor("")
	status, reply = f.request("GET", "/api/v1/tenant/dashboard", f.owner, "", nil)
	team := mustData(t, status, reply)
	if team["active"] != float64(1) || team["queued"] != float64(0) || team["today_created"] != float64(2) || team["online_agents"] != float64(1) {
		t.Fatalf("团队概览错误: %v", team)
	}
	status, reply = f.request("GET", "/api/v1/tenant/dashboard", f.agent, "", nil)
	self := mustData(t, status, reply)
	if self["active"] != float64(1) || self["today_received"] != float64(1) || self["unread"] != float64(1) {
		t.Fatalf("本人概览错误: %v", self)
	}
	for _, key := range []string{"queued", "online_agents", "today_created"} {
		if _, ok := self[key]; ok {
			t.Fatal("不能向客服返回团队汇总")
		}
	}
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	status, _ = f.request("GET", "/api/v1/tenant/dashboard", f.agent, "", nil)
	if status != 403 {
		t.Fatal("已停用成员不能继续读取概览")
	}
}
