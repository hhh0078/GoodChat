// 文件作用说明：以真实HTTP和隔离PostgreSQL验证头像上传公开读取、租户池隔离、个人固定头像和访客随机稳定性。
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"
)

// uploadAvatarFixture 以真实multipart请求上传图片，伪造的租户字段不得影响素材归属。
func uploadAvatarFixture(t *testing.T, f *fixture, member string, raw []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "头像.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("tenant_id", f.otherTenant); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("POST", f.server.URL+"/api/v1/tenant/avatar-assets", &body)
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
	var payload map[string]any
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

// TestAvatarAssetsAreTenantScopedAndPublicImage 验证普通成员可上传、仅本租户可枚举，公开响应只包含规范化PNG且支持缓存。
func TestAvatarAssetsAreTenantScopedAndPublicImage(t *testing.T) {
	f := newFixture(t)
	status, reply := f.request("GET", "/api/v1/tenant/avatar-assets", f.agent, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 50 {
		t.Fatal("未上传时应完整返回50张平台头像")
	}
	status, reply = uploadAvatarFixture(t, f, f.agent, avatarTestPNG(t, 600, 300))
	if status != 201 {
		t.Fatalf("普通客服上传失败:%d %v", status, reply["code"])
	}
	asset := reply["data"].(map[string]any)
	if asset["width"] != float64(256) || asset["height"] != float64(128) || asset["builtin"] != false {
		t.Fatal("上传没有保存规范化实际尺寸")
	}
	for _, private := range []string{"storage_key", "image_data", "uploaded_by", "tenant_id", "sha256"} {
		if _, ok := asset[private]; ok {
			t.Fatal("上传结果包含内部字段")
		}
	}
	status, reply = f.request("GET", "/api/v1/tenant/avatar-assets", f.owner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 51 {
		t.Fatal("上传未保存至当前租户")
	}
	status, reply = f.request("GET", "/api/v1/tenant/avatar-assets", f.otherOwner, "", nil)
	if len(mustData(t, status, reply)["items"].([]any)) != 50 {
		t.Fatal("其他租户可以枚举本租户上传头像")
	}
	response, err := http.Get(f.server.URL + asset["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || response.StatusCode != 200 || format != "png" || config.Width != 256 {
		t.Fatal("公开头像必须可直接读取实际PNG")
	}
	if response.Header.Get("Content-Type") != "image/png" || response.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(response.Header.Get("Cache-Control"), "immutable") || response.Header.Get("ETag") == "" {
		t.Fatal("公开图片缓存或媒体安全头缺失")
	}
	request, err := http.NewRequest("GET", f.server.URL+asset["url"].(string), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("If-None-Match", response.Header.Get("ETag"))
	cached, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	cached.Body.Close()
	if cached.StatusCode != 304 {
		t.Fatal("不可变头像应支持ETag条件读取")
	}
	status, reply = uploadAvatarFixture(t, f, f.agent, []byte("pretend.png"))
	if status != 400 {
		t.Fatalf("损坏图片不得保存:%d %v", status, reply["code"])
	}
}

// TestVisitorAvatarPoolStableAndScoped 验证访客默认随机头像持久化、显式HTTPS优先、旧客户不被池更改换图及跨租户引用拒绝。
func TestVisitorAvatarPoolStableAndScoped(t *testing.T) {
	f := newFixture(t)
	status, reply := uploadAvatarFixture(t, f, f.agent, avatarTestPNG(t, 20, 20))
	if status != 201 {
		t.Fatal("头像上传失败")
	}
	asset := reply["data"].(map[string]any)
	ref := asset["id"].(string)
	status, reply = f.request("PATCH", "/api/v1/tenant/avatar-pool", f.agent, "", map[string]any{"avatar_refs": []string{ref}})
	if status != 403 {
		t.Fatal("普通客服不能修改访客随机池")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/avatar-pool", f.owner, "", map[string]any{"avatar_refs": []string{ref, ref}})
	pool := mustData(t, status, reply)
	if pool["using_defaults"] != false || len(pool["avatar_refs"].([]any)) != 1 {
		t.Fatal("头像池保存失败或未去重")
	}
	token, contact, conversation := f.visitor("")
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	first := mustData(t, status, reply)
	if first["contact"].(map[string]any)["avatar_url"] != asset["url"] {
		t.Fatal("新访客未使用租户池中唯一头像")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/avatar-pool", f.owner, "", map[string]any{"avatar_refs": []string{"default-02"}})
	mustData(t, status, reply)
	for index := 0; index < 3; index++ {
		status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
		current := mustData(t, status, reply)
		if current["contact"].(map[string]any)["id"] != contact || current["contact"].(map[string]any)["avatar_url"] != asset["url"] || current["conversation"].(map[string]any)["id"] != conversation {
			t.Fatal("刷新或更改头像池导致同一客户头像跳变")
		}
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visit_id": newID(), "profile": map[string]any{"avatar_url": "https://example.invalid/explicit.png"}})
	if mustData(t, status, reply)["contact"].(map[string]any)["avatar_url"] != "https://example.invalid/explicit.png" {
		t.Fatal("明确传入的访客头像必须优先")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/avatar-pool", f.otherOwner, "", map[string]any{"avatar_refs": []string{ref}})
	if status != 400 {
		t.Fatal("头像池不能引用其他租户的上传素材")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/avatar-pool", f.owner, "", map[string]any{"avatar_refs": []string{}})
	defaults := mustData(t, status, reply)
	if defaults["using_defaults"] != true || len(defaults["avatar_refs"].([]any)) != 50 {
		t.Fatal("空数组应恢复默认50张")
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visit_id": newID()})
	if !strings.HasPrefix(mustData(t, status, reply)["contact"].(map[string]any)["avatar_url"].(string), "/images/avatars/default-") {
		t.Fatal("未配置租户池应使用真实平台库头像地址")
	}
}

// TestMemberAvatarFixedAndMessageProjection 验证客服只改本人固定头像、跨租户素材拒绝，实际客服及访客消息均返回发送者头像。
func TestMemberAvatarFixedAndMessageProjection(t *testing.T) {
	f := newFixture(t)
	status, reply := f.request("PATCH", "/api/v1/agent/avatar", f.agent, "", map[string]any{"avatar_ref": "default-07"})
	if mustData(t, status, reply)["avatar_url"] != "/images/avatars/default-07.png" {
		t.Fatal("客服固定头像未保存")
	}
	status, reply = f.request("GET", "/api/v1/agent/avatar", f.owner, "", nil)
	if mustData(t, status, reply)["avatar_ref"] != "" {
		t.Fatal("修改本人头像不应修改其他客服")
	}
	status, reply = uploadAvatarFixture(t, f, f.otherOwner, avatarTestPNG(t, 10, 10))
	if status != 201 {
		t.Fatal("其他租户测试素材上传失败")
	}
	other := reply["data"].(map[string]any)["id"].(string)
	status, _ = f.request("PATCH", "/api/v1/agent/avatar", f.agent, "", map[string]any{"avatar_ref": other})
	if status != 400 {
		t.Fatal("客服固定头像不能引用其他租户素材")
	}
	status, _ = f.request("PATCH", "/api/v1/agent/avatar", f.agent, "", map[string]any{"avatar_ref": "default-05", "membership_id": f.owner})
	if status != 400 {
		t.Fatal("不能通过头像请求指定其他成员")
	}
	f.online(f.agent)
	token, _, conversation := f.visitor("")
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "content": "使用固定头像发送"})
	if mustData(t, status, reply)["sender_avatar_url"] != "/images/avatars/default-07.png" {
		t.Fatal("客服消息缺少发送者固定头像")
	}
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", "", token, map[string]any{"client_message_id": newID(), "content": "访客头像"})
	if !strings.HasPrefix(mustData(t, status, reply)["sender_avatar_url"].(string), "/images/avatars/default-") {
		t.Fatal("访客消息缺少已保存的默认头像")
	}
	status, reply = f.request("PATCH", "/api/v1/agent/avatar", f.agent, "", map[string]any{"avatar_ref": ""})
	if mustData(t, status, reply)["avatar_url"] != "" {
		t.Fatal("本人应可清除固定头像")
	}
}

// TestLegacyVisitorAvatarFillsOnceAndRespectsLocks 验证旧空头像仅首次回访补选，显式头像优先且人工锁定字段保持不变。
func TestLegacyVisitorAvatarFillsOnceAndRespectsLocks(t *testing.T) {
	f := newFixture(t)
	token, contact, _ := f.visitor("")
	status, reply := f.request("PATCH", "/api/v1/tenant/avatar-pool", f.owner, "", map[string]any{"avatar_refs": []string{"default-03"}})
	mustData(t, status, reply)
	if _, err := f.admin.Exec(context.Background(), "UPDATE contacts SET avatar_url='' WHERE id=$1", contact); err != nil {
		t.Fatal(err)
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	if mustData(t, status, reply)["contact"].(map[string]any)["avatar_url"] != "/images/avatars/default-03.png" {
		t.Fatal("旧客户首次回访未补默认头像")
	}
	status, reply = f.request("PATCH", "/api/v1/tenant/avatar-pool", f.owner, "", map[string]any{"avatar_refs": []string{"default-04"}})
	mustData(t, status, reply)
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID()})
	if mustData(t, status, reply)["contact"].(map[string]any)["avatar_url"] != "/images/avatars/default-03.png" {
		t.Fatal("旧客户头像被修改后的池重新分配")
	}
	status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID(), "profile": map[string]any{"avatar_url": "https://example.invalid/customer.png"}})
	if mustData(t, status, reply)["contact"].(map[string]any)["avatar_url"] != "https://example.invalid/customer.png" {
		t.Fatal("明确客户头像不能更新未锁定资料")
	}
	if _, err := f.admin.Exec(context.Background(), `UPDATE contacts SET avatar_url='',locked_fields='["avatar_url"]'::jsonb WHERE id=$1`, contact); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []map[string]any{nil, {"avatar_url": "https://example.invalid/not-allowed.png"}} {
		status, reply = f.request("POST", "/api/v1/visitor/session", "", "", map[string]any{"entry_key": f.entry, "visitor_token": token, "visit_id": newID(), "profile": profile})
		if mustData(t, status, reply)["contact"].(map[string]any)["avatar_url"] != "" {
			t.Fatal("被人工锁定的空头像不可被默认池或链接覆盖")
		}
	}
}

// TestAvatarPermissionsAndReferenceValidation 验证未登录及停用账号不能管理素材，非法平台编号和任意外链不能成为池或个人引用。
func TestAvatarPermissionsAndReferenceValidation(t *testing.T) {
	f := newFixture(t)
	for _, ref := range []string{"default-00", "default-51", "../private", "https://example.invalid/not-library.png", ""} {
		status, _ := f.request("PATCH", "/api/v1/tenant/avatar-pool", f.owner, "", map[string]any{"avatar_refs": []string{ref}})
		if status != 400 {
			t.Fatalf("非法池引用未拒绝:%d", status)
		}
	}
	status, _ := uploadAvatarFixture(t, f, "", avatarTestPNG(t, 10, 10))
	if status != 401 {
		t.Fatalf("未登录不能上传头像:%d", status)
	}
	if _, err := f.admin.Exec(context.Background(), "UPDATE tenant_memberships SET disabled=true WHERE id=$1", f.agent); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/tenant/avatar-assets", "/api/v1/agent/avatar", "/api/v1/tenant/avatar-pool"} {
		status, _ := f.request("GET", path, f.agent, "", nil)
		if status != 403 {
			t.Fatalf("停用成员仍可读取头像管理配置:%s %d", path, status)
		}
	}
	status, _ = uploadAvatarFixture(t, f, f.agent, avatarTestPNG(t, 10, 10))
	if status != 403 {
		t.Fatalf("停用成员仍可上传头像:%d", status)
	}
}

// TestSlowAvatarBodiesDoNotHoldDecodeSlots 验证两个已登录慢传请求不会长期占满全局图片解码配额。
func TestSlowAvatarBodiesDoNotHoldDecodeSlots(t *testing.T) {
	f := newFixture(t)
	for index := 0; index < 2; index++ {
		reader, writer := io.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		started, done := make(chan struct{}), make(chan struct{})
		trace := &httptrace.ClientTrace{Got100Continue: func() { close(started) }}
		request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "POST", f.server.URL+"/api/v1/tenant/avatar-assets", reader)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "multipart/form-data; boundary=slow-avatar")
		request.Header.Set("Expect", "100-continue")
		request.Header.Set("X-Test-Member", f.agent)
		go func() {
			defer close(done)
			response, err := http.DefaultClient.Do(request)
			if err == nil {
				response.Body.Close()
			}
		}()
		t.Cleanup(func() { cancel(); reader.Close(); writer.Close(); <-done })
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("慢传测试未进入真实服务端正文读取")
		}
	}
	if len(f.s.avatarSlots) != 0 {
		t.Fatal("网络正文读取不应占用CPU解码配额")
	}
	status, reply := uploadAvatarFixture(t, f, f.agent, avatarTestPNG(t, 20, 20))
	if status != 201 {
		t.Fatalf("正常头像被其他慢传阻断:%d %v", status, reply["code"])
	}
}
