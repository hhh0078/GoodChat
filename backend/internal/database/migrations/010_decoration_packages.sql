-- 文件作用说明：增加租户私有且不可变的静态装修资源包，发布与短期预览只能读取自身绑定的静态文件。
CREATE TABLE decoration_code_packages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 owner_membership_id uuid NOT NULL, name text NOT NULL, entrypoint text NOT NULL,
 manifest jsonb NOT NULL, file_count integer NOT NULL CHECK(file_count BETWEEN 1 AND 200),
 total_bytes bigint NOT NULL CHECK(total_bytes BETWEEN 1 AND 20971520), sha256 text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,owner_membership_id) REFERENCES tenant_memberships(tenant_id,id)
);
CREATE TABLE decoration_package_files (
 tenant_id uuid NOT NULL, package_id uuid NOT NULL, path text NOT NULL, content_type text NOT NULL,
 sha256 text NOT NULL, content bytea NOT NULL CHECK(octet_length(content)<=5242880),
 PRIMARY KEY(tenant_id,package_id,path),
 FOREIGN KEY(tenant_id,package_id) REFERENCES decoration_code_packages(tenant_id,id)
);
COMMENT ON TABLE decoration_code_packages IS '租户私有静态代码资源包的不可变清单；修改时创建新资源包';
COMMENT ON COLUMN decoration_code_packages.id IS '资源包随机编号';
COMMENT ON COLUMN decoration_code_packages.tenant_id IS '归属租户编号';
COMMENT ON COLUMN decoration_code_packages.owner_membership_id IS '导入资源包的租户成员编号';
COMMENT ON COLUMN decoration_code_packages.name IS '资源包显示名称';
COMMENT ON COLUMN decoration_code_packages.entrypoint IS '经过路径与HTML校验的入口文件';
COMMENT ON COLUMN decoration_code_packages.manifest IS '固定的文件路径、实际类型、字节数与摘要清单';
COMMENT ON COLUMN decoration_code_packages.file_count IS '资源文件总数，上限二百';
COMMENT ON COLUMN decoration_code_packages.total_bytes IS '规范化后资源总字节数，上限二十MiB';
COMMENT ON COLUMN decoration_code_packages.sha256 IS '按路径排序后的完整资源清单摘要';
COMMENT ON COLUMN decoration_code_packages.created_at IS '资源包创建时间';
COMMENT ON TABLE decoration_package_files IS '不可变资源包的私有静态文件；不存放访客消息、附件凭证或服务端代码';
COMMENT ON COLUMN decoration_package_files.tenant_id IS '归属租户编号';
COMMENT ON COLUMN decoration_package_files.package_id IS '所属不可变资源包编号';
COMMENT ON COLUMN decoration_package_files.path IS '经过规范化并拒绝目录穿越的相对路径';
COMMENT ON COLUMN decoration_package_files.content_type IS '依据实际文件解码和结构校验确定的媒体类型';
COMMENT ON COLUMN decoration_package_files.sha256 IS '文件内容SHA256摘要';
COMMENT ON COLUMN decoration_package_files.content IS '经过大小和类型校验的静态文件字节';
ALTER TABLE decoration_code_packages ENABLE ROW LEVEL SECURITY;
ALTER TABLE decoration_code_packages FORCE ROW LEVEL SECURITY;
ALTER TABLE decoration_package_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE decoration_package_files FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON decoration_code_packages USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
CREATE POLICY tenant_isolation ON decoration_package_files USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
CREATE TRIGGER immutable_decoration_package BEFORE UPDATE OR DELETE ON decoration_code_packages FOR EACH ROW EXECUTE FUNCTION decoration_immutable();
CREATE TRIGGER immutable_decoration_file BEFORE UPDATE OR DELETE ON decoration_package_files FOR EACH ROW EXECUTE FUNCTION decoration_immutable();
-- 生成列令所有现有写入路径都受到租户复合外键保护，不能凭JSON引用其他租户的私有资源包。
ALTER TABLE tenant_templates ADD COLUMN package_id uuid GENERATED ALWAYS AS (NULLIF(design->>'package_id','')::uuid) STORED;
ALTER TABLE decoration_pages ADD COLUMN package_id uuid GENERATED ALWAYS AS (NULLIF(draft->>'package_id','')::uuid) STORED;
ALTER TABLE decoration_releases ADD COLUMN package_id uuid GENERATED ALWAYS AS (NULLIF(design->>'package_id','')::uuid) STORED;
ALTER TABLE decoration_previews ADD COLUMN package_id uuid GENERATED ALWAYS AS (NULLIF(design->>'package_id','')::uuid) STORED;
ALTER TABLE tenant_templates ADD CONSTRAINT template_package_scope FOREIGN KEY(tenant_id,package_id) REFERENCES decoration_code_packages(tenant_id,id);
ALTER TABLE decoration_pages ADD CONSTRAINT page_package_scope FOREIGN KEY(tenant_id,package_id) REFERENCES decoration_code_packages(tenant_id,id);
ALTER TABLE decoration_releases ADD CONSTRAINT release_package_scope FOREIGN KEY(tenant_id,package_id) REFERENCES decoration_code_packages(tenant_id,id);
ALTER TABLE decoration_previews ADD CONSTRAINT preview_package_scope FOREIGN KEY(tenant_id,package_id) REFERENCES decoration_code_packages(tenant_id,id);
COMMENT ON COLUMN tenant_templates.package_id IS '从设计生成的本租户不可变代码资源包引用';
COMMENT ON COLUMN decoration_pages.package_id IS '从当前草稿生成的本租户不可变代码资源包引用';
COMMENT ON COLUMN decoration_releases.package_id IS '发布时固定的本租户不可变代码资源包引用';
COMMENT ON COLUMN decoration_previews.package_id IS '短期预览时固定的本租户不可变代码资源包引用';
ALTER TABLE platform_templates ADD CONSTRAINT public_template_no_private_package CHECK(COALESCE(design->>'package_id','')='');
ALTER TABLE platform_template_versions ADD CONSTRAINT public_version_no_private_package CHECK(COALESCE(design->>'package_id','')='');
CREATE OR REPLACE FUNCTION decoration_render(p_id uuid,p_token_hash text) RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT jsonb_build_object('render_mode',s.render_mode,'design',s.design,'release_id',s.id,'package_entrypoint',p.entrypoint,'package_manifest',p.manifest)
 FROM (SELECT id,tenant_id,render_mode,design,package_id FROM public.decoration_releases WHERE id=p_id
 UNION ALL SELECT id,tenant_id,render_mode,design,package_id FROM public.decoration_previews WHERE id=p_id AND token_hash=p_token_hash AND expires_at>now()) s
 LEFT JOIN public.decoration_code_packages p ON p.tenant_id=s.tenant_id AND p.id=s.package_id LIMIT 1
$$;
COMMENT ON FUNCTION decoration_render(uuid,text) IS '只返回已发布或有效短凭证预览的静态设计与固定入口，不返回租户会话或管理凭证';
CREATE FUNCTION decoration_package_asset(p_id uuid,p_token_hash text,p_path text) RETURNS TABLE(content_type text,content bytea,sha256 text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT f.content_type,f.content,f.sha256 FROM
 (SELECT tenant_id,package_id FROM public.decoration_releases WHERE id=p_id
 UNION ALL SELECT tenant_id,package_id FROM public.decoration_previews WHERE id=p_id AND token_hash=p_token_hash AND expires_at>now()) s
 JOIN public.decoration_package_files f ON f.tenant_id=s.tenant_id AND f.package_id=s.package_id WHERE f.path=p_path LIMIT 1
$$;
COMMENT ON FUNCTION decoration_package_asset(uuid,text,text) IS '只按固定发布或有效预览编号读取该版本绑定的静态文件，不接受任意资源包或客户数据编号';
REVOKE ALL ON FUNCTION decoration_package_asset(uuid,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION decoration_render(uuid,text) FROM PUBLIC;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN
  GRANT SELECT,INSERT ON decoration_code_packages,decoration_package_files TO goodchat_saas_app;
  REVOKE UPDATE,DELETE ON decoration_code_packages,decoration_package_files FROM goodchat_saas_app;
  GRANT EXECUTE ON FUNCTION decoration_render(uuid,text),decoration_package_asset(uuid,text,text) TO goodchat_saas_app;
 END IF;
END $$;
