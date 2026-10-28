-- 文件作用说明：保存成员对当前接待段的查看确认及提醒领取位置，新增租户重复提醒间隔和提示音出厂配置。
ALTER TABLE chat_settings ALTER COLUMN settings SET DEFAULT '{"welcome_enabled":true,"welcome_text":"您好，请问有什么可以帮助您？","welcome_delay_seconds":0,"keywords_enabled":true,"typing_enabled":true,"repeat_unread_seconds":10,"notification_sound_id":"gentle"}';
DO $$ DECLARE tenant uuid; previous_context text:=current_setting('app.tenant_id',true); BEGIN
FOR tenant IN SELECT id FROM tenants LOOP
PERFORM set_config('app.tenant_id',tenant::text,true);
UPDATE chat_settings SET settings='{"repeat_unread_seconds":10,"notification_sound_id":"gentle"}'::jsonb||settings WHERE tenant_id=tenant;
END LOOP;
PERFORM set_config('app.tenant_id',coalesce(previous_context,''),true);
END $$;

CREATE TABLE conversation_attention_state(
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 conversation_id uuid NOT NULL,
 membership_id uuid NOT NULL,
 viewed_assignment_id uuid,
 reminder_key text NOT NULL DEFAULT '',
 last_reminded_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,conversation_id,membership_id),
 FOREIGN KEY(tenant_id,conversation_id) REFERENCES conversations(tenant_id,id),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES tenant_memberships(tenant_id,id),
 FOREIGN KEY(tenant_id,viewed_assignment_id) REFERENCES conversation_assignments(tenant_id,id)
);
COMMENT ON TABLE conversation_attention_state IS '成员对当前接待段的实际查看确认和重复提醒领取状态，多个标签或设备共用避免叠响';
COMMENT ON COLUMN conversation_attention_state.tenant_id IS '认证上下文中的所属租户编号';
COMMENT ON COLUMN conversation_attention_state.conversation_id IS '提醒对应咨询编号';
COMMENT ON COLUMN conversation_attention_state.membership_id IS '当前接待成员编号';
COMMENT ON COLUMN conversation_attention_state.viewed_assignment_id IS '成员实际打开并确认查看的接待段编号，空值表示尚未查看';
COMMENT ON COLUMN conversation_attention_state.reminder_key IS '最早未读消息或当前未查看接待段的稳定提醒标识';
COMMENT ON COLUMN conversation_attention_state.last_reminded_at IS '最近一次由客户端原子领取提醒的服务端时间';
COMMENT ON COLUMN conversation_attention_state.updated_at IS '查看或领取状态最近更新时间';
ALTER TABLE conversation_attention_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE conversation_attention_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON conversation_attention_state USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE INDEX conversation_attention_owner ON conversations(tenant_id,current_agent_id,status);
