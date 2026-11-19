-- 文件作用说明：新增按语言独立存储的官网Markdown文章、平台发布设置与可恢复的生成和搜索提交任务。
CREATE TABLE site_articles (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 group_id uuid NOT NULL DEFAULT gen_random_uuid(),
 source_id uuid REFERENCES site_articles(id),
 language text NOT NULL CHECK(language IN('zh-CN','en','zh-TW','ja','ko','es','pt-BR','fr','de')),
 title text NOT NULL CHECK(length(title) BETWEEN 1 AND 160),
 title_key text NOT NULL,
 slug text NOT NULL CHECK(slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug)<=120),
 summary text NOT NULL DEFAULT '',
 body_markdown text NOT NULL,
 body_html text NOT NULL,
 keywords text[] NOT NULL DEFAULT '{}',
 status text NOT NULL DEFAULT 'draft' CHECK(status IN('draft','published','archived')),
 published_at timestamptz,
 version bigint NOT NULL DEFAULT 1,
 created_by uuid REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(group_id,language), UNIQUE(language,slug),
 CHECK(status<>'published' OR published_at IS NOT NULL)
);
CREATE UNIQUE INDEX site_articles_unique_title ON site_articles(language,title_key) WHERE status<>'archived';
CREATE INDEX site_articles_public ON site_articles(language,published_at DESC,id) WHERE status='published';
CREATE TABLE site_article_settings (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 ai_enabled boolean NOT NULL DEFAULT false,
 ai_prompt text NOT NULL DEFAULT '',
 source_language text NOT NULL DEFAULT 'zh-CN',
 target_languages text[] NOT NULL DEFAULT '{}',
 daily_time text NOT NULL DEFAULT '18:00',
 timezone text NOT NULL DEFAULT 'Asia/Shanghai',
 next_run_at timestamptz,
 baidu_enabled boolean NOT NULL DEFAULT false,
 baidu_token_cipher text NOT NULL DEFAULT '',
 indexnow_enabled boolean NOT NULL DEFAULT false,
 indexnow_key_cipher text NOT NULL DEFAULT '',
 google_enabled boolean NOT NULL DEFAULT false,
 google_site text NOT NULL DEFAULT '',
 google_credentials_cipher text NOT NULL DEFAULT '',
 version bigint NOT NULL DEFAULT 1,
 updated_by uuid REFERENCES users(id),
 updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO site_article_settings(id) VALUES(true);
CREATE TABLE site_article_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 kind text NOT NULL CHECK(kind IN('generate','translate','submit')),
 article_id uuid REFERENCES site_articles(id),
 provider text NOT NULL DEFAULT '',
 payload jsonb NOT NULL DEFAULT '{}',
 status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','running','succeeded','failed','cancelled')),
 dedupe_key text NOT NULL UNIQUE,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 run_after timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 lease_token uuid,
 result jsonb NOT NULL DEFAULT '{}',
 error_code text NOT NULL DEFAULT '',
 error_message text NOT NULL DEFAULT '',
 created_by uuid REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz
);
CREATE INDEX site_article_jobs_due ON site_article_jobs(run_after,created_at) WHERE status IN('queued','running');

COMMENT ON TABLE site_articles IS '官网文章，每个语言版本独立一行，仅已发布版本对外可见';
COMMENT ON COLUMN site_articles.id IS '语言版本文章编号';
COMMENT ON COLUMN site_articles.group_id IS '同一篇文章各语言版本的关联编号';
COMMENT ON COLUMN site_articles.source_id IS '翻译来源文章编号，原文为空';
COMMENT ON COLUMN site_articles.language IS '本文使用的受支持语言';
COMMENT ON COLUMN site_articles.title IS '本语言标题，同时用于网页标题和主标题';
COMMENT ON COLUMN site_articles.title_key IS '规范化标题，用于同语言重复检查';
COMMENT ON COLUMN site_articles.slug IS '本语言规范网址路径标识';
COMMENT ON COLUMN site_articles.summary IS '本语言摘要及搜索描述';
COMMENT ON COLUMN site_articles.body_markdown IS '可编辑的Markdown原文';
COMMENT ON COLUMN site_articles.body_html IS '由服务端安全Markdown渲染生成的HTML';
COMMENT ON COLUMN site_articles.keywords IS '本语言主题关键词';
COMMENT ON COLUMN site_articles.status IS '草稿、已发布或已下架状态';
COMMENT ON COLUMN site_articles.published_at IS '首次对外发布时间，草稿为空';
COMMENT ON COLUMN site_articles.version IS '编辑及翻译任务并发校验版本';
COMMENT ON COLUMN site_articles.created_by IS '创建文章的平台管理员账号';
COMMENT ON COLUMN site_articles.created_at IS '版本建档时间';
COMMENT ON COLUMN site_articles.updated_at IS '最近内容或发布状态更新时间';

COMMENT ON TABLE site_article_settings IS '平台文章定时发布与搜索引擎配置，敏感凭据仅保存密文';
COMMENT ON COLUMN site_article_settings.id IS '平台唯一设置行，固定为真';
COMMENT ON COLUMN site_article_settings.ai_enabled IS '是否启用每日AI文章任务';
COMMENT ON COLUMN site_article_settings.ai_prompt IS '管理员自定义文章主题、场景和关键词提示词';
COMMENT ON COLUMN site_article_settings.source_language IS '每日文章的原文语言';
COMMENT ON COLUMN site_article_settings.target_languages IS '每日文章需要生成的翻译语言';
COMMENT ON COLUMN site_article_settings.daily_time IS '每日发布时间，二十四小时制时分';
COMMENT ON COLUMN site_article_settings.timezone IS '发布计划使用的IANA时区，默认北京时间';
COMMENT ON COLUMN site_article_settings.next_run_at IS '下一次应认领的发布时间';
COMMENT ON COLUMN site_article_settings.baidu_enabled IS '是否自动向百度提交已发布文章';
COMMENT ON COLUMN site_article_settings.baidu_token_cipher IS '百度提交令牌密文';
COMMENT ON COLUMN site_article_settings.indexnow_enabled IS '是否自动向IndexNow提交已发布文章';
COMMENT ON COLUMN site_article_settings.indexnow_key_cipher IS 'IndexNow所有权验证键密文，验证文件可公开读取';
COMMENT ON COLUMN site_article_settings.google_enabled IS '是否自动向Google Search Console提交站点地图';
COMMENT ON COLUMN site_article_settings.google_site IS 'Google站点资源标识';
COMMENT ON COLUMN site_article_settings.google_credentials_cipher IS 'Google服务账号凭据密文';
COMMENT ON COLUMN site_article_settings.version IS '平台设置并发版本';
COMMENT ON COLUMN site_article_settings.updated_by IS '最后修改设置的平台管理员';
COMMENT ON COLUMN site_article_settings.updated_at IS '设置最后更新时间';

COMMENT ON TABLE site_article_jobs IS '文章生成、翻译和搜索提交任务，使用租约恢复且保留实际执行结果';
COMMENT ON COLUMN site_article_jobs.id IS '任务编号';
COMMENT ON COLUMN site_article_jobs.kind IS '生成、翻译或搜索提交类型';
COMMENT ON COLUMN site_article_jobs.article_id IS '关联的文章语言版本编号';
COMMENT ON COLUMN site_article_jobs.provider IS '搜索提交平台标识，其他任务为空';
COMMENT ON COLUMN site_article_jobs.payload IS '执行参数快照，不保存模型密钥或搜索凭据';
COMMENT ON COLUMN site_article_jobs.status IS '排队、运行、成功、失败或取消状态';
COMMENT ON COLUMN site_article_jobs.dedupe_key IS '幂等唯一键，防止同一天和同一版本重复处理';
COMMENT ON COLUMN site_article_jobs.attempts IS '已开始的执行次数';
COMMENT ON COLUMN site_article_jobs.run_after IS '任务最早执行时间';
COMMENT ON COLUMN site_article_jobs.lease_until IS '当前执行者持有租约的截止时间';
COMMENT ON COLUMN site_article_jobs.lease_token IS '执行者围栏令牌，阻止过期执行者发布结果';
COMMENT ON COLUMN site_article_jobs.result IS '实际结果及计量摘要，不等同于搜索引擎已收录';
COMMENT ON COLUMN site_article_jobs.error_code IS '安全的失败分类';
COMMENT ON COLUMN site_article_jobs.error_message IS '不含凭据的失败原因';
COMMENT ON COLUMN site_article_jobs.created_by IS '手动发起任务的平台管理员，计划任务为空';
COMMENT ON COLUMN site_article_jobs.created_at IS '任务创建时间';
COMMENT ON COLUMN site_article_jobs.completed_at IS '任务最终完成时间';

-- 授权现有非特权运行角色，平台权限仍由经过认证的HTTP入口校验。
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN GRANT SELECT,INSERT,UPDATE,DELETE ON site_articles,site_article_settings,site_article_jobs TO goodchat_saas_app; END IF; END $$;
