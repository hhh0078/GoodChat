-- 文件作用说明：为十五分钟隔离编辑快照记录一次性选择器握手标识；公开发布永远不携带编辑通道。
ALTER TABLE decoration_previews ADD COLUMN editor_nonce text;
COMMENT ON COLUMN decoration_previews.editor_nonce IS '仅编辑器短期快照使用的UUID握手标识，不包含租户凭证';
ALTER TABLE decoration_previews ADD CONSTRAINT preview_editor_nonce_scope CHECK(
 editor_nonce IS NULL OR (ephemeral_entrypoint IS NOT NULL AND editor_nonce ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$')
);

CREATE OR REPLACE FUNCTION decoration_render(p_id uuid,p_token_hash text) RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 WITH snapshots AS (
  SELECT id,tenant_id,render_mode,design,package_id,NULL::text AS ephemeral_entrypoint,NULL::jsonb AS ephemeral_manifest,NULL::text AS editor_nonce
   FROM public.decoration_releases WHERE id=p_id
  UNION ALL
  SELECT id,tenant_id,render_mode,design,package_id,ephemeral_entrypoint,ephemeral_manifest,editor_nonce
   FROM public.decoration_previews WHERE id=p_id AND token_hash=p_token_hash AND expires_at>now()
 )
 SELECT jsonb_build_object('render_mode',s.render_mode,'design',s.design,'release_id',s.id,
  'package_entrypoint',COALESCE(s.ephemeral_entrypoint,p.entrypoint),
  'package_manifest',COALESCE(s.ephemeral_manifest,p.manifest),
  'ephemeral_package',s.ephemeral_entrypoint IS NOT NULL,'editor_nonce',s.editor_nonce)
 FROM snapshots s LEFT JOIN public.decoration_code_packages p ON p.tenant_id=s.tenant_id AND p.id=s.package_id LIMIT 1
$$;
COMMENT ON FUNCTION decoration_render(uuid,text) IS '只返回公开发布或有效令牌预览，元素选择标识仅属于临时编辑快照';
REVOKE ALL ON FUNCTION decoration_render(uuid,text) FROM PUBLIC;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN
  GRANT EXECUTE ON FUNCTION decoration_render(uuid,text) TO goodchat_saas_app;
  GRANT UPDATE(editor_nonce) ON decoration_previews TO goodchat_saas_app;
 END IF;
END $$;
