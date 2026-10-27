-- 文件作用说明：为异步正文导出保存撤回数量快照，后续下载拒绝已提交撤回前生成的缓存正文，不改变元数据导出。
ALTER TABLE chat_export_jobs ADD COLUMN withdrawn_count bigint NOT NULL DEFAULT 0 CHECK(withdrawn_count>=0);
COMMENT ON COLUMN chat_export_jobs.withdrawn_count IS 'CSV查询前读取的租户已撤回消息数，下载时变化则清理缓存；引入消息物理清理前须升级为单调撤回版本';
CREATE INDEX messages_tenant_withdrawn ON messages(tenant_id) WHERE withdrawn_at IS NOT NULL;
COMMENT ON INDEX messages_tenant_withdrawn IS '加速异步正文导出下载前的租户撤回快照核对';
