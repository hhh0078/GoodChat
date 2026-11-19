// 文件作用说明：显式开启时提供隔离真实数据库与消息接口供浏览器填写表单，退出后回收模式，不接触生产。
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFormsBrowserRuntime 向私有本地文件写入一次验收所需身份，不在控制台或仓库输出令牌。
func TestFormsBrowserRuntime(t *testing.T) {
	path := os.Getenv("GOODCHAT_FORMS_BROWSER_STATE")
	if path == "" {
		t.Skip("仅显式浏览器验收启用")
	}
	f := newFixture(t)
	f.s.cfg.PublicURL = "http://127.0.0.1:13002"
	_ = os.Remove(path + ".done")
	ctx := context.Background()
	f.online(f.agent)
	token, contact, conversation := f.visitor("")
	status, reply := f.request("POST", "/api/v1/conversations/"+conversation+"/assignments/claim", f.agent, "", map[string]any{})
	mustData(t, status, reply)
	fields := []formField{{ID: newID(), Label: "姓名", Required: true, Type: "text"}, {ID: newID(), Label: "需求说明", Type: "textarea"}}
	q := createQuickReply(t, f, f.owner, map[string]any{"title": "咨询需求登记", "scope": "public", "message_type": "form", "form_fields": fields})
	status, reply = f.request("POST", "/api/v1/conversations/"+conversation+"/messages", f.agent, "", map[string]any{"client_message_id": newID(), "quick_reply_id": q["id"], "quick_reply_version": q["version"]})
	m := mustData(t, status, reply)
	var space string
	if e := f.admin.QueryRow(ctx, "SELECT service_space_id::text FROM access_links WHERE entry_key=$1", f.entry).Scan(&space); e != nil {
		t.Fatal(e)
	}
	info := map[string]any{"entry": f.entry, "token": token, "contact": contact, "conversation": conversation, "space": space, "owner": f.owner, "agent": f.agent, "principal": f.principals[f.owner], "fields": fields, "request": m["id"]}
	raw, _ := json.Marshal(info)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Addr: "127.0.0.1:18080", Handler: f.server.Config.Handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.ListenAndServe() }()
	defer server.Close()
	timer := time.NewTimer(20 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("浏览器验收超时")
		case <-ticker.C:
			f.online(f.agent)
			if _, e := os.Stat(path + ".done"); e == nil {
				return
			}
		}
	}
}
