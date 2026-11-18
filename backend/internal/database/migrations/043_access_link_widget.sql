-- 文件作用说明：为接入链接增加可配置的网页悬浮窗及并发保存版本，兼容现有链接和会话。
ALTER TABLE access_links ADD COLUMN widget_settings jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE access_links ADD COLUMN widget_version bigint NOT NULL DEFAULT 1;
COMMENT ON COLUMN access_links.widget_settings IS 'JS悬浮窗展示、图标、位置、尺寸、自动展开和营业时段配置';
COMMENT ON COLUMN access_links.widget_version IS '悬浮窗配置乐观锁版本，避免多人保存互相覆盖';
