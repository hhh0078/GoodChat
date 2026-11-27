// 文件作用说明：真实数据库验证同邮箱并发接受邀请和旧成员重新启用不能产生多个工作台。
package auth

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestSingleWorkspaceInvitationRace 两个未过期邀请同时激活只能加入一个租户，且不能再启用另一租户的旧成员关系。
func TestSingleWorkspaceInvitationRace(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	first, firstCookie := f.register(t, "workspace-first@example.com")
	second, secondCookie := f.register(t, "workspace-second@example.com")
	user, session := f.register(t, "workspace-invitee@example.com")
	if _, err := f.pool.Exec(ctx, "UPDATE tenant_memberships SET disabled=true WHERE user_id=$1", user.User.ID); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"email": user.User.Email, "role": "agent", "can_serve": false}
	requireStatus(t, f.call("POST", "/api/v1/tenant/invitations", input, firstCookie, first.ActiveTenantID), 201)
	firstToken := f.invitationToken(t)
	requireStatus(t, f.call("POST", "/api/v1/tenant/invitations", input, secondCookie, second.ActiveTenantID), 201)
	secondToken := f.invitationToken(t)
	responses := make(chan *httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for _, token := range []string{firstToken, secondToken} {
		group.Add(1)
		go func() {
			defer group.Done()
			responses <- f.call("POST", "/api/v1/auth/accept-invitation", map[string]string{"token": token}, session, "")
		}()
	}
	group.Wait()
	close(responses)
	accepted, rejected := 0, 0
	for response := range responses {
		switch response.Code {
		case 200:
			accepted++
		case 409:
			if !strings.Contains(response.Body.String(), "workspace_exists") {
				t.Fatalf("跨工作台拒绝原因不明确：%s", response.Body.String())
			}
			rejected++
		default:
			t.Fatalf("并发接受邀请返回意外状态：%d %s", response.Code, response.Body.String())
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatal("同一账号只能成功接受一个工作台邀请")
	}
	var activeTenant string
	var count int
	if err := f.pool.QueryRow(ctx, "SELECT count(*),min(tenant_id::text) FROM tenant_memberships WHERE user_id=$1 AND NOT disabled AND removed_at IS NULL", user.User.ID).Scan(&count, &activeTenant); err != nil || count != 1 {
		t.Fatal("并发邀请产生多个有效成员关系")
	}
	otherTenant, ownerCookie := first.ActiveTenantID, firstCookie
	if activeTenant == first.ActiveTenantID {
		otherTenant, ownerCookie = second.ActiveTenantID, secondCookie
	}
	var previous string
	if err := f.pool.QueryRow(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id,role,disabled) VALUES($1,$2,'agent',true) RETURNING id::text", otherTenant, user.User.ID).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	response := f.call("PATCH", "/api/v1/tenant/members/"+previous, map[string]bool{"disabled": false}, ownerCookie, otherTenant)
	requireStatus(t, response, 409)
	if !strings.Contains(response.Body.String(), "workspace_exists") {
		t.Fatal("旧成员重新启用应明确拒绝创建第二工作台")
	}
	var disabled bool
	if err := f.pool.QueryRow(ctx, "SELECT disabled FROM tenant_memberships WHERE id=$1", previous).Scan(&disabled); err != nil || !disabled {
		t.Fatal("被拒绝的启用操作不能改变历史成员状态")
	}
}

// TestPlatformCannotCreateWorkspaceForExistingAccount 代开第三方租户时也拒绝已有工作台账号，不遗留空租户或邀请。
func TestPlatformCannotCreateWorkspaceForExistingAccount(t *testing.T) {
	f := identityIntegration(t)
	ctx := context.Background()
	actor, session := f.register(t, "workspace-platform@example.com")
	user, _ := f.register(t, "workspace-existing@example.com")
	if _, err := f.pool.Exec(ctx, "UPDATE users SET is_platform_admin=true WHERE id=$1", actor.User.ID); err != nil {
		t.Fatal(err)
	}
	response := f.platformCall("/api/v1/platform/tenants", "duplicate-account", map[string]any{"name": "不应创建", "admin_email": user.User.Email}, session)
	requireStatus(t, response, 409)
	if !strings.Contains(response.Body.String(), "workspace_exists") {
		t.Fatal("平台代开需要遵守账号唯一工作台约束")
	}
	var tenants, invitations int
	if err := f.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM tenants),(SELECT count(*) FROM member_invitations)").Scan(&tenants, &invitations); err != nil || tenants != 2 || invitations != 0 {
		t.Fatal("拒绝代开后不能留下空租户或邀请")
	}
}
