-- 文件作用说明：建立真实套餐、订阅、人工开通申请及审计；仅种子免费两坐席版本，不创建虚构付费价格或支付记录。
CREATE TABLE plans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9_-]{1,39}$'),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    description text NOT NULL DEFAULT '' CHECK (length(description)<=2000),
    price_amount bigint NOT NULL CHECK (price_amount BETWEEN 0 AND 1000000000),
    currency text NOT NULL DEFAULT 'CNY' CHECK (currency='CNY'),
    validity_days integer NOT NULL DEFAULT 0 CHECK (validity_days BETWEEN 0 AND 36500),
    seat_limit integer NOT NULL CHECK (seat_limit BETWEEN 1 AND 100000),
    features jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(features)='object'),
    is_public boolean NOT NULL DEFAULT false,
    is_active boolean NOT NULL DEFAULT true,
    sort_order integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (code<>'free' OR (price_amount=0 AND validity_days=0 AND seat_limit=2 AND is_public AND is_active))
);
COMMENT ON TABLE plans IS '平台管理员配置的套餐版本，公开报价与后台使用相同记录';
COMMENT ON COLUMN plans.id IS '套餐唯一标识';
COMMENT ON COLUMN plans.code IS '稳定版本代码，free保留为免费回退版本';
COMMENT ON COLUMN plans.name IS '官网和控制台展示名称';
COMMENT ON COLUMN plans.description IS '套餐文字说明';
COMMENT ON COLUMN plans.price_amount IS '人民币整数分价格，零表示免费';
COMMENT ON COLUMN plans.currency IS '币种，当前仅支持人民币CNY';
COMMENT ON COLUMN plans.validity_days IS '开通有效天数，零表示长期有效';
COMMENT ON COLUMN plans.seat_limit IS '有效接待成员与待激活接待邀请的总席位上限';
COMMENT ON COLUMN plans.features IS '功能代码到布尔值的权益配置';
COMMENT ON COLUMN plans.is_public IS '是否在官网与租户可选套餐中发布';
COMMENT ON COLUMN plans.is_active IS '是否接受新的开通申请，已开通快照不受下架影响';
COMMENT ON COLUMN plans.sort_order IS '列表展示顺序，数值小者优先';
COMMENT ON COLUMN plans.created_at IS '套餐创建时间';
COMMENT ON COLUMN plans.updated_at IS '套餐最近修改时间';

CREATE TABLE tenant_subscriptions (
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    plan_id uuid NOT NULL REFERENCES plans(id),
    plan_snapshot jsonb NOT NULL CHECK (jsonb_typeof(plan_snapshot)='object'),
    started_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz,
    activated_by uuid REFERENCES users(id),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE tenant_subscriptions IS '每租户当前订阅，付费权益保存申请快照，到期实时回退免费权益';
COMMENT ON COLUMN tenant_subscriptions.tenant_id IS '订阅所属租户';
COMMENT ON COLUMN tenant_subscriptions.plan_id IS '最近开通的套餐版本';
COMMENT ON COLUMN tenant_subscriptions.plan_snapshot IS '开通时价格、有效期、席位和功能的完整快照';
COMMENT ON COLUMN tenant_subscriptions.started_at IS '最近开通起始时间';
COMMENT ON COLUMN tenant_subscriptions.expires_at IS '有效截止时间，空值为长期有效';
COMMENT ON COLUMN tenant_subscriptions.activated_by IS '人工开通的平台管理员，自动免费初始化时为空';
COMMENT ON COLUMN tenant_subscriptions.updated_at IS '订阅最近修改时间';

CREATE TABLE upgrade_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    plan_id uuid NOT NULL REFERENCES plans(id),
    plan_snapshot jsonb NOT NULL CHECK (jsonb_typeof(plan_snapshot)='object'),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','activated','cancelled')),
    requested_by uuid NOT NULL REFERENCES users(id),
    activated_by uuid REFERENCES users(id),
    payment_reference text NOT NULL DEFAULT '' CHECK (length(payment_reference)<=500),
    created_at timestamptz NOT NULL DEFAULT now(),
    activated_at timestamptz
);
CREATE UNIQUE INDEX upgrade_requests_one_pending ON upgrade_requests(tenant_id,plan_id) WHERE status='pending';
CREATE INDEX upgrade_requests_by_tenant ON upgrade_requests(tenant_id,created_at DESC);
COMMENT ON TABLE upgrade_requests IS '真实套餐开通申请，人工确认实际收款后开通，不代表系统执行在线支付';
COMMENT ON COLUMN upgrade_requests.id IS '开通申请唯一标识，也是重复开通幂等键';
COMMENT ON COLUMN upgrade_requests.tenant_id IS '申请租户';
COMMENT ON COLUMN upgrade_requests.plan_id IS '申请套餐';
COMMENT ON COLUMN upgrade_requests.plan_snapshot IS '提交申请时锁定的完整价格和权益快照';
COMMENT ON COLUMN upgrade_requests.status IS '待核实开通、已开通或已取消';
COMMENT ON COLUMN upgrade_requests.requested_by IS '提交申请的租户管理员';
COMMENT ON COLUMN upgrade_requests.activated_by IS '确认实际收款并开通的平台管理员';
COMMENT ON COLUMN upgrade_requests.payment_reference IS '管理员填写的实际收款参考，不存支付账户凭证';
COMMENT ON COLUMN upgrade_requests.created_at IS '提交申请时间';
COMMENT ON COLUMN upgrade_requests.activated_at IS '成功开通时间';

CREATE TABLE billing_audit_logs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id uuid REFERENCES tenants(id),
    actor_id uuid NOT NULL REFERENCES users(id),
    action text NOT NULL,
    target_id uuid NOT NULL,
    details jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE billing_audit_logs IS '套餐配置与人工开通审计，不包含聊天正文和支付密钥';
COMMENT ON COLUMN billing_audit_logs.id IS '递增审计标识';
COMMENT ON COLUMN billing_audit_logs.tenant_id IS '操作租户，平台套餐配置时为空';
COMMENT ON COLUMN billing_audit_logs.actor_id IS '经过认证的操作者';
COMMENT ON COLUMN billing_audit_logs.action IS '套餐创建、修改、开通或租户状态修改';
COMMENT ON COLUMN billing_audit_logs.target_id IS '目标套餐、申请或租户';
COMMENT ON COLUMN billing_audit_logs.details IS '价格权益快照或状态变更内容';
COMMENT ON COLUMN billing_audit_logs.created_at IS '操作时间';

CREATE FUNCTION billing_platform_actor() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM users WHERE id=nullif(current_setting('app.billing_actor',true),'')::uuid AND is_platform_admin)
$$;
COMMENT ON FUNCTION billing_platform_actor() IS '验证平台事务注入的操作者仍为平台管理员，不接受客户端指定的身份';
ALTER TABLE tenant_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY subscription_scope ON tenant_subscriptions USING (tenant_id=current_tenant_id() OR billing_platform_actor()) WITH CHECK (tenant_id=current_tenant_id() OR billing_platform_actor());
ALTER TABLE upgrade_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE upgrade_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY upgrade_scope ON upgrade_requests USING (tenant_id=current_tenant_id() OR billing_platform_actor()) WITH CHECK (tenant_id=current_tenant_id() OR billing_platform_actor());
ALTER TABLE billing_audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_audit_logs FORCE ROW LEVEL SECURITY;
CREATE POLICY billing_audit_scope ON billing_audit_logs USING (tenant_id=current_tenant_id() OR billing_platform_actor()) WITH CHECK (tenant_id=current_tenant_id() OR billing_platform_actor());

INSERT INTO plans(code,name,description,price_amount,seat_limit,features,is_public,is_active,sort_order)
VALUES('free','免费版','长期免费，包含2个客服坐席；更多接待成员可申请升级。',0,2,'{"chat":true,"multi_link":true,"labels":true,"statistics":true,"decoration":true,"advanced_decoration":true,"ai_design":true,"callbacks":true,"export":true}',true,true,0);
