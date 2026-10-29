-- 文件作用说明：允许模板直接创建十五分钟隔离预览，临时静态文件随预览清理，不创建永久页面或资源包。
ALTER TABLE decoration_previews ALTER COLUMN page_id DROP NOT NULL;
ALTER TABLE decoration_previews ADD COLUMN ephemeral_entrypoint text;
ALTER TABLE decoration_previews ADD COLUMN ephemeral_manifest jsonb;
ALTER TABLE decoration_previews ADD CONSTRAINT preview_ephemeral_package CHECK(
 (ephemeral_entrypoint IS NULL AND ephemeral_manifest IS NULL)
 OR (ephemeral_entrypoint IS NOT NULL AND ephemeral_manifest IS NOT NULL AND jsonb_typeof(ephemeral_manifest)='array' AND package_id IS NULL)
);
COMMENT ON COLUMN decoration_previews.page_id IS '可选来源页面编号，模板直接预览不创建页面';
COMMENT ON COLUMN decoration_previews.ephemeral_entrypoint IS '仅在当前短期预览内有效的静态入口相对路径';
COMMENT ON COLUMN decoration_previews.ephemeral_manifest IS '当前预览临时静态文件的白名单及摘要，不含凭据';

CREATE TABLE decoration_preview_files (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 preview_id uuid NOT NULL,
 path text NOT NULL,
 content_type text NOT NULL,
 content bytea NOT NULL,
 sha256 text NOT NULL,
 PRIMARY KEY(preview_id,path),
 FOREIGN KEY(tenant_id,preview_id) REFERENCES decoration_previews(tenant_id,id) ON DELETE CASCADE
);
COMMENT ON TABLE decoration_preview_files IS '仅供短期隔离预览使用的静态字节，删除过期预览时级联回收';
COMMENT ON COLUMN decoration_preview_files.tenant_id IS '当前发起预览的租户编号';
COMMENT ON COLUMN decoration_preview_files.preview_id IS '持短期令牌访问的预览编号';
COMMENT ON COLUMN decoration_preview_files.path IS '经过校验的规范静态相对路径';
COMMENT ON COLUMN decoration_preview_files.content_type IS '经过校验的实际静态文件类型';
COMMENT ON COLUMN decoration_preview_files.content IS '预览期内冻结的静态文件字节';
COMMENT ON COLUMN decoration_preview_files.sha256 IS '静态文件原始字节的SHA256摘要';
ALTER TABLE decoration_preview_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE decoration_preview_files FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON decoration_preview_files USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());

CREATE OR REPLACE FUNCTION decoration_render(p_id uuid,p_token_hash text) RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 WITH snapshots AS (
  SELECT id,tenant_id,render_mode,design,package_id,NULL::text AS ephemeral_entrypoint,NULL::jsonb AS ephemeral_manifest
   FROM public.decoration_releases WHERE id=p_id
  UNION ALL
  SELECT id,tenant_id,render_mode,design,package_id,ephemeral_entrypoint,ephemeral_manifest
   FROM public.decoration_previews WHERE id=p_id AND token_hash=p_token_hash AND expires_at>now()
 )
 SELECT jsonb_build_object('render_mode',s.render_mode,'design',s.design,'release_id',s.id,
  'package_entrypoint',COALESCE(s.ephemeral_entrypoint,p.entrypoint),
  'package_manifest',COALESCE(s.ephemeral_manifest,p.manifest),
  'ephemeral_package',s.ephemeral_entrypoint IS NOT NULL)
 FROM snapshots s LEFT JOIN public.decoration_code_packages p ON p.tenant_id=s.tenant_id AND p.id=s.package_id LIMIT 1
$$;
COMMENT ON FUNCTION decoration_render(uuid,text) IS '仅返回公开发布或有效令牌预览的固定设计，支持自动过期的模板静态快照';

CREATE OR REPLACE FUNCTION decoration_package_asset(p_id uuid,p_token_hash text,p_path text) RETURNS TABLE(content_type text,content bytea,sha256 text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 WITH snapshots AS (
  SELECT tenant_id,package_id,NULL::uuid AS preview_id FROM public.decoration_releases WHERE id=p_id
  UNION ALL
  SELECT tenant_id,package_id,id AS preview_id FROM public.decoration_previews
   WHERE id=p_id AND token_hash=p_token_hash AND expires_at>now()
 )
 SELECT f.content_type,f.content,f.sha256 FROM snapshots s
  JOIN public.decoration_package_files f ON f.tenant_id=s.tenant_id AND f.package_id=s.package_id WHERE f.path=p_path
 UNION ALL
 SELECT f.content_type,f.content,f.sha256 FROM snapshots s
  JOIN public.decoration_preview_files f ON f.tenant_id=s.tenant_id AND f.preview_id=s.preview_id WHERE f.path=p_path
 LIMIT 1
$$;
COMMENT ON FUNCTION decoration_package_asset(uuid,text,text) IS '仅按版本与有效预览凭证读取该快照白名单静态文件，临时文件不能通过发布路径读取';
REVOKE ALL ON FUNCTION decoration_render(uuid,text),decoration_package_asset(uuid,text,text) FROM PUBLIC;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN
  GRANT SELECT,INSERT,DELETE ON decoration_preview_files TO goodchat_saas_app;
  GRANT EXECUTE ON FUNCTION decoration_render(uuid,text),decoration_package_asset(uuid,text,text) TO goodchat_saas_app;
 END IF;
END $$;
