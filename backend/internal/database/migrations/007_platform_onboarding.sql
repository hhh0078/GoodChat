-- 文件作用说明：记录平台代开租户的幂等映射及初始管理员邀请；不修改既有身份迁移。
CREATE TABLE platform_tenant_creations (
 actor_id uuid NOT NULL REFERENCES users(id),
 idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 128),
 request_hash bytea NOT NULL,
 tenant_id uuid NOT NULL UNIQUE REFERENCES tenants(id),
 invitation_id uuid NOT NULL REFERENCES member_invitations(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(actor_id,idempotency_key)
);
COMMENT ON TABLE platform_tenant_creations IS '平台经过鉴权的租户代开幂等记录与当前初始管理员邀请，访问由身份模块显式校验';
COMMENT ON COLUMN platform_tenant_creations.actor_id IS '发起代开的真实平台管理员账号';
COMMENT ON COLUMN platform_tenant_creations.idempotency_key IS '管理员提交并重复使用的创建幂等标识';
COMMENT ON COLUMN platform_tenant_creations.request_hash IS '规范化创建请求摘要，同键不同内容拒绝';
COMMENT ON COLUMN platform_tenant_creations.tenant_id IS '本次原子创建的独立租户编号';
COMMENT ON COLUMN platform_tenant_creations.invitation_id IS '当前有效初始管理员邀请，只有该邀请可在无所有者时晋升所有者';
COMMENT ON COLUMN platform_tenant_creations.created_at IS '首次创建提交时间';
COMMENT ON COLUMN platform_tenant_creations.updated_at IS '重发初始管理员邀请的最近时间';
