-- 文件作用说明：只按公开消息维护咨询活动时间与序号，配合成员已读游标提供真实未读数，避免已读与心跳事件改变卡片顺序。
ALTER TABLE conversations ADD COLUMN last_message_at timestamptz;
ALTER TABLE conversations ADD COLUMN last_message_seq bigint NOT NULL DEFAULT 0;
COMMENT ON COLUMN conversations.last_message_at IS '最近一条公开消息的保存时间，不受内部备注、心跳或已读事件影响';
COMMENT ON COLUMN conversations.last_message_seq IS '最近公开消息的持久事件序号，零表示尚无公开消息';

-- 按租户回填，兼容启用强制行级隔离且迁移角色不绕过策略的部署。
DO $$ DECLARE tenant uuid; previous_context text:=current_setting('app.tenant_id',true); BEGIN
FOR tenant IN SELECT id FROM tenants LOOP
PERFORM set_config('app.tenant_id',tenant::text,true);
UPDATE contacts SET avatar_url='/images/avatars/default-'||lpad((floor(random()*50)+1)::integer::text,2,'0')||'.png',field_sources=field_sources||'{"avatar_url":"platform_default"}'::jsonb
WHERE avatar_url='' AND NOT archived AND NOT locked_fields ? 'avatar_url';
UPDATE conversations c SET last_message_at=m.last_at,last_message_seq=m.last_seq
FROM (SELECT conversation_id,max(created_at) last_at,max(event_seq) last_seq FROM messages WHERE audience='public' GROUP BY conversation_id)m
WHERE c.id=m.conversation_id;
END LOOP;
PERFORM set_config('app.tenant_id',coalesce(previous_context,''),true);
END $$;

CREATE INDEX conversation_recent_messages ON conversations(tenant_id,(coalesce(last_message_at,created_at)) DESC,id);
CREATE INDEX message_visitor_unread ON messages(tenant_id,conversation_id,event_seq) WHERE audience='public' AND sender_kind='visitor' AND withdrawn_at IS NULL;

-- record_public_message_activity 仅在真实公开消息落库时维护活动位置，与消息保存处于同一事务。
CREATE FUNCTION record_public_message_activity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
BEGIN
IF NEW.audience='public' THEN
UPDATE conversations SET last_message_at=greatest(coalesce(last_message_at,NEW.created_at),NEW.created_at),last_message_seq=greatest(last_message_seq,NEW.event_seq)
WHERE id=NEW.conversation_id AND tenant_id=NEW.tenant_id;
END IF;
RETURN NEW;
END $$;
COMMENT ON FUNCTION record_public_message_activity() IS '公开消息落库后更新所属咨询的最近消息活动，忽略短期与内部事件';
CREATE TRIGGER conversation_public_message_activity AFTER INSERT ON messages FOR EACH ROW EXECUTE FUNCTION record_public_message_activity();
