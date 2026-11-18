// 文件作用说明：验证邮件扫描不退订、一键POST幂等退订、签名防伪及安全通知不受影响。
package notifications

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestSignedUnsubscribe 验证公开退订端点不依赖登录且只能修改签名指定的运营订阅。
func TestSignedUnsubscribe(t *testing.T) {
	f := integration(t)
	f.s.cfg.AuthSecret = strings.Repeat("k", 32)
	ctx := context.Background()
	f.admin.Exec(ctx, "INSERT INTO mail_notification_preferences(user_id,digest_enabled,updates_enabled) VALUES($1,true,true)", f.user)
	link, e := f.s.unsubscribeURL(f.user, "digest", "/unsubscribe", "en", "UTC")
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(link)
	token := u.Query().Get("token")
	get := httptest.NewRequest(http.MethodGet, "/api/v1/public/mail-unsubscribe?token="+url.QueryEscape(token), nil)
	if e := f.s.unsubscribe(httptest.NewRecorder(), get); e != nil {
		t.Fatal(e)
	}
	var digest, updates bool
	f.admin.QueryRow(ctx, "SELECT digest_enabled,updates_enabled FROM mail_notification_preferences WHERE user_id=$1", f.user).Scan(&digest, &updates)
	if !digest || !updates {
		t.Fatal("链接扫描改变订阅")
	}
	post := httptest.NewRequest(http.MethodPost, get.URL.String(), strings.NewReader("List-Unsubscribe=One-Click"))
	for i := 0; i < 2; i++ {
		if e := f.s.unsubscribe(httptest.NewRecorder(), post); e != nil {
			t.Fatal(e)
		}
	}
	f.admin.QueryRow(ctx, "SELECT digest_enabled,updates_enabled FROM mail_notification_preferences WHERE user_id=$1", f.user).Scan(&digest, &updates)
	if digest || !updates {
		t.Fatal("退订越权到其他类别")
	}
	if _, e := f.s.verifyUnsubscribe(token + "tampered"); e == nil {
		t.Fatal("伪造签名未拒绝")
	}
	f.enqueue(t, "security-after-optout")
	if e := f.s.tick(ctx); e != nil {
		t.Fatal(e)
	}
	if f.status(t, "security-after-optout") != "accepted" {
		t.Fatal("安全通知受退订影响")
	}
}
