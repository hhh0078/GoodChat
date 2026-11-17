-- 文件作用说明：新增无向量 AI 客服的模型版本、知识库、素材话术、机器人、持久任务及整数微元账本，保留现有装修和聊天路径。
CREATE TABLE ai_chat_models (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, enabled boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ai_chat_model_versions (
 model_id uuid NOT NULL REFERENCES ai_chat_models(id), version bigint NOT NULL,
 config jsonb NOT NULL, secret_ciphertext text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(model_id,version)
);
CREATE TABLE ai_wallets (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id), balance_micro bigint NOT NULL DEFAULT 0 CHECK(balance_micro>=0),
 reserved_micro bigint NOT NULL DEFAULT 0 CHECK(reserved_micro>=0 AND reserved_micro<=balance_micro), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ai_knowledge_bases (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), owner_membership_id uuid NOT NULL REFERENCES tenant_memberships(id),
 title text NOT NULL, draft_body text NOT NULL, published_body text NOT NULL DEFAULT '', description text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'pending' CHECK(status IN('pending','ready','failed')), version bigint NOT NULL DEFAULT 1, published_version bigint NOT NULL DEFAULT 0,
 enabled boolean NOT NULL DEFAULT true, archived boolean NOT NULL DEFAULT false, error_code text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,id)
);
CREATE TABLE ai_media (
 id uuid PRIMARY KEY REFERENCES attachments(id), tenant_id uuid NOT NULL REFERENCES tenants(id), owner_membership_id uuid NOT NULL REFERENCES tenant_memberships(id),
 title text NOT NULL, enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,id)
);
CREATE TABLE ai_scripts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), owner_membership_id uuid NOT NULL REFERENCES tenant_memberships(id),
 title text NOT NULL, description text NOT NULL CHECK(length(trim(description))>0), language text NOT NULL DEFAULT 'zh-CN', blocks jsonb NOT NULL,
 version bigint NOT NULL DEFAULT 1, enabled boolean NOT NULL DEFAULT true, archived boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,id)
);
CREATE TABLE ai_bots (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), owner_membership_id uuid NOT NULL REFERENCES tenant_memberships(id),
 model_id uuid REFERENCES ai_chat_models(id), avatar_ref text NOT NULL DEFAULT 'default-01', name text NOT NULL DEFAULT 'AI', mode text NOT NULL DEFAULT 'off' CHECK(mode IN('off','assistant','auto')),
 prompt text NOT NULL DEFAULT '', language text NOT NULL DEFAULT 'auto', knowledge_ids uuid[] NOT NULL DEFAULT '{}', script_ids uuid[] NOT NULL DEFAULT '{}',
 settings jsonb NOT NULL DEFAULT '{}', version bigint NOT NULL DEFAULT 1, updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,owner_membership_id), UNIQUE(tenant_id,id)
);
CREATE TABLE ai_tenant_settings (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id), default_bot_id uuid, reception_mode text NOT NULL DEFAULT 'human_first' CHECK(reception_mode IN('human_first','ai_first')),
 updated_at timestamptz NOT NULL DEFAULT now(), FOREIGN KEY(tenant_id,default_bot_id) REFERENCES ai_bots(tenant_id,id)
);
ALTER TABLE conversations ADD COLUMN ai_paused boolean NOT NULL DEFAULT false, ADD COLUMN ai_epoch bigint NOT NULL DEFAULT 0;
CREATE TABLE ai_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), kind text NOT NULL CHECK(kind IN('summary','reply','test','restore')),
 bot_id uuid, knowledge_id uuid, conversation_id uuid REFERENCES conversations(id), trigger_message_id uuid REFERENCES messages(id), owner_membership_id uuid NOT NULL REFERENCES tenant_memberships(id),
 status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','running','draft','succeeded','failed','cancelled','uncertain')),
 snapshot jsonb NOT NULL, result jsonb NOT NULL DEFAULT '{}', version bigint NOT NULL DEFAULT 1, reserved_micro bigint NOT NULL DEFAULT 0,
 input_tokens bigint NOT NULL DEFAULT 0, output_tokens bigint NOT NULL DEFAULT 0, image_inputs integer NOT NULL DEFAULT 0, cost_micro bigint NOT NULL DEFAULT 0, sale_micro bigint NOT NULL DEFAULT 0,
 error_code text NOT NULL DEFAULT '', billing_status text NOT NULL DEFAULT 'not_called', run_after timestamptz NOT NULL DEFAULT now(), lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz,
 FOREIGN KEY(tenant_id,bot_id) REFERENCES ai_bots(tenant_id,id), FOREIGN KEY(tenant_id,knowledge_id) REFERENCES ai_knowledge_bases(tenant_id,id),
 UNIQUE(tenant_id,trigger_message_id)
);
CREATE INDEX ai_jobs_pending ON ai_jobs(tenant_id,run_after) WHERE status IN('queued','running');
CREATE TABLE ai_call_attempts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), job_id uuid NOT NULL REFERENCES ai_jobs(id), call_index integer NOT NULL,
 model_id uuid NOT NULL REFERENCES ai_chat_models(id), model_version bigint NOT NULL, status text NOT NULL DEFAULT 'started', response_meta jsonb NOT NULL DEFAULT '{}',
 input_tokens bigint NOT NULL DEFAULT 0, output_tokens bigint NOT NULL DEFAULT 0, image_inputs integer NOT NULL DEFAULT 0, cost_micro bigint NOT NULL DEFAULT 0, sale_micro bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz, UNIQUE(job_id,call_index)
);
CREATE TABLE ai_wallet_entries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), job_id uuid REFERENCES ai_jobs(id),
 delta_micro bigint NOT NULL, kind text NOT NULL CHECK(kind IN('topup','charge','adjustment')), reason text NOT NULL DEFAULT '',
 client_key text NOT NULL, created_by uuid REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,client_key)
);
ALTER TABLE messages ADD COLUMN ai_job_id uuid REFERENCES ai_jobs(id), ADD COLUMN ai_bot_id uuid REFERENCES ai_bots(id);
CREATE INDEX messages_ai_job ON messages(ai_job_id) WHERE ai_job_id IS NOT NULL;
COMMENT ON TABLE ai_chat_models IS '平台客服模型当前开关和配置版本';
COMMENT ON COLUMN ai_chat_models.id IS '模型编号';
COMMENT ON COLUMN ai_chat_models.name IS '平台模型名称';
COMMENT ON COLUMN ai_chat_models.enabled IS '是否允许新任务使用';
COMMENT ON COLUMN ai_chat_models.version IS '当前不可变配置版本';
COMMENT ON COLUMN ai_chat_models.created_at IS '创建时间';
COMMENT ON COLUMN ai_chat_models.updated_at IS '修改时间';
COMMENT ON TABLE ai_chat_model_versions IS '平台客服模型不可变配置及价格版本';
COMMENT ON COLUMN ai_chat_model_versions.model_id IS '平台模型编号';
COMMENT ON COLUMN ai_chat_model_versions.version IS '配置和价格版本';
COMMENT ON COLUMN ai_chat_model_versions.config IS '不含明文密钥的模型调用上限及四项每百万Token费率，费率以微元计';
COMMENT ON COLUMN ai_chat_model_versions.secret_ciphertext IS '绑定模型用途的服务端加密密钥';
COMMENT ON COLUMN ai_chat_model_versions.created_at IS '版本创建时间';
COMMENT ON TABLE ai_wallets IS '租户AI人民币微元余额及预算预占';
COMMENT ON COLUMN ai_wallets.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_wallets.balance_micro IS '账面余额，百万微元为一元';
COMMENT ON COLUMN ai_wallets.reserved_micro IS '未结算任务预占金额';
COMMENT ON COLUMN ai_wallets.updated_at IS '余额变更时间';
ALTER TABLE ai_wallets ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_wallets FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_wallets USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_knowledge_bases IS '租户纯文本知识库及正文描述原子发布版本';
COMMENT ON COLUMN ai_knowledge_bases.id IS '知识库编号';
COMMENT ON COLUMN ai_knowledge_bases.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_knowledge_bases.owner_membership_id IS '创建成员';
COMMENT ON COLUMN ai_knowledge_bases.title IS '知识库名称';
COMMENT ON COLUMN ai_knowledge_bases.draft_body IS '最新编辑的纯文本正文';
COMMENT ON COLUMN ai_knowledge_bases.published_body IS '描述生成成功后发布的正文';
COMMENT ON COLUMN ai_knowledge_bases.description IS 'AI生成或人工修正的使用场景描述';
COMMENT ON COLUMN ai_knowledge_bases.status IS '最新草稿描述生成状态';
COMMENT ON COLUMN ai_knowledge_bases.version IS '草稿乐观版本';
COMMENT ON COLUMN ai_knowledge_bases.published_version IS '当前发布正文描述版本';
COMMENT ON COLUMN ai_knowledge_bases.enabled IS '是否允许机器人读取';
COMMENT ON COLUMN ai_knowledge_bases.archived IS '是否已移除';
COMMENT ON COLUMN ai_knowledge_bases.error_code IS '安全错误分类';
COMMENT ON COLUMN ai_knowledge_bases.created_at IS '创建时间';
COMMENT ON COLUMN ai_knowledge_bases.updated_at IS '修改时间';
ALTER TABLE ai_knowledge_bases ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_knowledge_bases FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_knowledge_bases USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_media IS '租户团队素材库，私有聊天附件需主动导入';
COMMENT ON COLUMN ai_media.id IS '对应私有附件编号';
COMMENT ON COLUMN ai_media.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_media.owner_membership_id IS '上传成员';
COMMENT ON COLUMN ai_media.title IS '素材名称';
COMMENT ON COLUMN ai_media.enabled IS '是否可供新的话术引用';
COMMENT ON COLUMN ai_media.created_at IS '导入素材库时间';
ALTER TABLE ai_media ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_media FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_media USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_scripts IS '租户AI富媒体话术及使用条件';
COMMENT ON COLUMN ai_scripts.id IS '话术编号';
COMMENT ON COLUMN ai_scripts.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_scripts.owner_membership_id IS '创建成员，仅本人维护';
COMMENT ON COLUMN ai_scripts.title IS '话术名称';
COMMENT ON COLUMN ai_scripts.description IS '必填使用场景描述';
COMMENT ON COLUMN ai_scripts.language IS '素材与话术适用语言';
COMMENT ON COLUMN ai_scripts.blocks IS '按发送顺序排列的文字或媒体编号内容块';
COMMENT ON COLUMN ai_scripts.version IS '话术乐观版本';
COMMENT ON COLUMN ai_scripts.enabled IS '是否允许新的机器人回复使用';
COMMENT ON COLUMN ai_scripts.archived IS '是否已移除';
COMMENT ON COLUMN ai_scripts.created_at IS '创建时间';
COMMENT ON COLUMN ai_scripts.updated_at IS '修改时间';
ALTER TABLE ai_scripts ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_scripts FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_scripts USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_bots IS '每租户成员唯一客服机器人';
COMMENT ON COLUMN ai_bots.id IS '机器人编号';
COMMENT ON COLUMN ai_bots.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_bots.owner_membership_id IS '机器人所属成员';
COMMENT ON COLUMN ai_bots.model_id IS '平台允许的模型，空值使用平台默认';
COMMENT ON COLUMN ai_bots.name IS '机器人展示名称';
COMMENT ON COLUMN ai_bots.mode IS '关闭、辅助或自动模式';
COMMENT ON COLUMN ai_bots.prompt IS '服务角色与语气约束';
COMMENT ON COLUMN ai_bots.language IS '固定回复语言或自动识别';
COMMENT ON COLUMN ai_bots.knowledge_ids IS '明确勾选的知识库编号';
COMMENT ON COLUMN ai_bots.script_ids IS '明确勾选的AI话术编号';
COMMENT ON COLUMN ai_bots.settings IS '接待时间、离线策略、额度和单次输出限制';
COMMENT ON COLUMN ai_bots.version IS '设置乐观版本';
COMMENT ON COLUMN ai_bots.updated_at IS '修改时间';
ALTER TABLE ai_bots ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_bots FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_bots USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_tenant_settings IS '租户默认AI接待策略';
COMMENT ON COLUMN ai_tenant_settings.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_tenant_settings.default_bot_id IS '未分配会话使用的默认机器人';
COMMENT ON COLUMN ai_tenant_settings.reception_mode IS '人工优先或AI优先';
COMMENT ON COLUMN ai_tenant_settings.updated_at IS '策略修改时间';
ALTER TABLE ai_tenant_settings ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_tenant_settings FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_tenant_settings USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_jobs IS '持久AI任务、回复草稿及结算总计';
COMMENT ON COLUMN ai_jobs.id IS '任务编号';
COMMENT ON COLUMN ai_jobs.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_jobs.kind IS '知识描述、客服回复、调试或知识回滚审计';
COMMENT ON COLUMN ai_jobs.bot_id IS '使用的机器人';
COMMENT ON COLUMN ai_jobs.knowledge_id IS '生成描述的知识库';
COMMENT ON COLUMN ai_jobs.conversation_id IS '关联会话';
COMMENT ON COLUMN ai_jobs.trigger_message_id IS '触发的真实访客消息，唯一约束防重复';
COMMENT ON COLUMN ai_jobs.owner_membership_id IS '费用归属及权限成员';
COMMENT ON COLUMN ai_jobs.status IS '任务执行及待核对状态';
COMMENT ON COLUMN ai_jobs.snapshot IS '冻结的模型版本、接待版本及授权目录，不含明文密钥';
COMMENT ON COLUMN ai_jobs.result IS '经过校验的待发送回复或描述';
COMMENT ON COLUMN ai_jobs.version IS '任务版本';
COMMENT ON COLUMN ai_jobs.reserved_micro IS '本任务预占微元';
COMMENT ON COLUMN ai_jobs.input_tokens IS '实际输入Token合计';
COMMENT ON COLUMN ai_jobs.output_tokens IS '实际输出Token合计';
COMMENT ON COLUMN ai_jobs.cost_micro IS '供应商实际用量按配置费率计算的成本微元';
COMMENT ON COLUMN ai_jobs.sale_micro IS '按配置售价计算的微元';
COMMENT ON COLUMN ai_jobs.error_code IS '安全错误分类';
COMMENT ON COLUMN ai_jobs.billing_status IS '未调用、实际用量或待核对';
COMMENT ON COLUMN ai_jobs.run_after IS '允许执行时间';
COMMENT ON COLUMN ai_jobs.lease_until IS '工作器租约截止时间';
COMMENT ON COLUMN ai_jobs.created_at IS '任务创建时间';
COMMENT ON COLUMN ai_jobs.completed_at IS '终止时间';
ALTER TABLE ai_jobs ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_jobs FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_jobs USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_call_attempts IS '每次真实供应商调用及未知计费审计';
COMMENT ON COLUMN ai_call_attempts.id IS '调用编号';
COMMENT ON COLUMN ai_call_attempts.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_call_attempts.job_id IS '所属逻辑任务';
COMMENT ON COLUMN ai_call_attempts.call_index IS '任务内调用序号';
COMMENT ON COLUMN ai_call_attempts.model_id IS '实际模型编号';
COMMENT ON COLUMN ai_call_attempts.model_version IS '冻结的价格和调用配置版本';
COMMENT ON COLUMN ai_call_attempts.status IS 'started、reported、uncertain或rejected';
COMMENT ON COLUMN ai_call_attempts.input_tokens IS '供应商报告输入Token';
COMMENT ON COLUMN ai_call_attempts.output_tokens IS '供应商报告输出Token';
COMMENT ON COLUMN ai_call_attempts.cost_micro IS '本次调用成本微元';
COMMENT ON COLUMN ai_call_attempts.sale_micro IS '本次调用售价微元';
COMMENT ON COLUMN ai_call_attempts.created_at IS '调用开始时间';
COMMENT ON COLUMN ai_call_attempts.completed_at IS '调用结束时间';
ALTER TABLE ai_call_attempts ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_call_attempts FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_call_attempts USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON TABLE ai_wallet_entries IS '租户AI充值及消费不可变流水';
COMMENT ON COLUMN ai_wallet_entries.id IS '流水编号';
COMMENT ON COLUMN ai_wallet_entries.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_wallet_entries.job_id IS '关联结算任务';
COMMENT ON COLUMN ai_wallet_entries.delta_micro IS '余额变化，充值为正消费为负';
COMMENT ON COLUMN ai_wallet_entries.kind IS '充值、消费或人工调整';
COMMENT ON COLUMN ai_wallet_entries.reason IS '流水原因或待核对处理说明';
COMMENT ON COLUMN ai_wallet_entries.client_key IS '租户内唯一幂等键';
COMMENT ON COLUMN ai_wallet_entries.created_by IS '人工操作平台账号';
COMMENT ON COLUMN ai_wallet_entries.created_at IS '流水创建时间';
ALTER TABLE ai_wallet_entries ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_wallet_entries FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_wallet_entries USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
COMMENT ON COLUMN conversations.ai_paused IS '人工接管后暂停机器人自动回复';
COMMENT ON COLUMN conversations.ai_epoch IS '人工控制版本，迟到AI结果必须校验';
COMMENT ON COLUMN messages.ai_job_id IS 'AI任务来源，不计为人工首响';
COMMENT ON COLUMN messages.ai_bot_id IS '用于冻结机器人发送身份';
COMMENT ON COLUMN ai_jobs.image_inputs IS '供应商已报告调用中图片输入次数，同图重复送入模型按当次输入Token计费';
COMMENT ON COLUMN ai_call_attempts.image_inputs IS '本次模型请求包含的访客图片数量，不额外按图片张数收费';

CREATE UNIQUE INDEX ai_one_charge_per_job ON ai_wallet_entries(job_id) WHERE kind='charge';
ALTER TABLE ai_call_attempts ADD CONSTRAINT ai_attempt_price_version FOREIGN KEY(model_id,model_version) REFERENCES ai_chat_model_versions(model_id,version);
CREATE FUNCTION ai_immutable_ledger() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'AI价格版本与资金流水不可修改'; END $$;
COMMENT ON FUNCTION ai_immutable_ledger() IS '保护AI价格版本和资金流水，修改须创建新版本或反向调整记录';
CREATE TRIGGER ai_price_immutable BEFORE UPDATE OR DELETE ON ai_chat_model_versions FOR EACH ROW EXECUTE FUNCTION ai_immutable_ledger();
CREATE TRIGGER ai_wallet_entry_immutable BEFORE UPDATE OR DELETE ON ai_wallet_entries FOR EACH ROW EXECUTE FUNCTION ai_immutable_ledger();
COMMENT ON COLUMN ai_call_attempts.response_meta IS '脱敏工具名称、调用数量和结束原因，不含模型原文及客户资料';
COMMENT ON COLUMN ai_bots.avatar_ref IS '机器人平台头像编号，发送身份按任务快照冻结';
