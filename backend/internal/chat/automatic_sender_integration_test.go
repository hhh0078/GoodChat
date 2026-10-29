// 文件作用说明：验证自动消息公开客服身份在接单前后、事件补拉和转接历史中保持正确，原始系统来源不变。
package chat

import (
	"context"
	"testing"
)

// TestAutomaticSenderIdentityPreservesAssignmentHistory 验证接单前欢迎关联首接客服，之后自动消息冻结发送时客服，转接不会改写历史身份或人工统计。
func TestAutomaticSenderIdentityPreservesAssignmentHistory(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, item := range []struct{ member, name, avatar string }{{f.agent, "客服甲", "default-07"}, {f.owner, "客服乙", "default-08"}} {
		if _, err := f.admin.Exec(ctx, `UPDATE tenant_memberships SET display_name=$2,avatar_ref=$3 WHERE id=$1`, item.member, item.name, item.avatar); err != nil {
			t.Fatal(err)
		}
		f.online(item.member)
	}
	token, _, conversation := f.visitor("")
	runWorkers(t, f.s, f.tenant)
	status, reply := f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	early := mustData(t, status, reply)["items"].([]any)
	if len(early) != 1 || early[0].(map[string]any)["automation_agent"] != nil {
		t.Fatal("无人接待时不能虚构客服身份")
	}
	personalClaim(t, f, conversation, f.agent)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, "", token, nil)
	first := mustData(t, status, reply)["first_agent"].(map[string]any)
	if first["id"] != f.agent || first["name"] != "客服甲" || first["avatar_url"] != "/images/avatars/default-07.png" || len(first) != 3 {
		t.Fatal("首次接待身份缺失或包含多余账号信息")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/events", "", token, nil)
	foundWelcome := false
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		event := raw.(map[string]any)
		payload, _ := event["payload"].(map[string]any)
		if payload["source"] == "welcome" {
			agent, _ := payload["automation_agent"].(map[string]any)
			foundWelcome = agent["id"] == f.agent && payload["sender_kind"] == "system" && payload["sender_name"] == "系统"
		}
	}
	if !foundWelcome {
		t.Fatal("接单前创建的持久事件未补齐首接客服身份")
	}
	status, reply = f.request("POST", "/api/v1/tenant/keyword-rules", f.owner, "", map[string]any{"name": "自动客服报价", "pattern": "报价", "reply": "自动报价正文", "cooldown_seconds": 0})
	mustData(t, status, reply)
	send := func() {
		status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "报价"})
		mustData(t, status, reply)
		runWorkers(t, f.s, f.tenant)
	}
	send()
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	mustData(t, status, reply)
	send()
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", "", token, nil)
	keywordIndex := 0
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		item := raw.(map[string]any)
		if item["source"] != "welcome" && item["source"] != "keyword" {
			continue
		}
		agent, ok := item["automation_agent"].(map[string]any)
		if !ok || len(agent) != 3 || item["sender_kind"] != "system" || item["sender_name"] != "系统" {
			t.Fatal("自动回复身份不完整或改变了系统来源")
		}
		expected := f.agent
		if item["source"] == "keyword" {
			if keywordIndex == 1 {
				expected = f.owner
			}
			keywordIndex++
		}
		if agent["id"] != expected {
			t.Fatal("转接后自动消息历史客服身份被改写")
		}
	}
	if keywordIndex != 2 || f.countRows("SELECT count(*) FROM messages WHERE conversation_id=$1 AND sender_kind='agent'", conversation) != 0 {
		t.Fatal("自动消息缺失或被错误计为人工回复")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation, "", token, nil)
	detail := mustData(t, status, reply)
	if detail["current_agent_id"] != f.owner || detail["first_agent"].(map[string]any)["id"] != f.agent {
		t.Fatal("转接改变了首接身份或未更新当前客服")
	}
	status, _ = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.otherOwner, "", nil)
	if status != 404 {
		t.Fatal("自动客服展示信息绕过了会话权限")
	}
}
