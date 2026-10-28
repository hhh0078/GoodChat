// 文件作用说明：真实执行升级补充迁移，确保旧接待未知历史保守隐藏，但同一当前接待在升级后仍能正常收到新消息。
package chat

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLegacyCurrentHistoryBoundaryUpgrade 验证026只锚定未知当前段，保留已知边界和旧段，不把升级后新消息永久隐藏。
func TestLegacyCurrentHistoryBoundaryUpgrade(t *testing.T) {
	f := newFixture(t)
	f.online(f.owner)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	historySend(t, f, conversation, "", token, "原客服的升级前未知内容")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	mustData(t, status, reply)
	historySend(t, f, conversation, "", token, "当前客服升级前边界也未知")
	if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_assignments SET visibility_after_seq=NULL,visibility_through_seq=NULL WHERE conversation_id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("..", "database", "migrations", "026_history_upgrade_boundary.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(t.Context(), string(body)); err != nil {
		t.Fatal(err)
	}
	if f.countRows("SELECT count(*) FROM conversation_assignments a JOIN conversations c ON c.current_assignment_id=a.id WHERE c.id=$1 AND a.visibility_after_seq=c.next_event_seq", conversation) != 1 {
		t.Fatal("未知当前段必须锚定实际升级水位")
	}
	if f.countRows("SELECT count(*) FROM conversation_assignments WHERE conversation_id=$1 AND released_at IS NOT NULL AND visibility_after_seq IS NULL", conversation) != 1 {
		t.Fatal("升级不能猜测或放宽已结束旧段的未知历史")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/chat-settings", f.owner, "", map[string]any{"share_previous_chats": false})
	mustData(t, status, reply)
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 0 {
		t.Fatal("升级锚点之前的未知历史应保持隐藏")
	}
	current := historySend(t, f, conversation, "", token, "升级后新消息必须正常展示")
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	items := mustData(t, status, reply)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != current["id"] {
		t.Fatal("升级后当前客服必须能读取新消息")
	}
	// 补充迁移再次执行也不会向前移动已知边界，避免重试时吞掉升级后消息。
	if _, err := f.admin.Exec(t.Context(), string(body)); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("GET", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 1 {
		t.Fatal("迁移重试不得改变已知当前段水位")
	}
}
