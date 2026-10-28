-- 文件作用说明：扩展快捷回复为文字、图片、语音和选项，按成员记录成功发送次数并冻结每条消息的话术版本。
ALTER TABLE quick_replies ADD COLUMN message_type text NOT NULL DEFAULT 'text' CHECK(message_type IN('text','image','audio','file'));
ALTER TABLE quick_replies ADD COLUMN attachment_id uuid;
ALTER TABLE quick_replies ADD COLUMN options jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(options)='array');
ALTER TABLE quick_replies ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK(version>0);
ALTER TABLE quick_replies ADD CONSTRAINT quick_reply_attachment FOREIGN KEY(tenant_id,attachment_id) REFERENCES attachments(tenant_id,id);
COMMENT ON COLUMN quick_replies.message_type IS '快捷回复发送的消息类型，选项使用文字类型并携带options';
COMMENT ON COLUMN quick_replies.attachment_id IS '本租户受控上传的快捷回复媒体附件编号';
COMMENT ON COLUMN quick_replies.options IS '选项显示文案与发送值，实际消息去重编号由发送事务生成';
COMMENT ON COLUMN quick_replies.version IS '快捷回复修改版本，未提交重试必须匹配，已保存消息返回原结果';

ALTER TABLE messages ADD COLUMN quick_reply_id uuid;
ALTER TABLE messages ADD COLUMN quick_reply_version bigint;
ALTER TABLE messages ADD CONSTRAINT message_quick_reply FOREIGN KEY(tenant_id,quick_reply_id) REFERENCES quick_replies(tenant_id,id);
ALTER TABLE messages ADD CONSTRAINT message_quick_reply_version CHECK((quick_reply_id IS NULL AND quick_reply_version IS NULL) OR (quick_reply_id IS NOT NULL AND quick_reply_version IS NOT NULL AND quick_reply_version>0));
COMMENT ON COLUMN messages.quick_reply_id IS '首次发送时实际使用的可见快捷回复编号，普通消息为空';
COMMENT ON COLUMN messages.quick_reply_version IS '消息首次入库时冻结的快捷回复版本，用于原编号重试核验';
COMMENT ON COLUMN messages.options IS '欢迎或客服快捷选项及服务端生成的稳定访客消息去重编号';

CREATE TABLE quick_reply_usage(
 tenant_id uuid NOT NULL,
 membership_id uuid NOT NULL,
 quick_reply_id uuid NOT NULL,
 use_count bigint NOT NULL DEFAULT 1 CHECK(use_count>0),
 last_used_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,membership_id,quick_reply_id),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES tenant_memberships(tenant_id,id),
 FOREIGN KEY(tenant_id,quick_reply_id) REFERENCES quick_replies(tenant_id,id)
);
COMMENT ON TABLE quick_reply_usage IS '每个客服成功发送快捷回复的使用次数，同消息编号重试不会重复累计';
COMMENT ON COLUMN quick_reply_usage.tenant_id IS '统计所属租户编号';
COMMENT ON COLUMN quick_reply_usage.membership_id IS '实际发送快捷回复的本租户成员编号';
COMMENT ON COLUMN quick_reply_usage.quick_reply_id IS '成功发送所使用的快捷回复编号';
COMMENT ON COLUMN quick_reply_usage.use_count IS '首次成功保存消息的累计次数，失败或幂等重试不增加';
COMMENT ON COLUMN quick_reply_usage.last_used_at IS '最近一次首次成功保存快捷回复消息的时间';
ALTER TABLE quick_reply_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE quick_reply_usage FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON quick_reply_usage USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
