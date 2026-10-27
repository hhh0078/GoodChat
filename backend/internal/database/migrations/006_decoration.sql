-- 文件作用说明：建立装修私有草稿、不可变发布、隔离预览和持久AI任务；仅新增本项目业务对象。
CREATE TABLE platform_templates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL,
 render_mode text NOT NULL CHECK(render_mode IN('schema','code')), design jsonb NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), status text NOT NULL DEFAULT 'published' CHECK(status IN('published','hidden')),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE platform_template_versions (
 template_id uuid NOT NULL REFERENCES platform_templates(id), version bigint NOT NULL,
 name text NOT NULL, render_mode text NOT NULL CHECK(render_mode IN('schema','code')), design jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(template_id,version)
);
CREATE TABLE tenant_templates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 owner_membership_id uuid NOT NULL, name text NOT NULL, render_mode text NOT NULL CHECK(render_mode IN('schema','code')),
 design jsonb NOT NULL, version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 status text NOT NULL DEFAULT 'ready' CHECK(status IN('generating','draft','ready')),
 source_platform_template_id uuid, source_platform_version bigint, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,owner_membership_id) REFERENCES tenant_memberships(tenant_id,id),
 FOREIGN KEY(source_platform_template_id,source_platform_version) REFERENCES platform_template_versions(template_id,version)
);
CREATE TABLE decoration_pages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), name text NOT NULL,
 template_id uuid, render_mode text NOT NULL CHECK(render_mode IN('schema','code')), draft jsonb NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), current_release_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,template_id) REFERENCES tenant_templates(tenant_id,id)
);
CREATE TABLE decoration_releases (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), page_id uuid NOT NULL,
 render_mode text NOT NULL CHECK(render_mode IN('schema','code')), design jsonb NOT NULL, draft_version bigint NOT NULL,
 content_hash text NOT NULL, created_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 idempotency_key text, request_hash text NOT NULL DEFAULT '', affected_link_ids jsonb NOT NULL DEFAULT '[]',
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,page_id,id), UNIQUE(tenant_id,page_id,idempotency_key),
 FOREIGN KEY(tenant_id,page_id) REFERENCES decoration_pages(tenant_id,id), FOREIGN KEY(tenant_id,created_by) REFERENCES tenant_memberships(tenant_id,id)
);
ALTER TABLE decoration_pages ADD CONSTRAINT page_current_release_scope FOREIGN KEY(tenant_id,id,current_release_id) REFERENCES decoration_releases(tenant_id,page_id,id);
ALTER TABLE access_links ADD CONSTRAINT access_link_page_scope FOREIGN KEY(tenant_id,page_id) REFERENCES decoration_pages(tenant_id,id);
ALTER TABLE access_links ADD CONSTRAINT access_link_release_scope FOREIGN KEY(tenant_id,page_id,pinned_release_id) REFERENCES decoration_releases(tenant_id,page_id,id);
ALTER TABLE access_links ADD CONSTRAINT access_link_pin_has_page CHECK(pinned_release_id IS NULL OR page_id IS NOT NULL);
CREATE TABLE decoration_previews (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), page_id uuid NOT NULL,
 render_mode text NOT NULL CHECK(render_mode IN('schema','code')), design jsonb NOT NULL,
 token_hash text NOT NULL, expires_at timestamptz NOT NULL DEFAULT now()+interval '15 minutes', created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,page_id) REFERENCES decoration_pages(tenant_id,id)
);
CREATE TABLE decoration_actions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), page_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN('publish','rollback')), previous_release_id uuid, release_id uuid NOT NULL,
 actor_id uuid NOT NULL, affected_link_ids jsonb NOT NULL DEFAULT '[]', idempotency_key text, request_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,page_id,action,idempotency_key),
 FOREIGN KEY(tenant_id,page_id) REFERENCES decoration_pages(tenant_id,id),
 FOREIGN KEY(tenant_id,page_id,release_id) REFERENCES decoration_releases(tenant_id,page_id,id),
 FOREIGN KEY(tenant_id,actor_id) REFERENCES tenant_memberships(tenant_id,id)
);
CREATE TABLE ai_design_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id), template_id uuid NOT NULL,
 created_by uuid NOT NULL, name text NOT NULL, prompt text NOT NULL, render_mode text NOT NULL CHECK(render_mode IN('schema','code')),
 model text NOT NULL, base_version bigint NOT NULL, status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','running','validating','succeeded','failed','cancelled')),
 result jsonb, applied boolean NOT NULL DEFAULT false, error_code text NOT NULL DEFAULT '', error_message text NOT NULL DEFAULT '',
 usage jsonb NOT NULL DEFAULT '{}', billing_status text NOT NULL DEFAULT 'not_called' CHECK(billing_status IN('not_called','reported','uncertain')),
 lease_until timestamptz, started_at timestamptz, completed_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,template_id) REFERENCES tenant_templates(tenant_id,id),
 FOREIGN KEY(tenant_id,created_by) REFERENCES tenant_memberships(tenant_id,id)
);
CREATE INDEX ai_design_tenant_pending ON ai_design_jobs(tenant_id,created_at) WHERE status IN('queued','running','validating');
-- 调度目录只携带随机标识，不保存需求、模型输出或客户数据。
CREATE TABLE ai_design_queue (
 job_id uuid PRIMARY KEY, tenant_id uuid NOT NULL, available_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,job_id) REFERENCES ai_design_jobs(tenant_id,id) ON DELETE CASCADE
);
COMMENT ON TABLE platform_templates IS '平台公开模板当前版本与可选状态';
COMMENT ON TABLE platform_template_versions IS '平台模板不可变版本快照';
COMMENT ON TABLE tenant_templates IS '租户私有模板及独立草稿';
COMMENT ON TABLE decoration_pages IS '租户装修页面与当前发布指针';
COMMENT ON TABLE decoration_releases IS '装修页面不可变发布快照';
COMMENT ON TABLE decoration_previews IS '短期授权的隔离装修预览快照';
COMMENT ON TABLE decoration_actions IS '装修发布和回滚的操作审计与幂等结果';
COMMENT ON TABLE ai_design_jobs IS '租户持久AI装修任务及供应商用量';
COMMENT ON TABLE ai_design_queue IS '不携带业务载荷的AI任务调度目录';
DO $$ DECLARE t text; c text; label text; BEGIN
 FOREACH t IN ARRAY ARRAY['platform_templates','platform_template_versions','tenant_templates','decoration_pages','decoration_releases','decoration_previews','decoration_actions','ai_design_jobs','ai_design_queue'] LOOP
  FOR c IN SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=t LOOP
   label:=CASE c WHEN 'id' THEN '记录随机编号' WHEN 'tenant_id' THEN '归属租户编号' WHEN 'name' THEN '显示名称' WHEN 'render_mode' THEN '结构化或代码渲染模式' WHEN 'design' THEN '经过校验的静态设计快照' WHEN 'draft' THEN '当前可编辑设计草稿' WHEN 'version' THEN '乐观并发版本号' WHEN 'status' THEN '业务状态' WHEN 'created_at' THEN '创建时间' WHEN 'updated_at' THEN '最近修改时间' WHEN 'template_id' THEN '来源私有模板编号' WHEN 'owner_membership_id' THEN '模板创建成员编号' WHEN 'source_platform_template_id' THEN '来源平台模板编号' WHEN 'source_platform_version' THEN '复制的平台模板不可变版本号' WHEN 'current_release_id' THEN '当前线上不可变发布编号' WHEN 'page_id' THEN '所属装修页面编号' WHEN 'draft_version' THEN '发布时固定的草稿版本号' WHEN 'content_hash' THEN '固定设计内容摘要' WHEN 'created_by' THEN '创建成员编号' WHEN 'idempotency_key' THEN '调用方提供的幂等标识' WHEN 'request_hash' THEN '幂等操作请求内容摘要' WHEN 'affected_link_ids' THEN '本次切换的接入链接编号清单' WHEN 'token_hash' THEN '短期预览令牌摘要' WHEN 'expires_at' THEN '预览授权到期时间' WHEN 'action' THEN '发布或回滚操作类型' WHEN 'previous_release_id' THEN '操作前线上版本编号' WHEN 'release_id' THEN '操作后线上版本编号' WHEN 'actor_id' THEN '执行成员编号' WHEN 'prompt' THEN '租户主动提交的装修需求' WHEN 'model' THEN '实际调用模型名称' WHEN 'base_version' THEN '任务创建时目标草稿版本' WHEN 'result' THEN '供应商结果候选设计' WHEN 'applied' THEN '结果是否成功写入原草稿' WHEN 'error_code' THEN '可安全展示的失败分类' WHEN 'error_message' THEN '可安全展示的失败原因' WHEN 'usage' THEN '供应商报告的任务级用量' WHEN 'billing_status' THEN '未调用或已报告或待核对的计费状态' WHEN 'lease_until' THEN 'worker租约截止时间' WHEN 'started_at' THEN '模型调用开始时间' WHEN 'completed_at' THEN '任务终止时间' WHEN 'job_id' THEN 'AI任务随机编号' WHEN 'available_at' THEN '调度目录下次可处理时间' ELSE '业务字段' END;
   EXECUTE format('COMMENT ON COLUMN %I.%I IS %L',t,c,label);
  END LOOP;
 END LOOP;
 FOREACH t IN ARRAY ARRAY['tenant_templates','decoration_pages','decoration_releases','decoration_previews','decoration_actions','ai_design_jobs'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id())',t);
 END LOOP;
END $$;
-- 平台模板修改由平台鉴权接口执行；版本与发布记录禁止原地覆盖。
CREATE FUNCTION decoration_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION '不可变装修快照不允许修改或删除' USING ERRCODE='23514'; END $$;
COMMENT ON FUNCTION decoration_immutable() IS '阻止已发布快照和平台模板历史版本被原地修改';
CREATE TRIGGER immutable_decoration_release BEFORE UPDATE OR DELETE ON decoration_releases FOR EACH ROW EXECUTE FUNCTION decoration_immutable();
CREATE TRIGGER immutable_platform_version BEFORE UPDATE OR DELETE ON platform_template_versions FOR EACH ROW EXECUTE FUNCTION decoration_immutable();
CREATE FUNCTION decoration_render(p_id uuid,p_token_hash text) RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT jsonb_build_object('render_mode',render_mode,'design',design,'release_id',id) FROM public.decoration_releases WHERE id=p_id
 UNION ALL SELECT jsonb_build_object('render_mode',render_mode,'design',design,'release_id',id) FROM public.decoration_previews WHERE id=p_id AND token_hash=p_token_hash AND expires_at>now() LIMIT 1
$$;
COMMENT ON FUNCTION decoration_render(uuid,text) IS '独立代码站点仅定位公开发布或持短期预览凭证的静态设计，不返回访客数据';
REVOKE ALL ON FUNCTION decoration_render(uuid,text) FROM PUBLIC;
-- 本函数由独立迁移管理员所有；应用角色仅获得执行权，绝不获得BYPASSRLS。
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN GRANT EXECUTE ON FUNCTION decoration_render(uuid,text) TO goodchat_saas_app; END IF;
END $$;
INSERT INTO platform_templates(id,name,render_mode,design)
SELECT ('10000000-0000-4000-8000-00000000000'||n)::uuid,names[n],'schema',jsonb_build_object(
 'schema_version',1,'theme',jsonb_build_object('primary_color',colors[n],'background',backgrounds[n],'text_color',texts[n],'radius',16),
 'brand',jsonb_build_object('title',names[n],'subtitle',subtitles[n],'logo_url',''),
 'blocks',jsonb_build_array(jsonb_build_object('id','welcome','type','welcome','title',titles[n],'content',subtitles[n],'items','[]'::jsonb),
 jsonb_build_object('id','info','type',types[n],'title',infos[n],'content','欢迎说明您的需求，客服会尽快为您服务。','items',CASE WHEN n=3 THEN '[{"question":"如何查询售后进度？","answer":"请在下方聊天中提供订单信息。"},{"question":"售后受理范围","answer":"我们会根据您的具体情况提供帮助。"}]'::jsonb ELSE '[]'::jsonb END),
 jsonb_build_object('id','chat','type','chat','title','在线咨询','content','','items','[]'::jsonb)),'custom_css','')
FROM generate_series(1,5) n,
 LATERAL (SELECT ARRAY['简洁聊天','品牌展示','售后服务','游戏服务','深色界面'] names,ARRAY['#1f6f5b','#876747','#2d7167','#726133','#c1d7c8'] colors,ARRAY['#f6f7f4','#faf7f1','#f2f8f5','#f5f4ed','#18221d'] backgrounds,ARRAY['#17231f','#2b231b','#173027','#28281f','#edf5ef'] texts,ARRAY['随时为您答疑','让每一次咨询更有温度','清晰的流程，贴心的服务','交流需求，轻松开启服务','安静舒适的夜间咨询'] subtitles,ARRAY['您好，有什么可以帮您？','欢迎来到我们的品牌服务','售后服务中心','游戏服务咨询','欢迎在线咨询'] titles,ARRAY['服务说明','品牌介绍','常见问题','服务公告','服务说明'] infos,ARRAY['announcement','contact','faq','announcement','announcement'] types) seeds;
INSERT INTO platform_template_versions(template_id,version,name,render_mode,design) SELECT id,version,name,render_mode,design FROM platform_templates;
