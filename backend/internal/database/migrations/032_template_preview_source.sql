-- 文件作用说明：为私有模板直接预览记录租户内来源，删除模板时先级联撤销预览，再由原有生命周期回收无引用私包。
-- 仅清除新直接预览入口的可重建短期缓存，保留原有页面预览及全部业务模板、页面、发布和资源包。
DELETE FROM decoration_previews WHERE page_id IS NULL;
ALTER TABLE decoration_previews ADD COLUMN source_template_id uuid;
COMMENT ON COLUMN decoration_previews.source_template_id IS '直接预览的本租户来源模板编号，删除模板时级联撤销预览；公共及行业预览为空';
ALTER TABLE decoration_previews ADD CONSTRAINT preview_source_template_scope
 FOREIGN KEY(tenant_id,source_template_id) REFERENCES tenant_templates(tenant_id,id) ON DELETE CASCADE;
ALTER TABLE decoration_previews ADD CONSTRAINT preview_direct_private_source
 CHECK(page_id IS NOT NULL OR package_id IS NULL OR source_template_id IS NOT NULL);
CREATE INDEX decoration_preview_source_template ON decoration_previews(tenant_id,source_template_id) WHERE source_template_id IS NOT NULL;
