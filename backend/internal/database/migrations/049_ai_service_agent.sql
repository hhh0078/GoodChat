-- 文件作用说明：为每个租户提供独立于人工登录坐席的默认AI接待成员，接入链接可显式选择，既有机器人配置继续复用。
CREATE TABLE ai_service_agents (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL UNIQUE REFERENCES tenants(id) ON DELETE CASCADE,
 name text NOT NULL DEFAULT 'AI客服' CHECK(length(name) BETWEEN 1 AND 60),
 avatar_ref text NOT NULL DEFAULT 'goodchat',
 enabled boolean NOT NULL DEFAULT true,
 priority integer NOT NULL DEFAULT 100 CHECK(priority BETWEEN 1 AND 100),
 description text NOT NULL DEFAULT '' CHECK(length(description)<=500),
 version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id)
);
COMMENT ON TABLE ai_service_agents IS '租户默认AI接待成员，不创建登录账号、不占用人工坐席，暂不提供删除操作';
COMMENT ON COLUMN ai_service_agents.id IS '稳定AI成员编号';
COMMENT ON COLUMN ai_service_agents.tenant_id IS '所属租户，每个租户固定一个默认AI成员';
COMMENT ON COLUMN ai_service_agents.name IS '对外显示名称，与现有默认机器人初始化保持一致';
COMMENT ON COLUMN ai_service_agents.avatar_ref IS '平台受控头像编号';
COMMENT ON COLUMN ai_service_agents.enabled IS '是否允许以AI成员身份自动接待';
COMMENT ON COLUMN ai_service_agents.priority IS '默认接待优先级，AI初始100，人工分配继续使用既有规则';
COMMENT ON COLUMN ai_service_agents.description IS '管理员可编辑的服务说明，不替代机器人的回答要求';
COMMENT ON COLUMN ai_service_agents.version IS '资料乐观并发版本';
COMMENT ON COLUMN ai_service_agents.created_at IS '成员建立时间';
COMMENT ON COLUMN ai_service_agents.updated_at IS '资料更新时间';
ALTER TABLE ai_service_agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_service_agents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON ai_service_agents USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
INSERT INTO ai_service_agents(tenant_id,name,avatar_ref)
SELECT t.id,coalesce(b.name,'AI客服'),coalesce(b.avatar_ref,'goodchat') FROM tenants t
LEFT JOIN ai_tenant_settings s ON s.tenant_id=t.id LEFT JOIN ai_bots b ON b.id=s.default_bot_id;
CREATE FUNCTION ensure_default_ai_service_agent() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN INSERT INTO ai_service_agents(tenant_id) VALUES(NEW.id) ON CONFLICT(tenant_id) DO NOTHING; RETURN NEW; END $$;
COMMENT ON FUNCTION ensure_default_ai_service_agent() IS '新租户创建时自动建立不可登录的默认AI接待成员';
REVOKE ALL ON FUNCTION ensure_default_ai_service_agent() FROM PUBLIC;
CREATE TRIGGER tenant_default_ai_service_agent AFTER INSERT ON tenants FOR EACH ROW EXECUTE FUNCTION ensure_default_ai_service_agent();
ALTER TABLE access_links ADD COLUMN ai_agent_id uuid;
COMMENT ON COLUMN access_links.ai_agent_id IS '可选的AI接待成员；AI自动接待，转人工后仍按本入口接待方式和指定人工配置执行';
ALTER TABLE access_links ADD FOREIGN KEY(tenant_id,ai_agent_id) REFERENCES ai_service_agents(tenant_id,id);
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN GRANT SELECT,INSERT,UPDATE ON ai_service_agents TO goodchat_saas_app; END IF; END $$;
