-- 文件作用说明：增加租户 AI 知识待办和逐步调用链路，保留原任务与知识发布路径。
CREATE TABLE ai_pending_items (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 type text NOT NULL DEFAULT 'ai_knowledge' CHECK(type='ai_knowledge'),
 question text NOT NULL, normalized_question text NOT NULL, reason text NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN('pending','supplementing','resolved','ignored','deleted')),
 source_job_id uuid NOT NULL REFERENCES ai_jobs(id), last_job_id uuid NOT NULL REFERENCES ai_jobs(id),
 occurrences integer NOT NULL DEFAULT 1, bot_ids uuid[] NOT NULL DEFAULT '{}', read_ids uuid[] NOT NULL DEFAULT '{}',
 knowledge_id uuid, knowledge_version bigint, handled_by uuid REFERENCES tenant_memberships(id),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,knowledge_id) REFERENCES ai_knowledge_bases(tenant_id,id)
);
CREATE INDEX ai_pending_items_queue ON ai_pending_items(tenant_id,status,created_at DESC,id DESC);
CREATE INDEX ai_pending_items_dedup ON ai_pending_items(tenant_id,md5(normalized_question)) WHERE status<>'resolved';
CREATE TABLE ai_job_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id),
 job_id uuid NOT NULL REFERENCES ai_jobs(id), kind text NOT NULL, name text NOT NULL DEFAULT '',
 payload jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_job_events_chain ON ai_job_events(tenant_id,job_id,id);
CREATE INDEX ai_jobs_history ON ai_jobs(tenant_id,created_at DESC,id DESC);
COMMENT ON TABLE ai_pending_items IS '租户知识缺口待办，忽略和删除保留去重标记';
COMMENT ON COLUMN ai_pending_items.id IS '待办编号';
COMMENT ON COLUMN ai_pending_items.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_pending_items.type IS '待办类型，目前为AI补充';
COMMENT ON COLUMN ai_pending_items.question IS '首次真实访客原始问题';
COMMENT ON COLUMN ai_pending_items.normalized_question IS '标准化问题用于幂等去重';
COMMENT ON COLUMN ai_pending_items.reason IS '缺失知识说明';
COMMENT ON COLUMN ai_pending_items.status IS '待处理、发布中、已补充、已忽略或已删除';
COMMENT ON COLUMN ai_pending_items.source_job_id IS '首次触发的AI任务';
COMMENT ON COLUMN ai_pending_items.last_job_id IS '最近触发的AI任务';
COMMENT ON COLUMN ai_pending_items.occurrences IS '命中同类问题的任务次数';
COMMENT ON COLUMN ai_pending_items.bot_ids IS '需要使用补充知识的机器人编号集合';
COMMENT ON COLUMN ai_pending_items.read_ids IS '报告缺口前实际读取的知识编号集合';
COMMENT ON COLUMN ai_pending_items.knowledge_id IS '补充所写入的知识库';
COMMENT ON COLUMN ai_pending_items.knowledge_version IS '等待发布的知识版本';
COMMENT ON COLUMN ai_pending_items.handled_by IS '最近处理的管理员成员';
COMMENT ON COLUMN ai_pending_items.created_at IS '首次发现时间';
COMMENT ON COLUMN ai_pending_items.updated_at IS '最近发现或处理时间';
ALTER TABLE ai_pending_items ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_pending_items FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_pending_items USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_job_events IS 'AI实际执行链路，不保存密钥或模型内部推理';
COMMENT ON COLUMN ai_job_events.id IS '全局递增事件序号';
COMMENT ON COLUMN ai_job_events.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_job_events.job_id IS '所属AI任务';
COMMENT ON COLUMN ai_job_events.kind IS '模型请求、模型返回、工具开始、工具结果或最终结果';
COMMENT ON COLUMN ai_job_events.name IS '模型或工具名称';
COMMENT ON COLUMN ai_job_events.payload IS '经过脱敏的实际输入输出';
COMMENT ON COLUMN ai_job_events.created_at IS '事件持久化时间';
ALTER TABLE ai_job_events ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_job_events FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_job_events USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
CREATE TABLE ai_pending_occurrences (
 tenant_id uuid NOT NULL REFERENCES tenants(id), job_id uuid NOT NULL REFERENCES ai_jobs(id), item_id uuid NOT NULL,
 PRIMARY KEY(tenant_id,job_id), FOREIGN KEY(tenant_id,item_id) REFERENCES ai_pending_items(tenant_id,id)
);
COMMENT ON TABLE ai_pending_occurrences IS '真实任务与知识缺口命中关系，防止重试重复计数';
COMMENT ON COLUMN ai_pending_occurrences.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_pending_occurrences.job_id IS '实际触发的任务';
COMMENT ON COLUMN ai_pending_occurrences.item_id IS '合并后的待办';
ALTER TABLE ai_pending_occurrences ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_pending_occurrences FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_pending_occurrences USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
