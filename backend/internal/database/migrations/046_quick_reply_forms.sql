-- 文件作用说明：扩展快捷回复为表单，将表单快照与访客提交存为会话消息，关键词可绑定租户快捷回复。
ALTER TABLE quick_replies DROP CONSTRAINT quick_replies_message_type_check;
ALTER TABLE quick_replies ADD CONSTRAINT quick_replies_message_type_check CHECK(message_type IN('text','image','audio','file','form'));
ALTER TABLE quick_replies ADD COLUMN form_fields jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(form_fields)='array');
COMMENT ON COLUMN quick_replies.form_fields IS '表单字段定义，含稳定编号、显示标签、是否必填与文本类型';
ALTER TABLE messages DROP CONSTRAINT messages_message_type_check;
ALTER TABLE messages ADD CONSTRAINT messages_message_type_check CHECK(message_type IN('text','image','audio','file','form','form_result'));
ALTER TABLE messages ADD COLUMN form_fields jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(form_fields)='array');
ALTER TABLE messages ADD COLUMN form_request_id uuid;
ALTER TABLE messages ADD COLUMN form_values jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(form_values)='object');
ALTER TABLE messages ADD CONSTRAINT message_form_request FOREIGN KEY(tenant_id,form_request_id) REFERENCES messages(tenant_id,id);
ALTER TABLE messages ADD CONSTRAINT message_form_result CHECK((message_type='form_result')=(form_request_id IS NOT NULL));
CREATE UNIQUE INDEX message_one_form_submission ON messages(tenant_id,form_request_id) WHERE form_request_id IS NOT NULL;
COMMENT ON COLUMN messages.form_fields IS '发送或提交时冻结的表单字段，历史消息不受配置修改影响';
COMMENT ON COLUMN messages.form_request_id IS '访客提交对应的表单请求消息编号，每条请求只接受一次提交';
COMMENT ON COLUMN messages.form_values IS '经过服务端字段及必填校验的原始提交值，不从链接或模型代填';
ALTER TABLE keyword_rules ADD COLUMN quick_reply_id uuid;
ALTER TABLE keyword_rules ADD CONSTRAINT keyword_quick_reply FOREIGN KEY(tenant_id,quick_reply_id) REFERENCES quick_replies(tenant_id,id);
COMMENT ON COLUMN keyword_rules.quick_reply_id IS '关键词命中后发送的快捷回复，空值沿用原有文字回复';
