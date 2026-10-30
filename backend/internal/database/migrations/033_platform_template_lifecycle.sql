-- 文件作用说明：为旧平台目录提供可审计归档与无引用字节清理，保留租户已复制作品的来源历史和线上发布。
ALTER TABLE platform_templates ADD COLUMN archived_at timestamptz;
COMMENT ON COLUMN platform_templates.archived_at IS '平台目录移除时间，非空后禁止新复制、预览、二创及设计修订，私有来源历史继续保留';
ALTER TABLE platform_templates ADD CONSTRAINT platform_template_archive_hidden CHECK(archived_at IS NULL OR status='hidden');

-- 只允许表所有者在下述固定聚合函数中检查来源数量；应用自行设置标志不能越过租户隔离。
DO $$ DECLARE relation_name text; BEGIN
 FOREACH relation_name IN ARRAY ARRAY['tenant_templates','decoration_pages','access_links'] LOOP
  EXECUTE format('CREATE POLICY platform_template_owner_usage ON public.%I FOR SELECT USING(current_setting(''goodchat.platform_template_usage'',true)=''on'' AND current_user=pg_get_userbyid((SELECT relowner FROM pg_class WHERE oid=%L::regclass)))',relation_name,'public.'||relation_name);
  EXECUTE format('COMMENT ON POLICY platform_template_owner_usage ON public.%I IS %L',relation_name,'仅受控平台来源统计函数的表所有者可跨租户读取数量，应用角色设置标志无效');
 END LOOP;
END $$;

-- 只有受控清理函数的表所有者可以删除平台快照，应用直接修改会话标志仍不获删除权限。
DROP TRIGGER immutable_platform_version ON platform_template_versions;
DROP TRIGGER immutable_platform_template_package ON platform_template_packages;
DROP TRIGGER immutable_platform_template_file ON platform_template_package_files;
CREATE TRIGGER immutable_platform_version BEFORE UPDATE OR DELETE ON platform_template_versions FOR EACH ROW EXECUTE FUNCTION decoration_cleanup_immutable();
CREATE TRIGGER immutable_platform_template_package BEFORE UPDATE OR DELETE ON platform_template_packages FOR EACH ROW EXECUTE FUNCTION decoration_cleanup_immutable();
CREATE TRIGGER immutable_platform_template_file BEFORE UPDATE OR DELETE ON platform_template_package_files FOR EACH ROW EXECUTE FUNCTION decoration_cleanup_immutable();

CREATE FUNCTION platform_template_usage(p_template uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE target_version bigint; copies bigint; links bigint; pages bigint; previous_scope text;
BEGIN
 SELECT version INTO target_version FROM public.platform_templates WHERE id=p_template AND archived_at IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION '平台模板不存在' USING ERRCODE='P0002'; END IF;
 previous_scope:=coalesce(current_setting('goodchat.platform_template_usage',true),'');
 PERFORM set_config('goodchat.platform_template_usage','on',true);
 SELECT count(*) INTO copies FROM public.tenant_templates WHERE source_platform_template_id=p_template;
 SELECT count(*) INTO pages FROM public.decoration_pages p JOIN public.tenant_templates t ON t.id=p.template_id AND t.tenant_id=p.tenant_id WHERE t.source_platform_template_id=p_template;
 SELECT count(*) INTO links FROM public.access_links l JOIN public.decoration_pages p ON p.id=l.page_id AND p.tenant_id=l.tenant_id JOIN public.tenant_templates t ON t.id=p.template_id AND t.tenant_id=p.tenant_id WHERE t.source_platform_template_id=p_template;
 PERFORM set_config('goodchat.platform_template_usage',previous_scope,true);
 RETURN jsonb_build_object('id',p_template,'version',target_version,'source_copies',copies,'pages',pages,'bound_links',links,'can_delete',true,'will_archive',copies>0);
END $$;
COMMENT ON FUNCTION platform_template_usage(uuid) IS '仅向平台管理接口返回来源副本和关联入口数量，不暴露其他租户身份、源码或访客资料';
REVOKE ALL ON FUNCTION platform_template_usage(uuid) FROM PUBLIC;

CREATE FUNCTION platform_delete_template(p_template uuid,p_version bigint) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE target_name text; target_version bigint; removed_at timestamptz; copies bigint; previous_scope text;
 package_ids uuid[]:='{}'; candidate uuid; file_count bigint:=0; package_count bigint:=0;
 retained_count bigint:=0; total_bytes bigint:=0; n bigint; b bigint;
BEGIN
 -- 当前行锁与复制入口的共享锁互斥，杜绝检查后又创建来源副本。
 SELECT name,version,archived_at INTO target_name,target_version,removed_at FROM public.platform_templates WHERE id=p_template FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION '平台模板不存在' USING ERRCODE='P0002'; END IF;
 IF p_version IS DISTINCT FROM target_version THEN RAISE EXCEPTION '平台模板版本冲突' USING ERRCODE='40001'; END IF;
 SELECT coalesce(array_agg(DISTINCT platform_package_id ORDER BY platform_package_id) FILTER(WHERE platform_package_id IS NOT NULL),'{}') INTO package_ids FROM public.platform_template_versions WHERE template_id=p_template;
 previous_scope:=coalesce(current_setting('goodchat.platform_template_usage',true),'');
 PERFORM set_config('goodchat.platform_template_usage','on',true);
 SELECT count(*) INTO copies FROM public.tenant_templates WHERE source_platform_template_id=p_template;
 PERFORM set_config('goodchat.platform_template_usage',previous_scope,true);
 IF copies>0 THEN
  -- 已使用副本字节独立，但来源外键仍需完整的不可变历史；只从目录移除。
  IF removed_at IS NULL THEN
   UPDATE public.platform_templates SET archived_at=now(),status='hidden',industry_key=NULL,version=version+1,updated_at=now() WHERE id=p_template;
  END IF;
  RETURN jsonb_build_object('id',p_template,'name',target_name,'deleted',true,'archived',true,'source_copies',copies,'deleted_packages',0,'deleted_files',0,'deleted_bytes',0,'retained_packages',cardinality(package_ids));
 END IF;
 PERFORM set_config('goodchat.template_cleanup','on',true);
 DELETE FROM public.platform_template_versions WHERE template_id=p_template;
 DELETE FROM public.platform_templates WHERE id=p_template;
 FOREACH candidate IN ARRAY package_ids LOOP
  PERFORM 1 FROM public.platform_template_packages WHERE id=candidate FOR UPDATE;
  IF EXISTS(SELECT 1 FROM public.platform_templates WHERE platform_package_id=candidate)
   OR EXISTS(SELECT 1 FROM public.platform_template_versions WHERE platform_package_id=candidate) THEN
   retained_count:=retained_count+1;
  ELSE
   SELECT count(*),coalesce(sum(octet_length(content)),0) INTO n,b FROM public.platform_template_package_files WHERE package_id=candidate;
   DELETE FROM public.platform_template_package_files WHERE package_id=candidate;
   DELETE FROM public.platform_template_packages WHERE id=candidate;
   file_count:=file_count+n;total_bytes:=total_bytes+b;package_count:=package_count+1;
  END IF;
 END LOOP;
 RETURN jsonb_build_object('id',p_template,'name',target_name,'deleted',true,'archived',false,'source_copies',copies,'deleted_packages',package_count,'deleted_files',file_count,'deleted_bytes',total_bytes,'retained_packages',retained_count);
END $$;
COMMENT ON FUNCTION platform_delete_template(uuid,bigint) IS '按版本移除平台模板，已有来源副本则归档，无来源时原子回收无其他公开版本引用的历史和文件';
REVOKE ALL ON FUNCTION platform_delete_template(uuid,bigint) FROM PUBLIC;

CREATE FUNCTION platform_collect_archived_template_source() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE source_version bigint;
BEGIN
 IF OLD.source_platform_template_id IS NULL THEN RETURN NULL; END IF;
 SELECT version INTO source_version FROM public.platform_templates WHERE id=OLD.source_platform_template_id AND archived_at IS NOT NULL FOR UPDATE;
 IF FOUND THEN
  -- 最后一个来源副本删除后重走同一引用检查，公共目录不再留下永久孤立文件。
  PERFORM public.platform_delete_template(OLD.source_platform_template_id,source_version);
 END IF;
 RETURN NULL;
END $$;
COMMENT ON FUNCTION platform_collect_archived_template_source() IS '删除私有来源副本后仅检查已移出平台目录的来源，最后引用消失时受控回收其公共历史与孤立文件';
REVOKE ALL ON FUNCTION platform_collect_archived_template_source() FROM PUBLIC;
CREATE TRIGGER collect_archived_platform_template_source AFTER DELETE ON tenant_templates FOR EACH ROW EXECUTE FUNCTION platform_collect_archived_template_source();
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN
  GRANT EXECUTE ON FUNCTION platform_template_usage(uuid),platform_delete_template(uuid,bigint) TO goodchat_saas_app;
 END IF;
END $$;
