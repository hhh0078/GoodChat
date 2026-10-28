// 文件作用说明：集中处理链接接待方式兼容和可诊断的客服领取资格，保留租户组池技能与容量限制。
package chat

import (
	"context"

	"github.com/jackc/pgx/v5"
	"supportdesk/backend/internal/platform"
)

// linkAlgorithm 根据入口覆盖选择自动分配或人工领取，继承模式保留已有池配置。
// algorithm 为池算法，mode 为入口接待方式，返回本次实际使用的算法。
func linkAlgorithm(algorithm, mode string) string {
	if mode == "manual" {
		return "manual"
	}
	if mode == "auto" && algorithm == "manual" {
		return "least_load"
	}
	return algorithm
}

// candidateFailure 返回当前客服被排除的具体原因，不改变统一候选筛选或透露其他租户配置。
// v 为当前受锁保护的会话路由，member 为请求领取或确认的客服编号。
func candidateFailure(ctx context.Context, tx pgx.Tx, v routeState, member string) error {
	var reason string
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT CASE
 WHEN NOT m.can_serve OR m.disabled OR m.removed_at IS NOT NULL THEN '当前账号已停用或没有客服接待权限'
 WHEN p.archived OR g.archived THEN '该咨询的客服组或分配池已归档，请联系管理员'
 WHEN NOT EXISTS(SELECT 1 FROM agent_group_members gm WHERE gm.tenant_id=m.tenant_id AND gm.group_id=c.service_group_id AND gm.membership_id=m.id) THEN '当前客服未加入该咨询所属的客服组'
 WHEN p.service_group_id<>c.service_group_id THEN '该咨询的客服组与分配池不兼容，请联系管理员'
 WHEN p.members_mode='fixed' AND NOT EXISTS(SELECT 1 FROM routing_pool_members pm WHERE pm.tenant_id=m.tenant_id AND pm.routing_pool_id=p.id AND pm.membership_id=m.id) THEN '当前客服未加入该咨询的指定分配池'
 WHEN EXISTS(SELECT 1 FROM unnest($4::uuid[]) requirement(skill_id) WHERE NOT EXISTS(SELECT 1 FROM member_routing_skills ms JOIN routing_skills sk ON sk.id=ms.skill_id AND sk.tenant_id=ms.tenant_id WHERE ms.membership_id=m.id AND ms.skill_id=requirement.skill_id AND NOT sk.archived)) THEN '当前客服缺少必备技能：'||COALESCE((SELECT string_agg(sk.name,'、' ORDER BY sk.name) FROM routing_skills sk WHERE sk.id=ANY($4::uuid[]) AND sk.tenant_id=m.tenant_id AND (sk.archived OR NOT EXISTS(SELECT 1 FROM member_routing_skills ms WHERE ms.skill_id=sk.id AND ms.membership_id=m.id))),'请联系管理员检查技能配置')
 WHEN NOT COALESCE(r.available,false) THEN '当前客服处于下班状态，请先切换为上班'
 WHEN COALESCE(r.busy,false) THEN '当前客服处于忙碌状态，请先切换为可接待'
 WHEN r.online_until IS NULL OR r.online_until<=statement_timestamp() THEN '当前客服实时连接已离线，请恢复连接后重试'
 WHEN r.current_load>m.capacity OR (r.current_load=m.capacity AND m.id<>NULLIF($5,'')::uuid) OR (r.current_load=m.capacity AND $5='') THEN '当前客服接待容量已满，请结束其他咨询后重试'
 WHEN c.rejected_agents ? m.id::text THEN '当前客服已拒绝或超时未接此咨询，本轮不会再次分配，请联系管理员调整'
 ELSE '客服接待资格已变化，请刷新后重试' END
 FROM tenant_memberships m JOIN conversations c ON c.id=$3 AND c.tenant_id=m.tenant_id
 JOIN routing_pools p ON p.id=$2 AND p.tenant_id=m.tenant_id
 JOIN agent_groups g ON g.id=p.service_group_id AND g.tenant_id=m.tenant_id
 LEFT JOIN agent_runtime r ON r.membership_id=m.id AND r.tenant_id=m.tenant_id
 WHERE m.id=$1),'当前账号没有该咨询的接待权限')`, member, v.Pool, v.ID, v.Required, v.ReservedFor).Scan(&reason)
	if err != nil {
		return err
	}
	return platform.Fail(409, "candidate_unavailable", reason)
}
