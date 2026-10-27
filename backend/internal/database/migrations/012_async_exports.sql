-- 文件作用说明：建立租户管理员异步CSV任务、短期结果和无客户载荷的调度索引，不改变原同步导出限额。
CREATE TABLE chat_export_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 requested_by uuid NOT NULL REFERENCES tenant_memberships(id),
 client_request_id uuid NOT NULL,
 filters text NOT NULL CHECK(length(filters)<=4000),
 status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','running','completed','failed','cancelled')),
 row_count integer NOT NULL DEFAULT 0 CHECK(row_count>=0),
 timezone text NOT NULL DEFAULT '',
 csv_body bytea,
 size_bytes integer NOT NULL DEFAULT 0,
 error_code text NOT NULL DEFAULT '',
 error_message text NOT NULL DEFAULT '',
 lease_token uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 started_at timestamptz,
 completed_at timestamptz,
 expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours',
 UNIQUE(tenant_id,requested_by,client_request_id)
);
COMMENT ON TABLE chat_export_jobs IS '管理员异步聊天导出及24小时有效结果，仅申请者可下载；结果到期后清理';
COMMENT ON COLUMN chat_export_jobs.id IS '异步导出任务编号';
COMMENT ON COLUMN chat_export_jobs.tenant_id IS '认证上下文确定的租户';
COMMENT ON COLUMN chat_export_jobs.requested_by IS '提出申请的本租户管理员成员';
COMMENT ON COLUMN chat_export_jobs.client_request_id IS '客户端生成并在提交重试时复用的幂等编号';
COMMENT ON COLUMN chat_export_jobs.filters IS '已校验的规范查询参数，包含独立正文和内部备注选项';
COMMENT ON COLUMN chat_export_jobs.status IS '排队、运行、完成、失败或取消状态';
COMMENT ON COLUMN chat_export_jobs.row_count IS '已处理的实际数据行数，不冒充预计百分比';
COMMENT ON COLUMN chat_export_jobs.timezone IS '实际生成使用的租户时区';
COMMENT ON COLUMN chat_export_jobs.csv_body IS '临时CSV结果，权限受RLS保护且到期清空；大规模部署改为私有对象存储';
COMMENT ON COLUMN chat_export_jobs.size_bytes IS '完成文件字节数';
COMMENT ON COLUMN chat_export_jobs.error_code IS '不含数据库细节的安全失败代码';
COMMENT ON COLUMN chat_export_jobs.error_message IS '供申请者查看的安全失败原因';
COMMENT ON COLUMN chat_export_jobs.lease_token IS '认领世代令牌，旧worker不能覆盖新世代结果';
COMMENT ON COLUMN chat_export_jobs.created_at IS '任务提交时间';
COMMENT ON COLUMN chat_export_jobs.started_at IS '最近认领开始时间';
COMMENT ON COLUMN chat_export_jobs.completed_at IS '最终结束时间';
COMMENT ON COLUMN chat_export_jobs.expires_at IS '结果和任务的下载授权截止时间，默认24小时';
CREATE INDEX chat_export_jobs_tenant_created ON chat_export_jobs(tenant_id,created_at DESC);
ALTER TABLE chat_export_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_export_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY chat_export_jobs_scope ON chat_export_jobs USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());

CREATE TABLE chat_export_dispatch (
 job_id uuid PRIMARY KEY REFERENCES chat_export_jobs(id),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 lease_until timestamptz,
 lease_token uuid,
 created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE chat_export_dispatch IS '服务内部导出调度索引，仅含任务和租户定位，不保存筛选条件、客户资料或CSV';
COMMENT ON COLUMN chat_export_dispatch.job_id IS '等待处理的导出任务';
COMMENT ON COLUMN chat_export_dispatch.tenant_id IS '任务租户定位，读取内容仍必须进入租户事务';
COMMENT ON COLUMN chat_export_dispatch.lease_until IS '调度租约到期时间';
COMMENT ON COLUMN chat_export_dispatch.lease_token IS '本次认领世代令牌';
COMMENT ON COLUMN chat_export_dispatch.created_at IS '入队顺序时间';
