-- 文件作用说明：建立租户进入回调配置、可靠投递记录和受限任务认领，配置变更不改写历史投递。
CREATE TABLE tenant_callback_settings (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
 enabled boolean NOT NULL DEFAULT false,
 url text NOT NULL DEFAULT '',
 secret_encrypted text NOT NULL,
 include_context boolean NOT NULL DEFAULT false,
 include_phone boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1,
 updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE tenant_callback_settings IS '租户访客进入回调设置';
COMMENT ON COLUMN tenant_callback_settings.tenant_id IS '所属租户编号';
COMMENT ON COLUMN tenant_callback_settings.enabled IS '是否启用进入回调';
COMMENT ON COLUMN tenant_callback_settings.url IS '经过校验的HTTPS回调地址';
COMMENT ON COLUMN tenant_callback_settings.secret_encrypted IS '加密保存的回调签名密钥';
COMMENT ON COLUMN tenant_callback_settings.include_context IS '是否向回调发送本次业务上下文';
COMMENT ON COLUMN tenant_callback_settings.include_phone IS '是否向回调发送访客联系电话';
COMMENT ON COLUMN tenant_callback_settings.version IS '配置版本，变更后旧任务取消';
COMMENT ON COLUMN tenant_callback_settings.updated_at IS '配置最近更新时间';
ALTER TABLE tenant_callback_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_callback_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_callback_settings_scope ON tenant_callback_settings USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());

CREATE TABLE callback_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 event_id text NOT NULL,
 event_type text NOT NULL DEFAULT 'visitor.entered',
 visit_id text NOT NULL,
 payload jsonb NOT NULL,
 target_url text NOT NULL,
 config_version bigint NOT NULL,
 status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','running','retrying','delivered','failed','cancelled')),
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 response_status integer,
 last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 delivered_at timestamptz,
 UNIQUE(tenant_id,event_type,visit_id)
);
COMMENT ON TABLE callback_deliveries IS '访客进入回调投递任务与结果';
COMMENT ON COLUMN callback_deliveries.id IS '投递任务编号';
COMMENT ON COLUMN callback_deliveries.tenant_id IS '所属租户编号';
COMMENT ON COLUMN callback_deliveries.event_id IS '供接收方幂等处理的事件标识';
COMMENT ON COLUMN callback_deliveries.event_type IS '回调事件类型';
COMMENT ON COLUMN callback_deliveries.visit_id IS '同一次访问去重标识';
COMMENT ON COLUMN callback_deliveries.payload IS '按租户设置筛选后的回调数据';
COMMENT ON COLUMN callback_deliveries.target_url IS '任务创建时的目标地址快照';
COMMENT ON COLUMN callback_deliveries.config_version IS '创建任务使用的回调配置版本';
COMMENT ON COLUMN callback_deliveries.status IS '排队运行重试成功失败或取消状态';
COMMENT ON COLUMN callback_deliveries.attempts IS '已尝试投递次数';
COMMENT ON COLUMN callback_deliveries.next_attempt_at IS '下次允许尝试时间';
COMMENT ON COLUMN callback_deliveries.lease_until IS '任务认领租约到期时间';
COMMENT ON COLUMN callback_deliveries.response_status IS '目标服务器HTTP状态码';
COMMENT ON COLUMN callback_deliveries.last_error IS '不含凭证的最近错误说明';
COMMENT ON COLUMN callback_deliveries.created_at IS '任务创建时间';
COMMENT ON COLUMN callback_deliveries.delivered_at IS '目标返回成功的时间';
CREATE INDEX callback_deliveries_due ON callback_deliveries(status,next_attempt_at);
CREATE INDEX callback_deliveries_tenant_created ON callback_deliveries(tenant_id,created_at DESC);
ALTER TABLE callback_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE callback_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY callback_deliveries_scope ON callback_deliveries USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());

CREATE TABLE callback_dispatch_queue (
 delivery_id uuid PRIMARY KEY REFERENCES callback_deliveries(id),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz
);
COMMENT ON TABLE callback_dispatch_queue IS '服务内部回调调度索引，仅保存任务定位不包含客户资料或目标地址';
COMMENT ON COLUMN callback_dispatch_queue.delivery_id IS '等待投递的任务编号';
COMMENT ON COLUMN callback_dispatch_queue.tenant_id IS '任务租户定位，载荷读取仍需租户事务';
COMMENT ON COLUMN callback_dispatch_queue.next_attempt_at IS '下次调度时间';
COMMENT ON COLUMN callback_dispatch_queue.lease_until IS '调度认领租约';
