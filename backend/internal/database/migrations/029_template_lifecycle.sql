-- 文件作用说明：为未绑定的私有模板提供受控原子删除，保持发布和代码包禁止原地改写，并清理无引用文件。
CREATE TABLE decoration_template_packages(
 tenant_id uuid NOT NULL,template_id uuid NOT NULL,package_id uuid NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,template_id,package_id),
 FOREIGN KEY(tenant_id,template_id) REFERENCES tenant_templates(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,package_id) REFERENCES decoration_code_packages(tenant_id,id)
);
COMMENT ON TABLE decoration_template_packages IS '记录模板历次修订拥有的代码包，删除最后一个使用者时可完整回收历史文件';
COMMENT ON COLUMN decoration_template_packages.tenant_id IS '所属租户编号';
COMMENT ON COLUMN decoration_template_packages.template_id IS '拥有该代码包的私有模板编号';
COMMENT ON COLUMN decoration_template_packages.package_id IS '本次或历史修订产生的不可变资源包编号';
COMMENT ON COLUMN decoration_template_packages.created_at IS '首次建立模板资源关联的时间';
ALTER TABLE decoration_template_packages ENABLE ROW LEVEL SECURITY;
ALTER TABLE decoration_template_packages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON decoration_template_packages USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
CREATE FUNCTION decoration_track_template_package() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
BEGIN
 IF NEW.package_id IS NOT NULL THEN
  INSERT INTO decoration_template_packages(tenant_id,template_id,package_id) VALUES(NEW.tenant_id,NEW.id,NEW.package_id) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
COMMENT ON FUNCTION decoration_track_template_package() IS '为每次模板保存保留代码包归属，支持以后完整清理该模板的无引用历史文件';
CREATE TRIGGER track_template_package AFTER INSERT OR UPDATE OF design ON tenant_templates FOR EACH ROW EXECUTE FUNCTION decoration_track_template_package();
-- 在所有者迁移上下文按租户建立既有模板的当前代码包归属。
DO $$ DECLARE t record; BEGIN
 FOR t IN SELECT id FROM tenants LOOP
  PERFORM set_config('app.tenant_id',t.id::text,true);
  INSERT INTO decoration_template_packages(tenant_id,template_id,package_id) SELECT tenant_id,id,package_id FROM tenant_templates WHERE tenant_id=t.id AND package_id IS NOT NULL ON CONFLICT DO NOTHING;
 END LOOP;
 PERFORM set_config('app.tenant_id','',true);
END $$;
CREATE FUNCTION decoration_cleanup_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND current_setting('goodchat.template_cleanup',true)='on'
  AND current_user=pg_get_userbyid((SELECT relowner FROM pg_class WHERE oid=TG_RELID)) THEN
  RETURN OLD;
 END IF;
 RAISE EXCEPTION '不可变装修快照不允许修改或删除' USING ERRCODE='23514';
END $$;
COMMENT ON FUNCTION decoration_cleanup_immutable() IS '仅允许表所有者在受控模板清理函数内删除无引用快照，应用自行设置会话参数无效';
DROP TRIGGER immutable_decoration_package ON decoration_code_packages;
DROP TRIGGER immutable_decoration_file ON decoration_package_files;
DROP TRIGGER immutable_decoration_release ON decoration_releases;
CREATE TRIGGER immutable_decoration_package BEFORE UPDATE OR DELETE ON decoration_code_packages FOR EACH ROW EXECUTE FUNCTION decoration_cleanup_immutable();
CREATE TRIGGER immutable_decoration_file BEFORE UPDATE OR DELETE ON decoration_package_files FOR EACH ROW EXECUTE FUNCTION decoration_cleanup_immutable();
CREATE TRIGGER immutable_decoration_release BEFORE UPDATE OR DELETE ON decoration_releases FOR EACH ROW EXECUTE FUNCTION decoration_cleanup_immutable();

CREATE FUNCTION decoration_delete_template(p_tenant uuid,p_template uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE
 target_name text; target_package uuid; page_ids uuid[]:='{}'; package_ids uuid[]:='{}';
 job_ids uuid[]:='{}'; item record; candidate_package uuid; file_count bigint:=0; package_count bigint:=0;
 retained_count bigint:=0; total_bytes bigint:=0; n bigint; b bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM public.current_tenant_id() THEN
  RAISE EXCEPTION '模板租户不匹配' USING ERRCODE='42501';
 END IF;
 SELECT name,t.package_id INTO target_name,target_package FROM public.tenant_templates t
  WHERE tenant_id=p_tenant AND id=p_template FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION '模板不存在' USING ERRCODE='P0002'; END IF;
 IF target_package IS NOT NULL THEN package_ids:=array_append(package_ids,target_package); END IF;
 FOR item IN SELECT p.package_id FROM public.decoration_template_packages p WHERE tenant_id=p_tenant AND template_id=p_template LOOP
  package_ids:=array_append(package_ids,item.package_id);
 END LOOP;
 -- 先锁定页面，绑定操作的外键锁将等待本事务完成，避免检查后又被绑定。
 FOR item IN SELECT id,p.package_id FROM public.decoration_pages p
  WHERE tenant_id=p_tenant AND template_id=p_template ORDER BY id FOR UPDATE LOOP
  page_ids:=array_append(page_ids,item.id);
  IF item.package_id IS NOT NULL THEN package_ids:=array_append(package_ids,item.package_id); END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM public.access_links WHERE tenant_id=p_tenant AND page_id=ANY(page_ids)) THEN
  RAISE EXCEPTION '模板仍有链接绑定，请先更换模板或解除绑定' USING ERRCODE='23503';
 END IF;
 IF EXISTS(SELECT 1 FROM public.ai_design_jobs WHERE tenant_id=p_tenant AND template_id=p_template AND status IN('queued','running','validating'))
  OR EXISTS(SELECT 1 FROM public.assistant_jobs j JOIN public.assistant_threads t ON t.tenant_id=j.tenant_id AND t.id=j.thread_id
   WHERE j.tenant_id=p_tenant AND j.status IN('queued','running','validating')
   AND (j.base_template_id=p_template OR t.template_id=p_template OR t.page_id=ANY(page_ids))) THEN
  RAISE EXCEPTION '模板仍有进行中的AI任务，请先取消任务' USING ERRCODE='23503';
 END IF;
 SELECT coalesce(array_agg(DISTINCT j.id),'{}') INTO job_ids FROM public.assistant_jobs j
  JOIN public.assistant_threads t ON t.tenant_id=j.tenant_id AND t.id=j.thread_id WHERE j.tenant_id=p_tenant
  AND (j.base_template_id=p_template OR t.template_id=p_template OR t.page_id=ANY(page_ids));
 FOR item IN SELECT r.package_id FROM public.decoration_releases r WHERE tenant_id=p_tenant AND page_id=ANY(page_ids)
  UNION SELECT p.package_id FROM public.decoration_previews p WHERE tenant_id=p_tenant AND page_id=ANY(page_ids)
  UNION SELECT nullif(j.design->>'package_id','')::uuid FROM public.assistant_jobs j WHERE tenant_id=p_tenant AND id=ANY(job_ids) LOOP
  IF item.package_id IS NOT NULL THEN package_ids:=array_append(package_ids,item.package_id); END IF;
 END LOOP;
 PERFORM set_config('goodchat.template_cleanup','on',true);
 DELETE FROM public.assistant_image_tasks WHERE tenant_id=p_tenant AND job_id=ANY(job_ids);
 UPDATE public.assistant_jobs SET design=NULL,source_result=NULL WHERE tenant_id=p_tenant AND id=ANY(job_ids);
 UPDATE public.assistant_threads SET template_id=NULL,page_id=NULL WHERE tenant_id=p_tenant AND (template_id=p_template OR page_id=ANY(page_ids));
 DELETE FROM public.ai_design_queue WHERE tenant_id=p_tenant AND job_id IN(SELECT id FROM public.ai_design_jobs WHERE tenant_id=p_tenant AND template_id=p_template);
 DELETE FROM public.ai_design_jobs WHERE tenant_id=p_tenant AND template_id=p_template;
 UPDATE public.decoration_pages SET current_release_id=NULL WHERE tenant_id=p_tenant AND id=ANY(page_ids);
 DELETE FROM public.decoration_actions WHERE tenant_id=p_tenant AND page_id=ANY(page_ids);
 DELETE FROM public.decoration_previews WHERE tenant_id=p_tenant AND page_id=ANY(page_ids);
 DELETE FROM public.decoration_releases WHERE tenant_id=p_tenant AND page_id=ANY(page_ids);
 DELETE FROM public.decoration_pages WHERE tenant_id=p_tenant AND id=ANY(page_ids);
 DELETE FROM public.tenant_templates WHERE tenant_id=p_tenant AND id=p_template;
 FOR candidate_package IN SELECT DISTINCT unnest(package_ids) LOOP
  -- 锁定包后重新检查全部引用，共用图片所在的包仍有使用者时继续保留。
  PERFORM 1 FROM public.decoration_code_packages WHERE tenant_id=p_tenant AND id=candidate_package FOR UPDATE;
  IF EXISTS(SELECT 1 FROM public.tenant_templates t WHERE t.tenant_id=p_tenant AND t.package_id=candidate_package)
   OR EXISTS(SELECT 1 FROM public.decoration_template_packages t WHERE t.tenant_id=p_tenant AND t.package_id=candidate_package)
   OR EXISTS(SELECT 1 FROM public.decoration_pages p WHERE p.tenant_id=p_tenant AND p.package_id=candidate_package)
   OR EXISTS(SELECT 1 FROM public.decoration_releases r WHERE r.tenant_id=p_tenant AND r.package_id=candidate_package)
   OR EXISTS(SELECT 1 FROM public.decoration_previews p WHERE p.tenant_id=p_tenant AND p.package_id=candidate_package)
   OR EXISTS(SELECT 1 FROM public.assistant_jobs j WHERE j.tenant_id=p_tenant AND j.design->>'package_id'=candidate_package::text) THEN
   retained_count:=retained_count+1;
  ELSE
   SELECT count(*),coalesce(sum(octet_length(content)),0) INTO n,b FROM public.decoration_package_files f WHERE f.tenant_id=p_tenant AND f.package_id=candidate_package;
   DELETE FROM public.decoration_package_files f WHERE f.tenant_id=p_tenant AND f.package_id=candidate_package;
   DELETE FROM public.decoration_code_packages p WHERE p.tenant_id=p_tenant AND p.id=candidate_package;
   file_count:=file_count+n;total_bytes:=total_bytes+b;package_count:=package_count+1;
  END IF;
 END LOOP;
 RETURN jsonb_build_object('id',p_template,'name',target_name,'deleted',true,'deleted_pages',cardinality(page_ids),
  'deleted_packages',package_count,'deleted_files',file_count,'deleted_bytes',total_bytes,'retained_packages',retained_count);
END $$;
COMMENT ON FUNCTION decoration_delete_template(uuid,uuid) IS '锁定当前租户模板及页面，拒绝仍绑定链接或活动任务的删除，原子清理自有版本和无引用资源字节';
REVOKE ALL ON FUNCTION decoration_delete_template(uuid,uuid) FROM PUBLIC;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN
  GRANT SELECT,INSERT ON decoration_template_packages TO goodchat_saas_app;
  GRANT EXECUTE ON FUNCTION decoration_delete_template(uuid,uuid) TO goodchat_saas_app;
 END IF;
END $$;
