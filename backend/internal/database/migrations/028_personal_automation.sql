-- 文件作用说明：为客服个人欢迎语和关键词规则增加成员归属、并发版本与归档状态，不改变既有团队规则。
ALTER TABLE welcome_rules DROP CONSTRAINT welcome_rules_scope_type_check;
ALTER TABLE welcome_rules ADD CONSTRAINT welcome_rules_scope_type_check CHECK(scope_type IN('tenant','space','link','member'));
COMMENT ON COLUMN welcome_rules.scope_type IS '租户、服务空间、链接或客服个人欢迎层级';
COMMENT ON COLUMN welcome_rules.scope_id IS '服务空间、链接或客服成员编号，租户层为空';
ALTER TABLE keyword_rules ADD COLUMN owner_membership_id uuid;
ALTER TABLE keyword_rules ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK(version>0);
ALTER TABLE keyword_rules ADD COLUMN archived boolean NOT NULL DEFAULT false;
ALTER TABLE keyword_rules ADD CONSTRAINT keyword_rule_owner FOREIGN KEY(tenant_id,owner_membership_id) REFERENCES tenant_memberships(tenant_id,id);
CREATE INDEX personal_keyword_rules ON keyword_rules(tenant_id,owner_membership_id,priority DESC) WHERE NOT archived;
COMMENT ON COLUMN keyword_rules.owner_membership_id IS '个人规则所属客服成员编号，空值表示租户公共规则';
COMMENT ON COLUMN keyword_rules.version IS '关键词规则并发编辑保护版本';
COMMENT ON COLUMN keyword_rules.archived IS '客服删除后归档并停止匹配，保留已发送消息和任务来源';
COMMENT ON TABLE keyword_rules IS '租户公共与客服个人的访客文字关键词自动回复规则';
