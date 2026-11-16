-- 文件作用说明：添加平台套餐发布译文，原始名称、价格权益和历史快照保持不变。
ALTER TABLE plans ADD COLUMN translations jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(translations)='object');
COMMENT ON COLUMN plans.translations IS '按语言保存套餐名称和描述译文，价格权益和历史开通快照保持不变';
