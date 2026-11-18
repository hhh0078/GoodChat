-- 文件作用说明：允许平台显式批准官方运营租户后付记账，并将新机器人每日零预算解释为不设金额上限；保留原有预付费租户限制。
ALTER TABLE ai_wallets ADD COLUMN unlimited boolean NOT NULL DEFAULT false;
COMMENT ON COLUMN ai_wallets.unlimited IS '平台批准的不限制余额运营模式，费用照常记录，禁止租户自行开启';
ALTER TABLE ai_wallets DROP CONSTRAINT ai_wallets_balance_micro_check;
-- PostgreSQL对引用多个字段的内联CHECK默认使用表级名称，兼容已有迁移的两种命名。
ALTER TABLE ai_wallets DROP CONSTRAINT IF EXISTS ai_wallets_reserved_micro_check;
ALTER TABLE ai_wallets DROP CONSTRAINT IF EXISTS ai_wallets_check;
ALTER TABLE ai_wallets ADD CONSTRAINT ai_wallets_balance_micro_check CHECK(unlimited OR balance_micro>=0);
ALTER TABLE ai_wallets ADD CONSTRAINT ai_wallets_reserved_micro_check CHECK(reserved_micro>=0 AND (unlimited OR reserved_micro<=balance_micro));
COMMENT ON COLUMN ai_wallets.balance_micro IS '账面余额，百万微元为一元；不限制余额运营模式允许为负以累计已发生费用';
