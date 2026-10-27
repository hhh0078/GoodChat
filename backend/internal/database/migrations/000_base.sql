-- 文件作用说明：建立租户范围读取函数，供业务表行级安全策略复用，不创建任何默认业务账号。
CREATE OR REPLACE FUNCTION current_tenant_id() RETURNS uuid
LANGUAGE sql STABLE
AS $$ SELECT nullif(current_setting('app.tenant_id', true), '')::uuid $$;
COMMENT ON FUNCTION current_tenant_id() IS '读取当前事务经过认证设置的租户编号，未设置时为空';
