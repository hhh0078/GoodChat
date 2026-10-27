-- 文件作用说明：新增租户技能、池与入口技能约束、历史接待优先、平滑加权轮询状态和分配决策快照，不改写历史接待及容量；回退需先停用引用新字段的应用版本。
ALTER TABLE routing_pools DROP CONSTRAINT routing_pools_algorithm_check;
ALTER TABLE routing_pools ADD CONSTRAINT routing_pools_algorithm_check CHECK(algorithm IN('least_load','round_robin','manual','least_ratio','random','weighted_round_robin'));
COMMENT ON CONSTRAINT routing_pools_algorithm_check ON routing_pools IS '主分配算法，仅允许已实现并受统一资格约束的策略';
ALTER TABLE routing_pools ADD COLUMN history_priority boolean NOT NULL DEFAULT false;
COMMENT ON COLUMN routing_pools.history_priority IS '新分配时优先最近已结束咨询的实际接待人，仍检查当前范围技能在线和容量';
ALTER TABLE conversation_assignments ADD COLUMN decision jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(decision)='object');
COMMENT ON COLUMN conversation_assignments.decision IS '此次分配算法、优先原因和权益约束快照，不改变历史接待归属';

CREATE TABLE routing_skills(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES tenants(id),name text NOT NULL CHECK(length(name) BETWEEN 1 AND 80),archived boolean NOT NULL DEFAULT false,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(tenant_id,id),UNIQUE(tenant_id,name));
COMMENT ON TABLE routing_skills IS '租户客服技能字典，与客服业务组及客户标签分开管理';
COMMENT ON COLUMN routing_skills.id IS '技能唯一标识';
COMMENT ON COLUMN routing_skills.tenant_id IS '技能所属租户';
COMMENT ON COLUMN routing_skills.name IS '技能显示名称，租户内唯一';
COMMENT ON COLUMN routing_skills.archived IS '是否归档，归档技能不参与新增选择且不能满足必备条件';
COMMENT ON COLUMN routing_skills.created_at IS '技能创建时间';

CREATE TABLE member_routing_skills(tenant_id uuid NOT NULL,membership_id uuid NOT NULL,skill_id uuid NOT NULL,PRIMARY KEY(tenant_id,membership_id,skill_id),FOREIGN KEY(tenant_id,membership_id) REFERENCES tenant_memberships(tenant_id,id),FOREIGN KEY(tenant_id,skill_id) REFERENCES routing_skills(tenant_id,id));
COMMENT ON TABLE member_routing_skills IS '租户成员具备的接待技能，复合外键禁止跨租户关联';
COMMENT ON COLUMN member_routing_skills.tenant_id IS '成员与技能共同所属租户';
COMMENT ON COLUMN member_routing_skills.membership_id IS '具有该技能的租户成员';
COMMENT ON COLUMN member_routing_skills.skill_id IS '关联的租户技能';

CREATE TABLE routing_pool_skills(tenant_id uuid NOT NULL,routing_pool_id uuid NOT NULL,skill_id uuid NOT NULL,is_required boolean NOT NULL,PRIMARY KEY(tenant_id,routing_pool_id,skill_id),FOREIGN KEY(tenant_id,routing_pool_id) REFERENCES routing_pools(tenant_id,id),FOREIGN KEY(tenant_id,skill_id) REFERENCES routing_skills(tenant_id,id));
COMMENT ON TABLE routing_pool_skills IS '分配池的必备技能过滤与可选技能排序规则';
COMMENT ON COLUMN routing_pool_skills.tenant_id IS '池与技能共同所属租户';
COMMENT ON COLUMN routing_pool_skills.routing_pool_id IS '适用的分配池';
COMMENT ON COLUMN routing_pool_skills.skill_id IS '关联的租户技能';
COMMENT ON COLUMN routing_pool_skills.is_required IS '真为必须全部具备，假为按匹配数量优先排序';

CREATE TABLE access_link_skills(tenant_id uuid NOT NULL,access_link_id uuid NOT NULL,skill_id uuid NOT NULL,is_required boolean NOT NULL,PRIMARY KEY(tenant_id,access_link_id,skill_id),FOREIGN KEY(tenant_id,access_link_id) REFERENCES access_links(tenant_id,id),FOREIGN KEY(tenant_id,skill_id) REFERENCES routing_skills(tenant_id,id));
COMMENT ON TABLE access_link_skills IS '入口附加技能约束，与选定分配池规则叠加且不能放宽池要求';
COMMENT ON COLUMN access_link_skills.tenant_id IS '入口与技能共同所属租户';
COMMENT ON COLUMN access_link_skills.access_link_id IS '适用的接入链接';
COMMENT ON COLUMN access_link_skills.skill_id IS '关联的租户技能';
COMMENT ON COLUMN access_link_skills.is_required IS '真为必备技能，假为可选排序技能，同技能必备优先';

CREATE TABLE routing_weight_state(tenant_id uuid NOT NULL,routing_pool_id uuid NOT NULL,membership_id uuid NOT NULL,current_weight bigint NOT NULL DEFAULT 0,PRIMARY KEY(tenant_id,routing_pool_id,membership_id),FOREIGN KEY(tenant_id,routing_pool_id) REFERENCES routing_pools(tenant_id,id),FOREIGN KEY(tenant_id,membership_id) REFERENCES tenant_memberships(tenant_id,id));
COMMENT ON TABLE routing_weight_state IS '池行锁保护的平滑加权轮询持久积分，只有实际分配成功才推进';
COMMENT ON COLUMN routing_weight_state.tenant_id IS '分配池与成员共同所属租户';
COMMENT ON COLUMN routing_weight_state.routing_pool_id IS '积分所属分配池';
COMMENT ON COLUMN routing_weight_state.membership_id IS '参与加权轮询的租户成员';
COMMENT ON COLUMN routing_weight_state.current_weight IS '平滑加权轮询累计积分，配置变化时重置';

DO $$ DECLARE t text; BEGIN
FOREACH t IN ARRAY ARRAY['routing_skills','member_routing_skills','routing_pool_skills','access_link_skills','routing_weight_state'] LOOP
EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
EXECUTE format('CREATE POLICY tenant_isolation ON %I USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id())',t);
END LOOP; END $$;
CREATE INDEX member_skills_by_skill ON member_routing_skills(tenant_id,skill_id,membership_id);
