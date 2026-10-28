-- 文件作用说明：加速按入口、接待段和评价归属的真实统计，并只为平台监控开放汇总接待容量。
CREATE INDEX contact_visits_link_period ON contact_visits(tenant_id,created_at,access_link_id,contact_id);
CREATE INDEX conversation_contact_history ON conversations(tenant_id,contact_id,created_at DESC,id DESC);
CREATE INDEX contact_visits_contact_history ON contact_visits(tenant_id,contact_id,created_at DESC);
CREATE INDEX assignments_member_period ON conversation_assignments(tenant_id,accepted_at,membership_id,conversation_id);
CREATE INDEX assignments_conversation_segment ON conversation_assignments(tenant_id,conversation_id,membership_id,accepted_at DESC);
CREATE INDEX evaluations_agent_period ON conversation_evaluations(tenant_id,created_at,final_agent_id);
CREATE INDEX messages_member_period ON messages(tenant_id,created_at,sender_id) WHERE sender_kind='agent' AND audience='public';
CREATE FUNCTION platform_reception_capacity() RETURNS TABLE(available_agents bigint,total_capacity bigint,current_load bigint,remaining bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT count(*)FILTER(WHERE r.current_load<m.capacity),coalesce(sum(m.capacity),0)::bigint,coalesce(sum(r.current_load),0)::bigint,coalesce(sum(greatest(m.capacity-r.current_load,0)),0)::bigint
 FROM public.agent_runtime r JOIN public.tenant_memberships m ON m.tenant_id=r.tenant_id AND m.id=r.membership_id JOIN public.tenants t ON t.id=r.tenant_id
 WHERE t.status='active' AND NOT m.disabled AND m.removed_at IS NULL AND m.can_serve AND r.available AND NOT r.busy AND r.online_until>statement_timestamp()
$$;
COMMENT ON FUNCTION platform_reception_capacity() IS '平台监控专用跨租户纯汇总；只返回当前在线上班非忙成员的容量，不返回身份、租户或消息内容';
