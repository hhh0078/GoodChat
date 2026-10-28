// 文件作用说明：验证认证账号的会员头像与成员列表返回当前租户固定头像，不将头像误作账号全局权限字段。
package auth

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMembershipAvatarInAccountAndMembers 验证同一会员固定头像通过真实Cookie身份接口和成员列表一致展示。
func TestMembershipAvatarInAccountAndMembers(t *testing.T) {
	f := identityIntegration(t)
	account, session := f.register(t, "avatar-owner@example.com")
	if len(account.Memberships) != 1 {
		t.Fatal("新账号缺少租户会员")
	}
	member := account.Memberships[0]
	if _, err := f.pool.Exec(context.Background(), "UPDATE tenant_memberships SET avatar_ref='default-11' WHERE id=$1 AND tenant_id=$2", member.ID, member.TenantID); err != nil {
		t.Fatal(err)
	}
	response := f.call("GET", "/api/v1/auth/me", nil, session, member.TenantID)
	requireStatus(t, response, 200)
	current := accountData(t, response)
	if current.Memberships[0].AvatarRef != "default-11" || current.Memberships[0].AvatarURL != "/images/avatars/default-11.png" {
		t.Fatal("账号会员结构没有返回固定头像")
	}
	response = f.call("GET", "/api/v1/tenant/members", nil, session, member.TenantID)
	requireStatus(t, response, 200)
	var payload struct {
		Data struct {
			Items []memberView `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Items) != 1 || payload.Data.Items[0].AvatarRef != "default-11" || payload.Data.Items[0].AvatarURL != "/images/avatars/default-11.png" {
		t.Fatal("成员列表头像与本人会员头像不一致")
	}
}
