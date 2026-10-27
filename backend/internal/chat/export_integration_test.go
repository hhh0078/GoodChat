// 文件作用说明：在真实PostgreSQL验证CSV筛选、租户和权益授权、公式中和、撤回清理、内部备注默认隔离及完整数量限制。
package chat

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// exportResponse 保存文件流与统一错误响应，测试不打印导出正文或访客凭证。
type exportResponse struct {
	status  int
	headers http.Header
	body    []byte
}

// downloadCSV 发起真实HTTP下载，不通过data信封解析CSV文件。
func (f *fixture) downloadCSV(path, member, token string) exportResponse {
	f.t.Helper()
	request, err := http.NewRequest("GET", f.server.URL+path, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	if member != "" {
		request.Header.Set("X-Test-Member", member)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return exportResponse{response.StatusCode, response.Header, body}
}

// csvRecords 验证下载类型、BOM与可解析CSV记录，支持正文中的逗号和换行。
func csvRecords(t *testing.T, response exportResponse) [][]string {
	t.Helper()
	if response.status != 200 {
		var problem map[string]any
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("下载HTTP%d: %v %v", response.status, problem["code"], problem["message"])
	}
	if !strings.HasPrefix(response.headers.Get("Content-Type"), "text/csv") || !strings.Contains(response.headers.Get("Content-Disposition"), "attachment;") {
		t.Fatal("下载响应类型或文件名不正确")
	}
	if !strings.HasPrefix(string(response.body), "\xef\xbb\xbf") {
		t.Fatal("中文CSV缺少UTF-8 BOM")
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(response.body), "\xef\xbb\xbf"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestExportScopeFiltersAndWithdrawn 验证列表导出口径一致、自然日期时区、公式处理和默认内部消息隔离，跨租户和普通客服无越权。
func TestExportScopeFiltersAndWithdrawn(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, contact, conversation := f.visitor("+8613800000000")
	_, _, excluded := f.visitor("")
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, "UPDATE contacts SET display_name='=2+3' WHERE id=$1", contact); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at='2026-10-25T16:30:00Z',source_snapshot=jsonb_set(source_snapshot,'{entry,channel_code}','\"test-channel\"') WHERE id=$1", conversation); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(ctx, "UPDATE conversations SET created_at='2026-10-26T16:30:00Z' WHERE id=$1", excluded); err != nil {
		t.Fatal(err)
	}
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "=HYPERLINK(\"https://example.invalid\")\n第二行,有逗号"})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "仅内部备注", "audience": "internal"})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "撤回秘密正文"})
	withdrawn := mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+withdrawn["id"].(string)+"/withdraw", "", token, map[string]any{})
	mustData(t, status, reply)
	attachment := newID()
	if _, err := f.admin.Exec(ctx, "INSERT INTO attachments(id,tenant_id,uploader_id,uploader_kind,file_name,storage_key,content_type,size_bytes,sha256) VALUES($1,$2,$3,'visitor','撤回秘密文件.pdf','test-private-path','application/pdf',1,'test-only')", attachment, f.tenant, contact); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "message_type": "file", "attachment_id": attachment, "content": "撤回附件描述"})
	fileMessage := mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+fileMessage["id"].(string)+"/withdraw", "", token, map[string]any{})
	mustData(t, status, reply)
	filters := "from=2026-10-26&to=2026-10-26&query=%3D2%2B3&status=active&agent_id=" + f.agent + "&channel_code=test-channel"
	status, reply = f.request("GET", "/api/v1/tenant/chat-records?"+filters, f.owner, "", nil)
	list := mustData(t, status, reply)["items"].([]any)
	metadata := f.downloadCSV("/api/v1/tenant/chat-records/export?"+filters, f.owner, "")
	rows := csvRecords(t, metadata)
	if len(list) != 1 || len(rows) != 2 || rows[1][0] != list[0].(map[string]any)["id"] {
		t.Fatal("列表与导出的过滤条件不一致")
	}
	if len(rows[0]) != 10 || rows[1][2] != "'=2+3" || rows[1][3] != "'+8613800000000" || rows[1][8] != "2026-10-26T00:30:00+08:00" {
		t.Fatalf("元数据字段、时区或公式防护错误: %v", rows[1])
	}
	if metadata.headers.Get("X-Export-Row-Count") != "1" || metadata.headers.Get("X-Export-Timezone") != "Asia/Shanghai" {
		t.Fatal("导出行数或时区说明不正确")
	}
	bodyCSV := f.downloadCSV("/api/v1/tenant/chat-records/export?include_messages=true&"+filters, f.owner, "")
	rows = csvRecords(t, bodyCSV)
	if len(rows) != 4 || bodyCSV.headers.Get("X-Export-Row-Count") != "3" {
		t.Fatalf("正文默认只应含3条公共消息，得到%d行", len(rows)-1)
	}
	if strings.Contains(string(bodyCSV.body), "仅内部备注") || strings.Contains(string(bodyCSV.body), "撤回秘密") || strings.Contains(string(bodyCSV.body), "撤回附件描述") || strings.Contains(string(bodyCSV.body), "test-private-path") {
		t.Fatal("导出泄漏内部、撤回内容或私有存储键")
	}
	formulaFound := false
	for _, row := range rows[1:] {
		if strings.HasPrefix(row[16], "'=HYPERLINK") {
			formulaFound = true
		}
		if row[18] == "true" && (row[16] != "" || row[17] != "") {
			t.Fatal("撤回消息仍包含正文或附件名称")
		}
	}
	if !formulaFound {
		t.Fatal("公式正文未转成文本或多行CSV解析错误")
	}
	internalCSV := f.downloadCSV("/api/v1/tenant/chat-records/export?include_messages=true&include_internal=true&"+filters, f.owner, "")
	rows = csvRecords(t, internalCSV)
	if len(rows) != 5 || !strings.Contains(string(internalCSV.body), "仅内部备注") {
		t.Fatal("管理员显式选择后应能导出内部备注")
	}
	for _, test := range []struct {
		member, token string
		status        int
	}{{f.agent, "", 403}, {"", token, 401}} {
		response := f.downloadCSV("/api/v1/tenant/chat-records/export?include_messages=true", test.member, test.token)
		if response.status != test.status {
			t.Fatalf("无导出权限应拒绝: %d", response.status)
		}
	}
	other := f.downloadCSV("/api/v1/tenant/chat-records/export?include_messages=true&tenant_id="+f.tenant, f.otherOwner, "")
	rows = csvRecords(t, other)
	if len(rows) != 1 || strings.Contains(string(other.body), conversation) {
		t.Fatal("跨租户导出泄漏数据")
	}
	f.s.SetFeatureChecker(func(ctx context.Context, tx pgx.Tx, tenant, feature string) error {
		if feature == "export" {
			return platform.Fail(403, "feature_not_available", "导出权益已关闭")
		}
		return nil
	})
	if response := f.downloadCSV("/api/v1/tenant/chat-records/export", f.owner, ""); response.status != 403 {
		t.Fatalf("关闭导出权益应拒绝: %d", response.status)
	}
	status, reply = f.request("GET", "/api/v1/tenant/chat-records?"+filters, f.owner, "", nil)
	mustData(t, status, reply)
	var auditCount int
	if err := f.admin.QueryRow(ctx, "SELECT count(*) FROM chat_audit_logs WHERE action='chat_records.exported' AND tenant_id=$1", f.tenant).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("仅成功下载应记录3条审计: %d %v", auditCount, err)
	}
}

// TestExportRowLimitRejectsEntireFile 验证超10000条消息明确拒绝且没有半份文件或成功审计，恰好10000条全部导出。
func TestExportRowLimitRejectsEntireFile(t *testing.T) {
	f := newFixture(t)
	_, contact, conversation := f.visitor("")
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, `INSERT INTO messages(tenant_id,conversation_id,sender_id,sender_kind,client_message_id,message_type,content,event_seq) SELECT $1::uuid,$2::uuid,$3::uuid,'visitor',gen_random_uuid(),'text','容量测试',100+number FROM generate_series(1,10001) number`, f.tenant, conversation, contact); err != nil {
		t.Fatal(err)
	}
	response := f.downloadCSV("/api/v1/tenant/chat-records/export?include_messages=true", f.owner, "")
	var failure map[string]any
	_ = json.Unmarshal(response.body, &failure)
	if response.status != 409 || failure["code"] != "export_limit_exceeded" || strings.HasPrefix(string(response.body), "\xef\xbb\xbf") {
		t.Fatalf("超量应整份拒绝: %d %v", response.status, failure["code"])
	}
	if _, err := f.admin.Exec(ctx, "DELETE FROM messages WHERE conversation_id=$1 AND event_seq=10101", conversation); err != nil {
		t.Fatal(err)
	}
	response = f.downloadCSV("/api/v1/tenant/chat-records/export?include_messages=true", f.owner, "")
	rows := csvRecords(t, response)
	if len(rows) != 10001 || response.headers.Get("X-Export-Row-Count") != "10000" {
		t.Fatalf("恰好10000条必须完整导出: %d", len(rows)-1)
	}
	var audits int
	if err := f.admin.QueryRow(ctx, "SELECT count(*) FROM chat_audit_logs WHERE action='chat_records.exported'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("超限失败不得记录下载成功: %d %v", audits, err)
	}
}

// TestExportMetadataAndByteLimits 验证会话元数据也执行10000行限制，大正文在行数未超限时仍受32MiB文件上限保护。
func TestExportMetadataAndByteLimits(t *testing.T) {
	f := newFixture(t)
	_, contact, conversation := f.visitor("")
	ctx := context.Background()
	if _, err := f.admin.Exec(ctx, `INSERT INTO conversations(tenant_id,service_space_id,contact_id,created_entry_id,service_group_id,routing_pool_id,status,closed_at)
	SELECT c.tenant_id,c.service_space_id,c.contact_id,c.created_entry_id,c.service_group_id,c.routing_pool_id,'closed',now() FROM conversations c CROSS JOIN generate_series(1,10001) WHERE c.id=$1`, conversation); err != nil {
		t.Fatal(err)
	}
	response := f.downloadCSV("/api/v1/tenant/chat-records/export?status=closed", f.owner, "")
	var failure map[string]any
	_ = json.Unmarshal(response.body, &failure)
	if response.status != 409 || failure["code"] != "export_limit_exceeded" {
		t.Fatalf("元数据超10000行须整份拒绝: %d %v", response.status, failure["code"])
	}
	if _, err := f.admin.Exec(ctx, `INSERT INTO messages(tenant_id,conversation_id,sender_id,sender_kind,client_message_id,message_type,content,event_seq)
	SELECT $1::uuid,$2::uuid,$3::uuid,'visitor',gen_random_uuid(),'text',repeat('x',10000),100+number FROM generate_series(1,3500) number`, f.tenant, conversation, contact); err != nil {
		t.Fatal(err)
	}
	response = f.downloadCSV("/api/v1/tenant/chat-records/export?include_messages=true&status=queued", f.owner, "")
	_ = json.Unmarshal(response.body, &failure)
	if response.status != 409 || failure["code"] != "export_size_exceeded" || strings.HasPrefix(string(response.body), "\xef\xbb\xbf") {
		t.Fatalf("大文件须整份拒绝: %d %v", response.status, failure["code"])
	}
}
