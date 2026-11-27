-- 文件作用说明：允许同一成员保留独立机器人配置，并由接入链接显式绑定机器人；既有入口继续沿用租户默认配置。
ALTER TABLE ai_bots DROP CONSTRAINT IF EXISTS ai_bots_tenant_id_owner_membership_id_key;
CREATE INDEX IF NOT EXISTS ai_bots_owner_lookup ON ai_bots(tenant_id,owner_membership_id,id);
COMMENT ON INDEX ai_bots_owner_lookup IS '按租户成员稳定列出独立机器人配置，默认选择仍由租户接待设置确定';
ALTER TABLE access_links ADD COLUMN IF NOT EXISTS ai_bot_id uuid;
COMMENT ON COLUMN access_links.ai_bot_id IS '当前入口显式使用的机器人配置，空值沿用原租户默认接待；不改变人工转接与暂停规则';
-- 运营归并可先准备同一结构；正式发布再次执行时不重复创建约束。
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='access_links'::regclass AND conname='access_link_ai_bot_scope') THEN
  ALTER TABLE access_links ADD CONSTRAINT access_link_ai_bot_scope FOREIGN KEY(tenant_id,ai_bot_id) REFERENCES ai_bots(tenant_id,id);
 END IF;
END $$;
