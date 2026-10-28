// 文件作用说明：真实PostgreSQL模拟事务开始时间与接待顺序相反，验收027前段定位、旧起点单向修复和消息附件引用事件的隐私边界。
package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// boundaryTestMember 新增第三位普通客服，避免管理员审计权限掩盖跨接待段泄漏。
func boundaryTestMember(t *testing.T, f *fixture) string {
	t.Helper()
	member, user := newID(), newID()
	if _, err := f.admin.Exec(t.Context(), "INSERT INTO users(id,email,display_name,password_hash) VALUES($1,$2,'第三位普通客服','test-only-unusable-password')", user, "boundary-"+user+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(t.Context(), "INSERT INTO tenant_memberships(id,tenant_id,user_id,role,can_serve,capacity) VALUES($1,$2,$3,'agent',true,2)", member, f.tenant, user); err != nil {
		t.Fatal(err)
	}
	if err := platform.TenantTx(t.Context(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "SELECT ensure_chat_defaults($1::uuid,$2::uuid)", f.tenant, user)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.principals[member] = platform.Principal{TenantID: f.tenant, UserID: user, MembershipID: member, Role: "agent", CanServe: true}
	f.online(member)
	return member
}

// applyBoundaryOrderMigration 真正执行027迁移的隔离schema副本，覆盖已有错误边界的单向提高修复。
func applyBoundaryOrderMigration(t *testing.T, f *fixture) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "database", "migrations", "027_history_boundary_order.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ReplaceAll(string(body), "public.", f.schema+".")
	sql = strings.ReplaceAll(sql, "pg_catalog,public", "pg_catalog,"+f.schema)
	if _, err := f.admin.Exec(t.Context(), sql); err != nil {
		t.Fatal(err)
	}
}

// TestHistoryBoundaryUsesActualCurrentAssignment 验证B事务开始早于A但实际接待晚于A时，C仍从B真实释放水位起算，不能看到B的正文或附件。
func TestHistoryBoundaryUsesActualCurrentAssignment(t *testing.T) {
	f := newFixture(t)
	f.online(f.owner)
	f.online(f.agent)
	third := boundaryTestMember(t, f)
	// fixture尚未升级027时同样运行新函数，测试明确覆盖实际发布迁移。
	applyBoundaryOrderMigration(t, f)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	historySend(t, f, conversation, "", token, "A段旧正文")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_assignments SET created_at=created_at-interval '1 day' WHERE conversation_id=$1 AND membership_id=$2", conversation, f.agent); err != nil {
		t.Fatal(err)
	}
	secret := historySend(t, f, conversation, "", token, "B段必须隐藏的正文")
	status, reply = uploadQuickReplyMedia(t, f, f.agent, "b-private.png", avatarTestPNG(t, 3, 3))
	if status != 201 {
		t.Fatalf("媒体上传失败: %d", status)
	}
	file := reply["data"].(map[string]any)["id"].(string)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "message_type": "image", "attachment_id": file})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": third})
	mustData(t, status, reply)
	var after, through, firstEnd int64
	if err := f.admin.QueryRow(t.Context(), `SELECT c.visibility_after_seq,b.visibility_through_seq,a.visibility_through_seq FROM conversation_assignments c JOIN conversation_assignments b ON b.conversation_id=c.conversation_id AND b.membership_id=$2 JOIN conversation_assignments a ON a.conversation_id=c.conversation_id AND a.membership_id=$3 WHERE c.conversation_id=$1 AND c.membership_id=$4`, conversation, f.agent, f.owner, third).Scan(&after, &through, &firstEnd); err != nil {
		t.Fatal(err)
	}
	if after != through || through <= firstEnd {
		t.Fatalf("C起点必须等于实际B释放水位: after=%d B=%d A=%d", after, through, firstEnd)
	}
	// 模拟025已经写入的较低旧起点，重新执行027只能提高并修复，不能降低其他已知段。
	if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_assignments SET visibility_after_seq=$3 WHERE conversation_id=$1 AND membership_id=$2", conversation, third, firstEnd); err != nil {
		t.Fatal(err)
	}
	applyBoundaryOrderMigration(t, f)
	if f.countRows("SELECT count(*) FROM conversation_assignments WHERE conversation_id=$1 AND membership_id=$2 AND visibility_after_seq=$3", conversation, third, through) != 1 {
		t.Fatal("027必须单向提高025错误起点")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"share_previous_chats": false})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", third, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("C历史泄漏了B正文或附件")
	}
	status, _ = f.request("GET", "/api/v1/attachments/"+file, third, "", nil)
	if status != 404 {
		t.Fatalf("C不得下载B历史附件: %d", status)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", third, "", map[string]any{"client_message_id": newID(), "content": "不能引用B", "reply_to_id": secret["id"]})
	if status != 403 {
		t.Fatal("C不得引用B历史正文")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "C段新消息", "reply_to_id": secret["id"]})
	current := mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", third, "", nil)
	items := mustData(t, status, reply)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != current["id"] || items[0].(map[string]any)["reply"] != nil {
		t.Fatal("C新消息中的B引用必须裁剪")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/events", third, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		event := raw.(map[string]any)
		if event["event_type"] == "message.created" {
			payload := event["payload"].(map[string]any)
			if payload["id"] != current["id"] || payload["reply"] != nil {
				t.Fatal("C事件补拉泄漏B旧消息或引用")
			}
		}
	}
	if unreadValue(t, f.conversationList(third), conversation) != 1 {
		t.Fatal("C未读数不能计入B历史")
	}
	// 回队列会清空接待指针；任何旧未知结束水位都不允许取其他段较早的max并暴露排队空隙。
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", third, "", map[string]any{"target": "queue"})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_assignments SET visibility_through_seq=NULL WHERE conversation_id=$1 AND membership_id=$2", conversation, f.owner); err != nil {
		t.Fatal(err)
	}
	queued := historySend(t, f, conversation, "", token, "未知旧段之后的排队信息应保守隐藏")
	var watermark int64
	if err := f.admin.QueryRow(t.Context(), "SELECT next_event_seq FROM conversations WHERE id=$1", conversation).Scan(&watermark); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", third, "", map[string]any{})
	mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM conversation_assignments a JOIN conversations c ON c.current_assignment_id=a.id WHERE c.id=$1 AND a.visibility_after_seq=$2", conversation, watermark) != 1 {
		t.Fatal("指针为空且存在未知旧段时必须从当前水位开始")
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", third, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		if raw.(map[string]any)["id"] == queued["id"] {
			t.Fatal("未知接待边界不能扩大到排队前历史")
		}
	}
}

// TestHistoryPreferenceUsesFinalAssignment 验证接待确认时间反序时仍优先真正最后的客服，指针缺失只接受可靠唯一结束水位。
func TestHistoryPreferenceUsesFinalAssignment(t *testing.T) {
	for _, test := range []struct {
		name                  string
		clearPointer, unknown bool
		wantHistory           bool
	}{{"当前指针优先", false, false, true}, {"缺指针冻结水位回退", true, false, true}, {"未知旧段保守回主算法", true, true, false}} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.online(f.owner)
			f.online(f.agent)
			token, _, conversation := f.visitor("")
			status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.owner, "", map[string]any{})
			mustData(t, status, reply)
			historySend(t, f, conversation, "", token, "A段消息")
			status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
			mustData(t, status, reply)
			if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_assignments SET accepted_at=accepted_at-interval '1 day',created_at=created_at-interval '1 day' WHERE conversation_id=$1 AND membership_id=$2", conversation, f.agent); err != nil {
				t.Fatal(err)
			}
			historySend(t, f, conversation, "", token, "真正最终B段消息")
			status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
			mustData(t, status, reply)
			if test.clearPointer {
				if _, err := f.admin.Exec(t.Context(), "UPDATE conversations SET current_assignment_id=NULL,current_agent_id=NULL WHERE id=$1", conversation); err != nil {
					t.Fatal(err)
				}
			}
			if test.unknown {
				if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_assignments SET visibility_through_seq=NULL WHERE conversation_id=$1 AND membership_id=$2", conversation, f.agent); err != nil {
					t.Fatal(err)
				}
			}
			next := revisitHistory(t, f, token, conversation, nil)["conversation"].(map[string]any)
			if test.wantHistory {
				if next["current_agent_id"] != f.agent {
					t.Fatal("历史优先必须使用实际最终B客服，不能按较晚accepted_at选择A")
				}
			} else if next["status"] != "queued" || next["current_agent_id"] != nil {
				t.Fatal("未知旧段不能猜测历史客服，应回到正常人工分配")
			}
		})
	}
}
