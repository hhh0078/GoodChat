-- 文件作用说明：限制一个账号只有一个有效工作台、一个租户只有一个默认服务空间；遇到历史重复时安全失败，不自动删除或迁移数据。
CREATE UNIQUE INDEX memberships_one_workspace ON tenant_memberships(user_id) WHERE removed_at IS NULL AND NOT disabled;
COMMENT ON INDEX memberships_one_workspace IS '账号只能有一个未停用且未移除的工作台身份，保留停用和移除的历史成员关系';

ALTER TABLE service_spaces ADD CONSTRAINT service_spaces_one_per_tenant UNIQUE(tenant_id);
COMMENT ON CONSTRAINT service_spaces_one_per_tenant ON service_spaces IS '每个租户只保留唯一服务空间，业务来源由接入链接区分';

ALTER TABLE service_spaces ADD CONSTRAINT service_spaces_default_only CHECK(is_default);
COMMENT ON CONSTRAINT service_spaces_default_only ON service_spaces IS '唯一服务空间必须是默认空间，不再建立可切换空间';
