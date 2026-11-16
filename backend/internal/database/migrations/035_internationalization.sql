-- 文件作用说明：添加个人语言与显示时区、企业默认语言、可发布内容译文及异步任务语言快照；保留历史原文和企业业务时区。
ALTER TABLE users ADD COLUMN preferred_locale text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN display_timezone text NOT NULL DEFAULT '';
COMMENT ON COLUMN users.preferred_locale IS '用户明确确认的个人界面及通知语言，空值表示尚未确认';
COMMENT ON COLUMN users.display_timezone IS '用户明确确认的个人显示IANA时区，不改变租户营业统计口径';
ALTER TABLE tenants ADD COLUMN default_locale text NOT NULL DEFAULT 'zh-CN';
COMMENT ON COLUMN tenants.default_locale IS '企业默认内容语言，不覆盖成员或访客已选语言';
