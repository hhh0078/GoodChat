-- 文件作用说明：为租户本地自然日AI提交次数统计增加覆盖所有任务状态的索引，不修改任务或额度历史。
CREATE INDEX ai_design_tenant_daily ON ai_design_jobs(tenant_id,created_at);
COMMENT ON INDEX ai_design_tenant_daily IS '按租户及提交时间统计AI自然日次数，包含失败和取消任务以防绕过日限制';
