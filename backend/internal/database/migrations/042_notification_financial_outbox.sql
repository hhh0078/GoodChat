-- 文件作用说明：在不可变AI账本入账事务内建立邮件事件，按未处理索引领取，不反复扫描全部历史流水。
CREATE TABLE notification_sources(
 source_id uuid PRIMARY KEY REFERENCES ai_wallet_entries(id),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 processed_at timestamptz
);
COMMENT ON TABLE notification_sources IS 'AI账本同事务邮件事件，不保存密钥和客户正文';
COMMENT ON COLUMN notification_sources.source_id IS '不可变AI账本流水编号，同时用于去重';
COMMENT ON COLUMN notification_sources.tenant_id IS '所属租户，沿用入账事务的认证上下文';
COMMENT ON COLUMN notification_sources.created_at IS '事件创建UTC时间，不作为跨事务游标';
COMMENT ON COLUMN notification_sources.processed_at IS '通知任务已可靠保存的时间';
ALTER TABLE notification_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY notification_source_tenant ON notification_sources USING(tenant_id=current_tenant_id()) WITH CHECK(tenant_id=current_tenant_id());
CREATE INDEX notification_sources_pending ON notification_sources(tenant_id,created_at,source_id) WHERE processed_at IS NULL;
COMMENT ON INDEX notification_sources_pending IS '仅扫描当前租户未处理事件，避免历史账本全表轮询';
CREATE FUNCTION enqueue_financial_notice_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO notification_sources(source_id,tenant_id) VALUES(NEW.id,NEW.tenant_id) ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
COMMENT ON FUNCTION enqueue_financial_notice_source() IS '账本写入与邮件事件同事务提交，回滚时共同撤销';
CREATE TRIGGER financial_notice_source AFTER INSERT ON ai_wallet_entries FOR EACH ROW EXECUTE FUNCTION enqueue_financial_notice_source();
