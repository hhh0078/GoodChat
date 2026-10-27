-- 文件作用说明：补充独立欢迎规则、营业场景、富消息来源和可追溯评价版本，不修改既有迁移或接待路由。
CREATE TABLE automation_settings(
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    settings jsonb NOT NULL DEFAULT '{}',
    version integer NOT NULL DEFAULT 1 CHECK(version>0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE welcome_rules(
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL,
    scope_type text NOT NULL CHECK(scope_type IN('tenant','space','link')),
    scope_id uuid,
    scenario text NOT NULL DEFAULT 'all' CHECK(scenario IN('all','business','outside')),
    enabled boolean NOT NULL DEFAULT true,
    stop_on_agent boolean NOT NULL DEFAULT true,
    contents jsonb NOT NULL DEFAULT '[]',
    version integer NOT NULL DEFAULT 1 CHECK(version>0),
    archived boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id,id),
    CHECK((scope_type='tenant' AND scope_id IS NULL) OR (scope_type<>'tenant' AND scope_id IS NOT NULL))
);
CREATE UNIQUE INDEX welcome_rule_scope ON welcome_rules(tenant_id,scope_type,COALESCE(scope_id,'00000000-0000-0000-0000-000000000000'::uuid),scenario) WHERE NOT archived;
ALTER TABLE chat_operation_jobs ADD COLUMN sort_order integer NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN options jsonb NOT NULL DEFAULT '[]';
ALTER TABLE messages ADD COLUMN source text NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN source_rule_id uuid;
ALTER TABLE messages ADD COLUMN source_version text NOT NULL DEFAULT '';
ALTER TABLE conversation_evaluations ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK(version>0);
ALTER TABLE conversation_evaluations ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE conversation_evaluations ADD COLUMN tags jsonb NOT NULL DEFAULT '[]';
ALTER TABLE conversation_evaluations ADD COLUMN final_agent_id uuid;
ALTER TABLE conversation_evaluations ADD CONSTRAINT evaluation_tenant_id UNIQUE(tenant_id,id);
ALTER TABLE conversation_evaluations ADD CONSTRAINT evaluation_final_agent FOREIGN KEY(tenant_id,final_agent_id) REFERENCES tenant_memberships(tenant_id,id);
UPDATE conversation_evaluations e SET final_agent_id=c.current_agent_id FROM conversations c WHERE c.id=e.conversation_id AND c.tenant_id=e.tenant_id AND c.status='closed';
CREATE TABLE evaluation_versions(
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    evaluation_id uuid NOT NULL,
    version integer NOT NULL CHECK(version>0),
    score integer NOT NULL CHECK(score BETWEEN 1 AND 5),
    comment text NOT NULL DEFAULT '',
    tags jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id,evaluation_id,version),
    FOREIGN KEY(tenant_id,evaluation_id) REFERENCES conversation_evaluations(tenant_id,id)
);
INSERT INTO evaluation_versions(tenant_id,evaluation_id,version,score,comment,tags,created_at)
SELECT tenant_id,id,version,score,comment,tags,created_at FROM conversation_evaluations;
DO $$ DECLARE t text; a record; d jsonb := '{"id":"主键编号","tenant_id":"所属租户编号","settings":"营业时段与评价策略配置","version":"并发保护和历史追溯的递增版本","updated_at":"最近修改时间","name":"欢迎规则名称","scope_type":"租户服务空间或链接覆盖层级","scope_id":"服务空间或链接编号，租户层为空","scenario":"全部营业中或非营业欢迎场景","enabled":"是否允许新触发","stop_on_agent":"人工正式接入后是否取消未发送欢迎内容","contents":"按排序保存的文字图片和选项以及从会话创建计算的绝对延迟","archived":"是否已归档且不再参与继承","created_at":"创建或访客提交时间","evaluation_id":"关联当前评价记录编号","score":"访客提交的一至五星评分","comment":"访客原始文字评价","tags":"访客选择的评价标签"}'::jsonb;
BEGIN
FOREACH t IN ARRAY ARRAY['automation_settings','welcome_rules','evaluation_versions'] LOOP
EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK (tenant_id = nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',t);
FOR a IN SELECT attname FROM pg_attribute WHERE attrelid=('public.'||t)::regclass AND attnum>0 AND NOT attisdropped LOOP
IF NOT d ? a.attname THEN RAISE EXCEPTION '字段缺少中文注释：%.%',t,a.attname; END IF;
EXECUTE format('COMMENT ON COLUMN %I.%I IS %L',t,a.attname,d->>a.attname);
END LOOP;
END LOOP;
END $$;
COMMENT ON TABLE automation_settings IS '租户营业场景和访客评价期限策略';
COMMENT ON TABLE welcome_rules IS '按层级和营业场景覆盖的持久版本欢迎内容';
COMMENT ON TABLE evaluation_versions IS '不可覆盖的访客评价初评及改评历史';
COMMENT ON COLUMN chat_operation_jobs.sort_order IS '同一执行时刻按规则配置顺序发送';
COMMENT ON COLUMN messages.options IS '系统欢迎选项及其稳定访客消息去重编号';
COMMENT ON COLUMN messages.source IS '人工欢迎语或关键词等可辨识来源';
COMMENT ON COLUMN messages.source_rule_id IS '触发系统消息的租户内规则编号';
COMMENT ON COLUMN messages.source_version IS '消息发送时冻结的规则版本或旧配置摘要';
COMMENT ON COLUMN conversation_evaluations.version IS '当前评价的递增并发版本';
COMMENT ON COLUMN conversation_evaluations.updated_at IS '最近一次访客评价提交时间';
COMMENT ON COLUMN conversation_evaluations.tags IS '当前评价的访客选择标签';
COMMENT ON COLUMN conversation_evaluations.final_agent_id IS '首次评价时冻结的最终接待成员，防止转接重复计数';
