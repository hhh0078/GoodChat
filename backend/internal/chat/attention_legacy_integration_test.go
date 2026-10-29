// 文件作用说明：用真实PostgreSQL复现旧已读接待经过026升级后丢失查看元数据的重复提醒，验证事件因果证据与新接待/旁观/转接/真实未读边界。
package chat

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"path/filepath"
	"strings"
	"supportdesk/backend/internal/platform"
	"testing"
)

// legacyAttentionAnchor 执行真实026迁移复现旧段隐私水位被提升，不把该水位误当实际接待开始。
func legacyAttentionAnchor(t *testing.T, f *fixture, conversation, viewed string) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, "UPDATE conversation_assignments SET visibility_after_seq=NULL,accepted_at=now()-interval '30 minutes',created_at=now()-interval '30 minutes' WHERE id=(SELECT current_assignment_id FROM conversations WHERE id=$1)", conversation); err != nil {
		t.Fatal("准备旧接待边界失败")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE read_cursors SET updated_at=now()-interval '20 minutes' WHERE conversation_id=$1 AND participant_kind='agent' AND participant_id=$2", conversation, f.agent); err != nil {
		t.Fatal("准备旧已读时间失败")
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversation_attention_state SET viewed_assignment_id=NULLIF($3,'')::uuid,last_reminded_at=now()-interval '20 seconds' WHERE conversation_id=$1 AND membership_id=$2", conversation, f.agent, viewed); err != nil {
		t.Fatal("准备旧查看状态失败")
	}
	raw, err := os.ReadFile(filepath.Join("..", "database", "migrations", "026_history_upgrade_boundary.sql"))
	if err != nil {
		t.Fatal("读取真实升级迁移失败")
	}
	// 保留真实迁移主体，但执行副本只允许当前隔离租户；找不到唯一遍历语句就停止，不能意外执行全租户循环。
	if strings.Count(string(raw), "FOR tenant IN SELECT id FROM tenants LOOP") != 1 {
		t.Fatal("无法安全限定升级迁移的隔离租户范围")
	}
	scoped := strings.Replace(string(raw), "FOR tenant IN SELECT id FROM tenants LOOP", "FOR tenant IN SELECT id FROM tenants WHERE id='"+f.tenant+"'::uuid LOOP", 1)
	if _, err := f.admin.Exec(ctx, scoped); err != nil {
		t.Fatal("执行旧当前段升级迁移失败")
	}
}

// attentionClaimData 读取真实提醒响应，保留pending_count以验证不是仅因领取冷却而暂时没响。
func attentionClaimData(t *testing.T, f *fixture, member string) map[string]any {
	t.Helper()
	status, reply := f.request("POST", "/api/v1/agent/attention-reminders/claim", member, "", map[string]any{"initial": true})
	return mustData(t, status, reply)
}

// TestLegacyAttentionReadBeforeUpgradeAnchor 验证026锚点高于真实已读消息时，当前段已读证据仍能阻止NULL或旧viewID造成永久新咨询提醒。
func TestLegacyAttentionReadBeforeUpgradeAnchor(t *testing.T) {
	for _, stale := range []string{"null_view", "previous_assignment_view"} {
		t.Run(stale, func(t *testing.T) {
			f := newFixture(t)
			f.online(f.agent)
			f.online(f.owner)
			token, _, conversation := f.visitor("")
			status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
			first := mustData(t, status, reply)
			oldAssignment := first["current_assignment_id"].(string)
			if stale == "previous_assignment_view" {
				status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
				mustData(t, status, reply)
				status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
				mustData(t, status, reply)
			}
			message := historySend(t, f, conversation, "", token, "升级前实际已读的隔离访客消息")
			status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": message["seq"]})
			mustData(t, status, reply)
			viewed := ""
			if stale == "previous_assignment_view" {
				viewed = oldAssignment
			}
			legacyAttentionAnchor(t, f, conversation, viewed)
			var cursor, anchor, start, readEvent int64
			if err := f.admin.QueryRow(t.Context(), `SELECT rc.last_read_seq,a.visibility_after_seq,(SELECT max(e.seq) FROM conversation_events e WHERE e.conversation_id=c.id AND e.event_type='assignment.changed' AND e.payload->>'assignment_id'=a.id::text),(SELECT max(e.seq) FROM conversation_events e WHERE e.conversation_id=c.id AND e.event_type='read.updated' AND e.payload->>'participant_id'=$2::text AND e.payload->>'last_read_seq'=rc.last_read_seq::text) FROM conversations c JOIN conversation_assignments a ON a.id=c.current_assignment_id JOIN read_cursors rc ON rc.conversation_id=c.id AND rc.participant_id=$2::uuid AND rc.participant_kind='agent' WHERE c.id=$1`, conversation, f.agent).Scan(&cursor, &anchor, &start, &readEvent); err != nil {
				var pgerr *pgconn.PgError
				state := "unknown"
				if errors.As(err, &pgerr) {
					state = pgerr.Code
				}
				t.Fatalf("核对旧段因果序列失败 type=%T SQLSTATE=%s", err, state)
			}
			if cursor >= anchor || cursor <= start || readEvent <= start {
				t.Fatal("隔离夹具没有复现已读消息低于026锚点且位于当前接待内的条件")
			}
			t.Logf("legacy evidence: assignment seq=%d < read message=%d < read.updated=%d <= upgrade anchor=%d", start, cursor, readEvent, anchor)
			result := attentionClaimData(t, f, f.agent)
			if result["pending_count"] != float64(0) || len(result["items"].([]any)) != 0 {
				t.Fatal("已读旧接待仍被当作新咨询重复提醒")
			}
			if f.countRows("SELECT count(*) FROM conversation_attention_state att JOIN conversations c ON c.id=att.conversation_id WHERE c.id=$1 AND att.membership_id=$2 AND att.viewed_assignment_id=c.current_assignment_id", conversation, f.agent) != 1 {
				t.Fatal("应幂等修复当前段查看元数据")
			}
			var after int64
			if err := f.admin.QueryRow(t.Context(), "SELECT last_read_seq FROM read_cursors WHERE conversation_id=$1 AND participant_id=$2 AND participant_kind='agent'", conversation, f.agent).Scan(&after); err != nil || after != cursor {
				t.Fatal("兼容修复不能推进实际已读游标")
			}
			again := attentionClaimData(t, f, f.agent)
			if again["pending_count"] != float64(0) {
				t.Fatal("再次领取不应重新出现新咨询")
			}
		})
	}
}

// TestLegacyAttentionRetainsRealUnread 验证旧查看状态修复之后新来的真实未读仍立即提醒，不会被兼容逻辑清除。
func TestLegacyAttentionRetainsRealUnread(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	old := historySend(t, f, conversation, "", token, "旧已读")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": old["seq"]})
	mustData(t, status, reply)
	legacyAttentionAnchor(t, f, conversation, "")
	historySend(t, f, conversation, "", token, "升级后真实未读")
	result := attentionClaimData(t, f, f.agent)
	items := result["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "unread" || items[0].(map[string]any)["unread_count"] != float64(1) {
		t.Fatal("真实新未读被旧段兼容逻辑消除了")
	}
}

// requireNewAttention 验证缺少当前段阅读证据时仍为新咨询，不把领取冷却误认为已查看。
func requireNewAttention(t *testing.T, f *fixture, conversation, member string) {
	t.Helper()
	result := attentionClaimData(t, f, member)
	items := result["items"].([]any)
	if result["pending_count"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["kind"] != "new" {
		t.Fatal("未查看的新接待被旧已读证据错误消除了")
	}
	if f.countRows("SELECT count(*) FROM conversation_attention_state att JOIN conversations c ON c.id=att.conversation_id WHERE c.id=$1 AND att.membership_id=$2 AND att.viewed_assignment_id=c.current_assignment_id", conversation, member) != 0 {
		t.Fatal("证据不明时不能补写本段已查看")
	}
}

// TestLegacyAttentionDoesNotInferFromReadTime 验证空接待、之前排队旁观、转回本人和迟到旧read事件均不能被时间戳误判已查看。
func TestLegacyAttentionDoesNotInferFromReadTime(t *testing.T) {
	for _, kind := range []string{"new_empty", "queued_spectator", "return_to_same_member", "late_previous_read_event"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			f.online(f.agent)
			f.online(f.owner)
			token, _, conversation := f.visitor("")
			if kind == "queued_spectator" {
				message := historySend(t, f, conversation, "", token, "排队时旁观读取")
				status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": message["seq"]})
				mustData(t, status, reply)
			}
			status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
			mustData(t, status, reply)
			if kind == "return_to_same_member" || kind == "late_previous_read_event" {
				message := historySend(t, f, conversation, "", token, "本人旧接待段已读")
				status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": message["seq"]})
				mustData(t, status, reply)
				status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
				mustData(t, status, reply)
				status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
				mustData(t, status, reply)
				if kind == "late_previous_read_event" {
					if err := platform.TenantTx(t.Context(), f.pool, f.tenant, func(tx pgx.Tx) error {
						_, err := f.s.emit(t.Context(), tx, f.tenant, conversation, "read.updated", "public", map[string]any{"participant_kind": "agent", "participant_id": f.agent, "last_read_seq": message["seq"]})
						return err
					}); err != nil {
						t.Fatal("构造旧请求迟到事件失败")
					}
				}
			}
			// 事务开始时间早于实际分配，不能仅因read.updated_at更晚就推断当前新段已查看。
			if _, err := f.admin.Exec(t.Context(), "UPDATE conversation_assignments SET accepted_at=now()-interval '1 day',created_at=now()-interval '1 day' WHERE id=(SELECT current_assignment_id FROM conversations WHERE id=$1)", conversation); err != nil {
				t.Fatal("准备反序事务时间失败")
			}
			requireNewAttention(t, f, conversation, f.agent)
		})
	}
}

// TestLegacyAttentionRequiresExactPersistentEvents 验证只有游标或不精确事件时保留提醒，不依据agent_id或已读更新时间猜测。
func TestLegacyAttentionRequiresExactPersistentEvents(t *testing.T) {
	for _, missing := range []string{"assignment_id", "read_participant"} {
		t.Run(missing, func(t *testing.T) {
			f := newFixture(t)
			f.online(f.agent)
			token, _, conversation := f.visitor("")
			status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
			mustData(t, status, reply)
			message := historySend(t, f, conversation, "", token, "只有部分旧证据")
			status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": message["seq"]})
			mustData(t, status, reply)
			legacyAttentionAnchor(t, f, conversation, "")
			query := "UPDATE conversation_events SET payload=payload-'assignment_id' WHERE conversation_id=$1 AND event_type='assignment.changed'"
			if missing == "read_participant" {
				query = "UPDATE conversation_events SET payload=payload-'participant_id' WHERE conversation_id=$1 AND event_type='read.updated'"
			}
			if _, err := f.admin.Exec(t.Context(), query, conversation); err != nil {
				t.Fatal("构造缺少旧事件证据失败")
			}
			requireNewAttention(t, f, conversation, f.agent)
		})
	}
}

// TestLegacyAttentionConfirmedTransferProof 验证确认式转接通过实际assignment容量hold关联transfer_request_id精确恢复，不要求旧事件补造assignment_id。
func TestLegacyAttentionConfirmedTransferProof(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	f.online(f.owner)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.owner, "", map[string]any{})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(t.Context(), "UPDATE routing_pools SET assignment_mode='confirm' WHERE id=$1", f.poolID); err != nil {
		t.Fatal("准备确认式转接配置失败")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.owner, "", map[string]any{"agent_id": f.agent})
	transfer := mustData(t, status, reply)["pending_transfer"].(map[string]any)["id"]
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/accept", f.agent, "", map[string]any{"transfer_request_id": transfer})
	accepted := mustData(t, status, reply)
	if f.countRows("SELECT count(*) FROM conversation_events WHERE conversation_id=$1 AND event_type='assignment.changed' AND payload->>'transfer_request_id'=$2 AND NOT payload ? 'assignment_id'", conversation, transfer) != 1 {
		t.Fatal("夹具没有使用真实无assignment_id的转接事件")
	}
	message := historySend(t, f, conversation, "", token, "确认式转接后实际已读")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/read-cursor", f.agent, "", map[string]any{"last_read_seq": message["seq"]})
	mustData(t, status, reply)
	legacyAttentionAnchor(t, f, conversation, "")
	result := attentionClaimData(t, f, f.agent)
	if result["pending_count"] != float64(0) {
		t.Fatal("确认式转接的精确hold证据没有恢复已查看")
	}
	if f.countRows("SELECT count(*) FROM conversation_attention_state WHERE conversation_id=$1 AND membership_id=$2 AND viewed_assignment_id=$3", conversation, f.agent, accepted["current_assignment_id"]) != 1 {
		t.Fatal("恢复查看状态绑定了错误接待段")
	}
}
