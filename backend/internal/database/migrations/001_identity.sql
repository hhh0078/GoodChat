-- 文件作用说明：建立邮箱认证、登录会话、租户成员与邀请数据；身份表由认证服务显式校验访问范围，业务表另行启用租户RLS。
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL UNIQUE CHECK (email = lower(email)),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
    password_hash text NOT NULL,
    is_platform_admin boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE users IS '经过邮箱验证的平台登录账号，不直接保存租户角色';
COMMENT ON COLUMN users.id IS '登录账号唯一标识';
COMMENT ON COLUMN users.email IS '已验证并规范化的小写邮箱，平台唯一';
COMMENT ON COLUMN users.display_name IS '账号默认显示名称';
COMMENT ON COLUMN users.password_hash IS '包含参数与随机盐的Argon2id密码摘要';
COMMENT ON COLUMN users.is_platform_admin IS '受控初始化的平台管理员标记，公开注册不可设置';
COMMENT ON COLUMN users.created_at IS '账号创建时间';

CREATE TABLE tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    slug text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','expired')),
    timezone text NOT NULL DEFAULT 'Asia/Shanghai',
    created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE tenants IS '独立使用GoodChat的商户租户';
COMMENT ON COLUMN tenants.id IS '租户唯一标识';
COMMENT ON COLUMN tenants.name IS '租户显示名称';
COMMENT ON COLUMN tenants.slug IS '不可重复的租户路径标识';
COMMENT ON COLUMN tenants.status IS '租户状态：有效、停用或到期';
COMMENT ON COLUMN tenants.timezone IS '营业与统计使用的IANA时区';
COMMENT ON COLUMN tenants.created_at IS '租户创建时间';

CREATE TABLE tenant_memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    user_id uuid NOT NULL REFERENCES users(id),
    role text NOT NULL CHECK (role IN ('owner','admin','agent')),
    display_name text NOT NULL DEFAULT '' CHECK (length(display_name) <= 80),
    can_serve boolean NOT NULL DEFAULT false,
    disabled boolean NOT NULL DEFAULT false,
    capacity integer NOT NULL DEFAULT 5 CHECK (capacity BETWEEN 1 AND 100),
    removed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id,user_id)
);
CREATE UNIQUE INDEX memberships_one_owner ON tenant_memberships(tenant_id) WHERE role='owner' AND removed_at IS NULL;
CREATE INDEX memberships_by_user ON tenant_memberships(user_id,created_at) WHERE removed_at IS NULL;
COMMENT ON TABLE tenant_memberships IS '账号在指定租户的管理角色与独立接待资格';
COMMENT ON COLUMN tenant_memberships.id IS '成员关系唯一标识';
COMMENT ON COLUMN tenant_memberships.tenant_id IS '所属租户';
COMMENT ON COLUMN tenant_memberships.user_id IS '关联登录账号';
COMMENT ON COLUMN tenant_memberships.role IS '所有者、租户管理员或客服角色';
COMMENT ON COLUMN tenant_memberships.display_name IS '仅本租户使用的成员显示名称，空值回退账号默认名称';
COMMENT ON COLUMN tenant_memberships.can_serve IS '是否具备接待资格，与管理角色独立';
COMMENT ON COLUMN tenant_memberships.disabled IS '成员是否停用，停用立即禁止租户访问';
COMMENT ON COLUMN tenant_memberships.capacity IS '同时接待上限，包含待接单预留';
COMMENT ON COLUMN tenant_memberships.removed_at IS '移出租户时间，保留历史作者及统计关系';
COMMENT ON COLUMN tenant_memberships.created_at IS '加入租户时间';

CREATE TABLE auth_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id),
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_sessions_by_user ON auth_sessions(user_id) WHERE revoked_at IS NULL;
COMMENT ON TABLE auth_sessions IS '可撤销的服务端登录会话，只保存高熵凭证摘要';
COMMENT ON COLUMN auth_sessions.id IS '登录会话唯一标识';
COMMENT ON COLUMN auth_sessions.user_id IS '关联登录账号';
COMMENT ON COLUMN auth_sessions.token_hash IS 'Cookie登录令牌的SHA256摘要，非原始令牌';
COMMENT ON COLUMN auth_sessions.expires_at IS '会话到期时间';
COMMENT ON COLUMN auth_sessions.revoked_at IS '退出、重置密码或管理撤销时间';
COMMENT ON COLUMN auth_sessions.created_at IS '会话创建时间';

CREATE TABLE email_challenges (
    id uuid PRIMARY KEY,
    email text NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('register','login','reset_password','invitation','ownership')),
    code_hash bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts BETWEEN 0 AND 5),
    consumed_at timestamptz,
    revoked_at timestamptz,
    delivery_status text NOT NULL DEFAULT 'pending' CHECK (delivery_status IN ('pending','accepted','unknown','failed')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX challenges_latest ON email_challenges(email,purpose,created_at DESC);
COMMENT ON TABLE email_challenges IS '独立用途及重发世代的邮箱验证码，事务锁定并原子消费';
COMMENT ON COLUMN email_challenges.id IS '本次验证码申请暨重发世代标识';
COMMENT ON COLUMN email_challenges.email IS '验证码绑定的规范化邮箱';
COMMENT ON COLUMN email_challenges.purpose IS '注册、登录、重置、邀请或所有者移交用途';
COMMENT ON COLUMN email_challenges.code_hash IS '以服务端密钥保护的验证码HMAC摘要';
COMMENT ON COLUMN email_challenges.expires_at IS '验证码有效截止时间';
COMMENT ON COLUMN email_challenges.failed_attempts IS '已提交的错误次数，达到五次不可再用';
COMMENT ON COLUMN email_challenges.consumed_at IS '成功原子消费时间';
COMMENT ON COLUMN email_challenges.revoked_at IS '被新申请替代或主动撤销时间';
COMMENT ON COLUMN email_challenges.delivery_status IS 'SMTP待发、受理、结果不确定或失败状态';
COMMENT ON COLUMN email_challenges.created_at IS '验证码申请时间与重发间隔基点';

CREATE TABLE auth_rate_limits (
    key text PRIMARY KEY,
    attempts integer NOT NULL DEFAULT 1,
    expires_at timestamptz NOT NULL
);
COMMENT ON TABLE auth_rate_limits IS '跨进程重启仍有效的认证邮箱及可信IP限流计数';
COMMENT ON COLUMN auth_rate_limits.key IS '经HMAC脱敏并包含时间窗口的限流键';
COMMENT ON COLUMN auth_rate_limits.attempts IS '当前窗口请求次数';
COMMENT ON COLUMN auth_rate_limits.expires_at IS '时间窗口结束暨可清理时间';

CREATE TABLE member_invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    email text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    role text NOT NULL CHECK (role IN ('admin','agent')),
    can_serve boolean NOT NULL,
    capacity integer NOT NULL CHECK (capacity BETWEEN 1 AND 100),
    token_hash bytea NOT NULL UNIQUE,
    invited_by uuid NOT NULL REFERENCES users(id),
    accepted_by uuid REFERENCES users(id),
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at timestamptz,
    delivery_status text NOT NULL DEFAULT 'pending' CHECK (delivery_status IN ('pending','accepted','unknown','failed')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invitations_by_tenant ON member_invitations(tenant_id,created_at DESC);
COMMENT ON TABLE member_invitations IS '绑定原租户及受邀邮箱的管理员或客服邀请';
COMMENT ON COLUMN member_invitations.id IS '邀请唯一标识';
COMMENT ON COLUMN member_invitations.tenant_id IS '受邀加入的既有租户';
COMMENT ON COLUMN member_invitations.email IS '必须验证匹配的受邀邮箱';
COMMENT ON COLUMN member_invitations.display_name IS '建议显示名称，激活时可由本人确认';
COMMENT ON COLUMN member_invitations.role IS '邀请固定的租户管理员或客服角色';
COMMENT ON COLUMN member_invitations.can_serve IS '受邀成员的独立接待资格';
COMMENT ON COLUMN member_invitations.capacity IS '受邀成员接待上限';
COMMENT ON COLUMN member_invitations.token_hash IS '高熵邀请令牌摘要，重发轮换旧凭证';
COMMENT ON COLUMN member_invitations.invited_by IS '发起邀请的登录账号';
COMMENT ON COLUMN member_invitations.accepted_by IS '实际接受邀请的账号';
COMMENT ON COLUMN member_invitations.expires_at IS '邀请到期时间';
COMMENT ON COLUMN member_invitations.accepted_at IS '已成功接受的时间';
COMMENT ON COLUMN member_invitations.revoked_at IS '主动撤销或重发替代时间';
COMMENT ON COLUMN member_invitations.delivery_status IS '最新邀请邮件的真实SMTP结果';
COMMENT ON COLUMN member_invitations.created_at IS '邀请创建时间';

CREATE TABLE email_delivery_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL CHECK (kind IN ('challenge','invitation')),
    source_id uuid NOT NULL,
    recipient text NOT NULL,
    purpose text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','unknown','failed')),
    failure_kind text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE email_delivery_records IS '认证邮件发送记录，不存验证码明文、邀请凭证或SMTP密钥';
COMMENT ON COLUMN email_delivery_records.id IS '邮件发送记录唯一标识';
COMMENT ON COLUMN email_delivery_records.kind IS '验证码或成员邀请邮件';
COMMENT ON COLUMN email_delivery_records.source_id IS '关联验证码申请或邀请标识';
COMMENT ON COLUMN email_delivery_records.recipient IS '收件邮箱，仅授权服务端可访问';
COMMENT ON COLUMN email_delivery_records.purpose IS '邮件业务用途';
COMMENT ON COLUMN email_delivery_records.status IS 'SMTP待发、受理、结果待核对或明确失败';
COMMENT ON COLUMN email_delivery_records.failure_kind IS '安全分类，避免返回SMTP凭证及内部服务器详情';
COMMENT ON COLUMN email_delivery_records.created_at IS '邮件发送申请时间';
COMMENT ON COLUMN email_delivery_records.updated_at IS '最终发送状态更新时间';

CREATE TABLE identity_audit_logs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id uuid REFERENCES tenants(id),
    actor_id uuid REFERENCES users(id),
    action text NOT NULL,
    target_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE identity_audit_logs IS '身份与成员管理操作审计，不保存密码验证码或原始令牌';
COMMENT ON COLUMN identity_audit_logs.id IS '递增审计记录标识';
COMMENT ON COLUMN identity_audit_logs.tenant_id IS '操作关联租户';
COMMENT ON COLUMN identity_audit_logs.actor_id IS '实际操作者账号';
COMMENT ON COLUMN identity_audit_logs.action IS '注册、成员变更、邀请或所有者移交动作';
COMMENT ON COLUMN identity_audit_logs.target_id IS '目标成员或邀请标识';
COMMENT ON COLUMN identity_audit_logs.created_at IS '操作发生时间';
