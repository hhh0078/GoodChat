// 文件作用说明：使用真实PostgreSQL及HTTP验证富话术权限、公共媒体、版本重试、选项编号和成员独立使用次数。
package chat

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"sync"
	"testing"
)

// uploadQuickReplyMedia 通过真实附件接口上传指定字节，不使用声明媒体类型绕过内容嗅探。
func uploadQuickReplyMedia(t *testing.T, f *fixture, member, name string, raw []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("POST", f.server.URL+"/api/v1/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-Test-Member", member)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, result
}

// createQuickReply 通过真实话术管理接口创建指定内容，测试不直接伪造话术表记录。
func createQuickReply(t *testing.T, f *fixture, member string, input map[string]any) map[string]any {
	t.Helper()
	status, reply := f.request("POST", "/api/v1/tenant/quick-replies", member, "", input)
	return mustData(t, status, reply)
}

// replyUseCount 读取当前成员真实接口计数，不从客户端双击次数推算。
func replyUseCount(t *testing.T, f *fixture, member, id string) float64 {
	t.Helper()
	status, reply := f.request("GET", "/api/v1/tenant/quick-replies", member, "", nil)
	for _, raw := range mustData(t, status, reply)["items"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == id {
			return item["use_count"].(float64)
		}
	}
	t.Fatal("当前可见话术不存在")
	return -1
}

// TestRichQuickReplyPublicMediaRetryAndVersion 验证公共媒体精确授权、原编号并发重试仅计一次和停用后仍恢复已保存结果。
func TestRichQuickReplyPublicMediaRetryAndVersion(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	image := uploadWelcomeImage(t, f, f.owner)
	status, _ = f.request("GET", "/api/v1/attachments/"+image, f.agent, "", nil)
	if status == 200 {
		t.Fatal("未共享的他人媒体不应可见")
	}
	shared := createQuickReply(t, f, f.owner, map[string]any{"title": "公共图片", "content": "服务端原始说明", "scope": "public", "message_type": "image", "attachment_id": image})
	id := shared["id"].(string)
	status, _ = f.request("GET", "/api/v1/attachments/"+image, f.agent, "", nil)
	if status != 200 {
		t.Fatal("有效公共话术媒体应允许本租户成员预览")
	}
	status, _ = f.request("GET", "/api/v1/attachments/"+image, "", token, nil)
	if status == 200 {
		t.Fatal("话术配置不直接授权访客媒体")
	}
	status, _ = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "伪造普通附件", "attachment_id": image})
	if status == 200 {
		t.Fatal("文字类型不能绕过他人媒体权限")
	}
	client := newID()
	input := map[string]any{"client_message_id": client, "quick_reply_id": id, "quick_reply_version": shared["version"], "content": "伪造正文", "attachment_id": newID()}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", input)
	message := mustData(t, status, reply)
	if message["content"] != "服务端原始说明" || message["message_type"] != "image" || message["attachment"].(map[string]any)["id"] != image {
		t.Fatal("快捷消息必须使用服务端授权模板")
	}
	var group sync.WaitGroup
	statuses := make(chan int, 12)
	for index := 0; index < 12; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			code, _ := f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", input)
			statuses <- code
		}()
	}
	group.Wait()
	close(statuses)
	for code := range statuses {
		if code != 200 {
			t.Fatalf("原编号并发重试失败%d", code)
		}
	}
	if replyUseCount(t, f, f.agent, id) != 1 || replyUseCount(t, f, f.owner, id) != 0 {
		t.Fatal("使用次数应只累计实际发送客服的首次保存")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/quick-replies/"+id, f.owner, "", map[string]any{"content": "修改后说明", "version": shared["version"]})
	updated := mustData(t, status, reply)
	stale := map[string]any{"client_message_id": newID(), "quick_reply_id": id, "quick_reply_version": shared["version"]}
	status, _ = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", stale)
	if status != 409 {
		t.Fatal("未保存的旧版本必须明确冲突")
	}
	status, _ = f.request("DELETE", "/api/v1/tenant/quick-replies/"+id, f.owner, "", map[string]any{"version": shared["version"]})
	if status != 409 {
		t.Fatal("旧版删除不能覆盖已修改的公共话术")
	}
	status, reply = f.request("DELETE", "/api/v1/tenant/quick-replies/"+id, f.owner, "", map[string]any{"version": updated["version"]})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", input)
	restored := mustData(t, status, reply)
	if restored["id"] != message["id"] || restored["content"] != message["content"] || replyUseCount(t, f, f.agent, id) != 1 {
		t.Fatal("模板修改停用不能破坏已保存原消息的幂等恢复")
	}
	stale["quick_reply_version"] = updated["version"]
	status, _ = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", stale)
	if status != 404 {
		t.Fatal("停用话术不能产生新消息")
	}
	status, _ = f.request("GET", "/api/v1/attachments/"+image, "", token, nil)
	if status != 200 {
		t.Fatal("公开消息应授权当前访客附件")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages/"+message["id"].(string)+"/withdraw", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, _ = f.request("GET", "/api/v1/attachments/"+image, "", token, nil)
	if status == 200 {
		t.Fatal("消息撤回且话术停用后不应保留访客媒体授权")
	}
}

// TestQuickReplyOptionsAndPersonalIsolation 验证选项消息稳定编号、个人和租户隔离，以及按个人使用计数排序。
func TestQuickReplyOptionsAndPersonalIsolation(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	personal := createQuickReply(t, f, f.agent, map[string]any{"title": "个人选项", "content": "请选择", "options": []any{map[string]any{"id": "price", "label": "咨询价格", "value": "我想咨询价格"}}})
	newer := createQuickReply(t, f, f.agent, map[string]any{"title": "个人新话术", "content": "稍后"})
	privateOwner := createQuickReply(t, f, f.owner, map[string]any{"title": "管理员个人", "content": "只给自己看"})
	foreign := createQuickReply(t, f, f.otherOwner, map[string]any{"title": "其他租户", "content": "隔离", "scope": "public"})
	for _, forbidden := range []map[string]any{privateOwner, foreign} {
		status, _ = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "quick_reply_id": forbidden["id"], "quick_reply_version": forbidden["version"]})
		if status != 404 {
			t.Fatal("不能使用他人个人或跨租户话术")
		}
	}
	status, _ = f.request("PATCH", "/api/v1/tenant/quick-replies/"+personal["id"].(string), f.owner, "", map[string]any{"content": "越权"})
	if status != 403 {
		t.Fatal("管理员不应改写他人的个人话术")
	}
	input := map[string]any{"client_message_id": newID(), "quick_reply_id": personal["id"], "quick_reply_version": personal["version"]}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", input)
	message := mustData(t, status, reply)
	options := message["options"].([]any)
	option := options[0].(map[string]any)
	client := option["client_message_id"].(string)
	if !uuidPattern.MatchString(client) {
		t.Fatal("选项缺少服务端生成编号")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", input)
	again := mustData(t, status, reply)
	if again["options"].([]any)[0].(map[string]any)["client_message_id"] != client {
		t.Fatal("选项编号不能在幂等重试时变化")
	}
	for index := 0; index < 3; index++ {
		status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": client, "content": option["value"]})
		mustData(t, status, reply)
	}
	if f.countRows("SELECT count(*) FROM messages WHERE client_message_id=$1", client) != 1 {
		t.Fatal("选项重复点击应只产生一条访客消息")
	}
	status, reply = f.request("GET", "/api/v1/tenant/quick-replies", f.agent, "", nil)
	items := mustData(t, status, reply)["items"].([]any)
	if items[0].(map[string]any)["id"] != personal["id"] || items[1].(map[string]any)["id"] != newer["id"] {
		t.Fatal("个人话术应按真实成功使用次数排序")
	}
	status, _ = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, input)
	if status != 403 {
		t.Fatal("访客不能调用客服快捷模板")
	}
	if replyUseCount(t, f, f.agent, personal["id"].(string)) != 1 {
		t.Fatal("失败或原编号重试不应增加次数")
	}
}

// TestChatVideoMediaAllowlist 精确允许实际嗅探出的常用视频类型，不开放任意video前缀或主动内容。
func TestChatVideoMediaAllowlist(t *testing.T) {
	for _, mime := range []string{"video/mp4", "video/quicktime", "video/webm"} {
		if !allowedMedia(mime) {
			t.Fatalf("缺少允许类型%s", mime)
		}
	}
	for _, mime := range []string{"video/x-executable", "image/svg+xml", "text/html"} {
		if allowedMedia(mime) {
			t.Fatalf("不应允许类型%s", mime)
		}
	}
}

// TestQuickReplyAudioUsesRealUploadedMedia 验证真实PCM音频可作为公共语音发送，伪造扩展名和图片冒充音频会拒绝。
func TestQuickReplyAudioUsesRealUploadedMedia(t *testing.T) {
	f := newFixture(t)
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	wav := make([]byte, 44+16000)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 8000)
	binary.LittleEndian.PutUint32(wav[28:], 16000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], 16000)
	status, reply = uploadQuickReplyMedia(t, f, f.owner, "语音.wav", wav)
	if status != 201 {
		t.Fatalf("真实语音上传失败HTTP%d", status)
	}
	audio := reply["data"].(map[string]any)
	if audio["content_type"] != "audio/wave" {
		t.Fatalf("真实WAV未被识别:%d %v", status, audio["content_type"])
	}
	shared := createQuickReply(t, f, f.owner, map[string]any{"title": "公共语音", "content": "", "scope": "public", "message_type": "audio", "attachment_id": audio["id"]})
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "quick_reply_id": shared["id"], "quick_reply_version": shared["version"]})
	message := mustData(t, status, reply)
	if message["message_type"] != "audio" || message["attachment"].(map[string]any)["id"] != audio["id"] {
		t.Fatal("公共语音未保存为真实授权附件消息")
	}
	status, _ = f.request("GET", "/api/v1/attachments/"+audio["id"].(string), "", token, nil)
	if status != 200 {
		t.Fatal("当前访客不能读取公开语音")
	}
	image := uploadWelcomeImage(t, f, f.owner)
	status, _ = f.request("POST", "/api/v1/tenant/quick-replies", f.owner, "", map[string]any{"title": "错误语音", "content": "", "message_type": "audio", "attachment_id": image})
	if status != 400 {
		t.Fatal("图片不能冒充语音话术")
	}
	status, _ = uploadQuickReplyMedia(t, f, f.owner, "伪造.mp4", []byte("<html><script>doNotRun()</script></html>"))
	if status != 400 {
		t.Fatal("视频扩展名不能跳过实际内容嗅探")
	}
}
