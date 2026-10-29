// 文件作用说明：枚举已验证演示租户全部未结束咨询，真实HTTP关闭并核对零负载后才停用夹具，保留消息审计。
package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// closePendingConversations 每批重新枚举未结束资源，覆盖浏览器新增咨询；任一HTTP或状态错误立即停止。
func closePendingConversations(ctx context.Context, a *apiClient, owner identity, list func(context.Context) ([]string, error)) (int, error) {
	closed := 0
	for batch := 0; batch < 100; batch++ {
		ids, e := list(ctx)
		if e != nil {
			return closed, e
		}
		if len(ids) == 0 {
			return closed, nil
		}
		for _, id := range ids {
			var result struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			}
			if e = a.request(ctx, "POST", "/conversations/"+id+"/assignments/close", owner, map[string]any{"reason": "演示数据·本次验收结束"}, &result); e != nil {
				return closed, e
			}
			if result.ID != id || result.Status != "closed" {
				return closed, fmt.Errorf("close_not_confirmed")
			}
			closed++
		}
	}
	return closed, fmt.Errorf("cleanup_enumeration_limit")
}

// cleanupFixture 只操作夹具中经名称核实的测试tenant；所有咨询与接待占用归零之前不撤销登录或停用租户。
func cleanupFixture(ctx context.Context, o options) error {
	f, e := readFixture(o.fixture)
	if e != nil {
		return e
	}
	db, e := database(ctx, o.config)
	if e != nil {
		return e
	}
	defer db.Close(ctx)
	var name string
	if e = db.QueryRow(ctx, "SELECT name FROM tenants WHERE id=$1", f.TenantID).Scan(&name); e != nil || name != f.Name || len(f.Accounts) == 0 {
		return fmt.Errorf("fixture_name_mismatch")
	}
	if _, e = db.Exec(ctx, "SELECT set_config('app.tenant_id',$1,false)", f.TenantID); e != nil {
		return e
	}
	var planCode string
	if e = db.QueryRow(ctx, `SELECT p.code FROM plans p JOIN tenant_subscriptions s ON s.plan_id=p.id WHERE s.tenant_id=$1 AND p.id=$2`, f.TenantID, f.PlanID).Scan(&planCode); e != nil {
		return e
	}
	if planCode != "load-"+strings.ReplaceAll(f.TenantID, "-", "")[:20] {
		return fmt.Errorf("fixture_plan_mismatch")
	}
	var messagesBefore, messagesAfter int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM messages WHERE tenant_id=$1", f.TenantID).Scan(&messagesBefore); e != nil {
		return e
	}
	a := &apiClient{f.Base, f.TenantID, &http.Client{Timeout: 10 * time.Second}, newMeasurements()}
	defer a.http.CloseIdleConnections()
	// list 只在已确认的测试tenant内安全枚举，每次从剩余首批读取而非依赖静态fixture.visitors。
	list := func(ctx context.Context) ([]string, error) {
		rows, e := db.Query(ctx, "SELECT id::text FROM conversations WHERE tenant_id=$1 AND status IN('queued','assigned','active') ORDER BY id LIMIT 200", f.TenantID)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return nil, e
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}
	closed, e := closePendingConversations(ctx, a, identity{cookie: f.Accounts[0].Cookie}, list)
	if e != nil {
		return e
	}
	var unfinished, load int
	checkSQL := `SELECT (SELECT count(*) FROM conversations WHERE tenant_id=$1 AND status<>'closed'),coalesce((SELECT sum(current_load) FROM agent_runtime WHERE tenant_id=$1),0)`
	if e = db.QueryRow(ctx, checkSQL, f.TenantID).Scan(&unfinished, &load); e != nil {
		return e
	}
	if unfinished != 0 || load != 0 {
		return fmt.Errorf("cleanup_nonzero_remaining_or_load")
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for _, sql := range []string{"UPDATE access_links SET status='disabled' WHERE tenant_id=$1", "UPDATE agent_runtime SET available=false,busy=false,online_until=now() WHERE tenant_id=$1", "UPDATE tenant_memberships SET disabled=true WHERE tenant_id=$1", "UPDATE auth_sessions SET revoked_at=now() WHERE user_id IN(SELECT user_id FROM tenant_memberships WHERE tenant_id=$1)", "UPDATE tenants SET status='suspended' WHERE id=$1"} {
		if _, e = tx.Exec(ctx, sql, f.TenantID); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE plans SET is_active=false WHERE id=$1", f.PlanID); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if e = db.QueryRow(ctx, checkSQL, f.TenantID).Scan(&unfinished, &load); e != nil {
		return e
	}
	if unfinished != 0 || load != 0 {
		return fmt.Errorf("cleanup_final_nonzero")
	}
	if e = db.QueryRow(ctx, "SELECT count(*) FROM messages WHERE tenant_id=$1", f.TenantID).Scan(&messagesAfter); e != nil {
		return e
	}
	if messagesAfter < messagesBefore {
		return fmt.Errorf("cleanup_message_audit_changed")
	}
	proof := map[string]any{"message_count_before": messagesBefore, "message_count_after": messagesAfter, "tenant_id": f.TenantID, "http_closed": closed, "unfinished": unfinished, "current_load": load, "tenant_suspended": true, "retained_messages": true, "checked_at": time.Now().UTC()}
	if e = writeJSON(filepath.Join(filepath.Dir(o.fixture), "cleanup.json"), proof, false); e != nil {
		return e
	}
	fmt.Printf("fixture_suspended http_closed=%d unfinished=0 current_load=0 retained_messages=true\n", closed)
	return nil
}
