-- 文件作用说明：为既有知识文档增加租户隔离的文件夹树，旧文档保留在根目录，不改动正文与历史版本。
CREATE TABLE ai_knowledge_folders (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 parent_id uuid, name text NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 100),
 version bigint NOT NULL DEFAULT 1, archived boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,parent_id) REFERENCES ai_knowledge_folders(tenant_id,id), CHECK(parent_id IS NULL OR parent_id<>id)
);
ALTER TABLE ai_knowledge_bases ADD COLUMN folder_id uuid;
ALTER TABLE ai_knowledge_bases ADD CONSTRAINT ai_knowledge_folder_tenant FOREIGN KEY(tenant_id,folder_id) REFERENCES ai_knowledge_folders(tenant_id,id);
CREATE INDEX ai_knowledge_folders_parent ON ai_knowledge_folders(tenant_id,parent_id) WHERE NOT archived;
CREATE INDEX ai_knowledge_bases_folder ON ai_knowledge_bases(tenant_id,folder_id) WHERE NOT archived;
COMMENT ON TABLE ai_knowledge_folders IS '租户共享知识目录树，管理员维护，正文单独按机器人授权读取';
COMMENT ON COLUMN ai_knowledge_folders.id IS '目录编号';
COMMENT ON COLUMN ai_knowledge_folders.tenant_id IS '所属租户';
COMMENT ON COLUMN ai_knowledge_folders.parent_id IS '同租户父目录，空值表示根目录';
COMMENT ON COLUMN ai_knowledge_folders.name IS '目录名称';
COMMENT ON COLUMN ai_knowledge_folders.version IS '目录编辑乐观版本';
COMMENT ON COLUMN ai_knowledge_folders.archived IS '空目录移除标记';
COMMENT ON COLUMN ai_knowledge_folders.created_at IS '创建时间';
COMMENT ON COLUMN ai_knowledge_folders.updated_at IS '修改时间';
COMMENT ON COLUMN ai_knowledge_bases.folder_id IS '同租户目录编号，旧文档默认位于根目录';
ALTER TABLE ai_knowledge_folders ENABLE ROW LEVEL SECURITY; ALTER TABLE ai_knowledge_folders FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_isolation ON ai_knowledge_folders USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
