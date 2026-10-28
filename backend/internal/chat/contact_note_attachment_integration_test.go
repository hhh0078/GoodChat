// 文件作用说明：通过真实上传与HTTP验证旧内部附件备注的当前客户授权、跨咨询读取、撤回隔离及私有路径保护。
package chat

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// readContactNoteFile 读取实际文件响应及安全头，身份始终放在请求头而非下载链接中。
func readContactNoteFile(t *testing.T, f *fixture, path, member, token string) (int, []byte, http.Header) {
	t.Helper()
	request, err := http.NewRequest("GET", f.server.URL+"/api/v1"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if member != "" {
		request.Header.Set("X-Test-Member", member)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body, response.Header
}

// TestContactNoteAttachmentCurrentScope 验证旧附件实际可读、跨租户与访客被拒、接待变更及时撤权，后续咨询可读取客户旧备注。
func TestContactNoteAttachmentCurrentScope(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	f.online(f.owner)
	token, contact, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	file := uploadLeadFixture(t, f, "", f.agent)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "message_type": "file", "attachment_id": file, "audience": "internal"})
	note := mustData(t, status, reply)["id"].(string)
	path := "/tenant/contacts/" + contact + "/notes/" + note + "/attachment"
	status, reply = f.request("GET", "/api/v1/tenant/contacts/"+contact+"/notes", f.agent, "", nil)
	items := mustData(t, status, reply)["items"].([]any)
	if len(items) != 1 {
		t.Fatal("旧内部附件应保留为一条客户备注")
	}
	item := items[0].(map[string]any)
	metadata, ok := item["attachment"].(map[string]any)
	if !ok || metadata["file_name"] != "lead-file.txt" || metadata["download_path"] != path || item["message_type"] != "file" || item["conversation_id"] != conversation {
		t.Fatal("旧附件备注缺少安全展示和下载元数据")
	}
	if _, leaked := metadata["storage_key"]; leaked {
		t.Fatal("备注不能暴露私有存储键")
	}
	status, body, headers := readContactNoteFile(t, f, path, f.agent, "")
	if status != 200 || string(body) != "真实HTTP留言附件内容" {
		t.Fatalf("授权旧备注必须返回实际文件:%d", status)
	}
	if headers.Get("Cache-Control") != "private, no-store" || headers.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(headers.Get("Content-Disposition"), "attachment;") || !strings.HasPrefix(headers.Get("Content-Type"), "text/plain") {
		t.Fatal("私有下载响应头不完整")
	}
	status, _, _ = readContactNoteFile(t, f, path, f.otherOwner, "")
	if status != 404 {
		t.Fatalf("跨租户不得读取旧备注附件:%d", status)
	}
	status, _, _ = readContactNoteFile(t, f, path, "", token)
	if status != 401 {
		t.Fatalf("访客不得读取内部备注附件:%d", status)
	}
	_, otherContact, _ := f.visitor("")
	status, _, _ = readContactNoteFile(t, f, strings.Replace(path, contact, otherContact, 1), f.owner, "")
	if status != 404 {
		t.Fatalf("不能把其他客户编号与旧备注混搭:%d", status)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/transfer", f.agent, "", map[string]any{"agent_id": f.owner})
	mustData(t, status, reply)
	status, _, _ = readContactNoteFile(t, f, path, f.agent, "")
	if status != 404 {
		t.Fatalf("转出客服应立即失去客户备注附件权限:%d", status)
	}
	status, _, _ = readContactNoteFile(t, f, path, f.owner, "")
	if status != 200 {
		t.Fatalf("当前接待管理员应可读取旧备注附件:%d", status)
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/close", "", token, map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	next := mustData(t, status, reply)["conversation"].(map[string]any)["id"].(string)
	status, reply = f.request("POST", "/api/v1/conversations/"+next+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, _, _ = readContactNoteFile(t, f, path, f.agent, "")
	if status != 200 {
		t.Fatalf("当前客户接待权应支持跨咨询旧备注附件:%d", status)
	}
}

// TestContactNoteAttachmentRejectsWithdrawnAndPublic 验证猜测公开消息、撤回消息或普通文本编号均不能获得内部附件。
func TestContactNoteAttachmentRejectsWithdrawnAndPublic(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	_, contact, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	file := uploadLeadFixture(t, f, "", f.agent)
	for _, audience := range []string{"public", "internal"} {
		status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "message_type": "file", "attachment_id": file, "audience": audience})
		note := mustData(t, status, reply)["id"].(string)
		if audience == "internal" {
			status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+note+"/withdraw", f.agent, "", map[string]any{})
			mustData(t, status, reply)
		}
		status, _, _ = readContactNoteFile(t, f, "/tenant/contacts/"+contact+"/notes/"+note+"/attachment", f.agent, "")
		if status != 404 {
			t.Fatalf("公开或撤回消息不能通过旧备注附件接口读取:%s %d", audience, status)
		}
	}
}

// TestServeChatAttachmentPrivatePath 验证共享流式读取只接受UUID常规文件，不跟随符号链接且保留完整内容。
func TestServeChatAttachmentPrivatePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "chat-attachments"), 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(secret, []byte("不得读取"), 0600); err != nil {
		t.Fatal(err)
	}
	symlink := newID()
	if err := os.Symlink(secret, filepath.Join(dir, "chat-attachments", symlink)); err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: platform.Config{DataDir: dir}}
	for _, key := range []string{"../outside.txt", symlink, newID()} {
		response := httptest.NewRecorder()
		s.serveChatAttachment(response, httptest.NewRequest("GET", "/attachment", nil), key, "private.txt", "text/plain")
		if response.Code != 404 || bytes.Contains(response.Body.Bytes(), []byte("不得读取")) {
			t.Fatal("共享下载未拒绝危险或不存在的路径")
		}
	}
}
