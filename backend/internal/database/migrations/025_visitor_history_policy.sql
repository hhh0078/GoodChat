-- 文件作用说明：配置访客历史客服窗口和跨客服聊天分享，记录精确接待段消息水位并统一历史可见边界；未知旧水位不按时间猜测。
ALTER TABLE chat_settings ALTER COLUMN settings SET DEFAULT '{"welcome_enabled":true,"welcome_text":"您好，请问有什么可以帮助您？","welcome_delay_seconds":0,"keywords_enabled":true,"typing_enabled":true,"repeat_unread_seconds":10,"notification_sound_id":"gentle","history_agent_minutes":0,"share_previous_chats":true}';
DO $$ DECLARE tenant uuid; previous_context text:=current_setting('app.tenant_id',true); BEGIN
FOR tenant IN SELECT id FROM tenants LOOP
PERFORM set_config('app.tenant_id',tenant::text,true);
UPDATE chat_settings SET settings='{"history_agent_minutes":0,"share_previous_chats":true}'::jsonb||settings WHERE tenant_id=tenant;
END LOOP;
PERFORM set_config('app.tenant_id',coalesce(previous_context,''),true);
END $$;

ALTER TABLE conversation_assignments ADD COLUMN visibility_after_seq bigint CHECK(visibility_after_seq>=0);
ALTER TABLE conversation_assignments ADD COLUMN visibility_through_seq bigint CHECK(visibility_through_seq>=0);
COMMENT ON COLUMN conversation_assignments.visibility_after_seq IS '本接待段可见消息的排他起点，首次接待从零开始；空表示升级前未知，关闭分享时保守隐藏';
COMMENT ON COLUMN conversation_assignments.visibility_through_seq IS '本接待段释放时包含的末尾消息序号，未释放时为空；未知旧段不猜测时间边界';

CREATE FUNCTION record_assignment_message_boundary() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
DECLARE watermark bigint; previous_end bigint; previous_count bigint; BEGIN
SELECT next_event_seq INTO watermark FROM conversations WHERE id=NEW.conversation_id AND tenant_id=NEW.tenant_id;
IF TG_OP='INSERT' THEN
 SELECT count(*) INTO previous_count FROM conversation_assignments WHERE conversation_id=NEW.conversation_id AND tenant_id=NEW.tenant_id;
 SELECT visibility_through_seq INTO previous_end FROM conversation_assignments WHERE conversation_id=NEW.conversation_id AND tenant_id=NEW.tenant_id ORDER BY created_at DESC,id DESC LIMIT 1;
 NEW.visibility_after_seq:=CASE WHEN previous_count=0 THEN 0 ELSE coalesce(previous_end,watermark) END;
 IF NEW.released_at IS NOT NULL THEN NEW.visibility_through_seq:=watermark; END IF;
ELSIF OLD.released_at IS NULL AND NEW.released_at IS NOT NULL THEN
 NEW.visibility_through_seq:=watermark;
END IF;
RETURN NEW;
END $$;
COMMENT ON FUNCTION record_assignment_message_boundary() IS '在分配和释放的同一事务内冻结消息序号，首次接待保留排队消息，不用接待时间反推旧历史';
CREATE TRIGGER assignment_message_boundary BEFORE INSERT OR UPDATE OF released_at ON conversation_assignments FOR EACH ROW EXECUTE FUNCTION record_assignment_message_boundary();

CREATE FUNCTION chat_history_shared() RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
 SELECT coalesce((SELECT settings->>'share_previous_chats'<>'false' FROM chat_settings WHERE tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid),true)
$$;
COMMENT ON FUNCTION chat_history_shared() IS '读取当前认证租户的聊天历史分享策略，升级前未配置时遵循默认开启';

CREATE FUNCTION chat_conversation_visible(p_member uuid,p_contact uuid,p_admin boolean,p_conversation uuid) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
SELECT EXISTS(SELECT 1 FROM conversations c WHERE c.id=p_conversation AND (
 c.contact_id=p_contact OR p_admin OR
 (p_contact IS NULL AND EXISTS(SELECT 1 FROM tenant_memberships self WHERE self.id=p_member AND self.tenant_id=c.tenant_id AND self.can_serve AND NOT self.disabled AND self.removed_at IS NULL) AND EXISTS(SELECT 1 FROM agent_group_members authorized JOIN agent_groups ag ON ag.id=authorized.group_id AND ag.tenant_id=authorized.tenant_id WHERE authorized.tenant_id=c.tenant_id AND authorized.group_id=c.service_group_id AND authorized.membership_id=p_member AND NOT ag.archived) AND (
 c.current_agent_id=p_member
 OR (c.status='queued' AND EXISTS(SELECT 1 FROM agent_group_members gm WHERE gm.tenant_id=c.tenant_id AND gm.group_id=c.service_group_id AND gm.membership_id=p_member))
 OR EXISTS(SELECT 1 FROM transfer_requests tr WHERE tr.conversation_id=c.id AND tr.tenant_id=c.tenant_id AND tr.target_agent_id=p_member AND tr.status='pending' AND tr.expires_at>statement_timestamp())
 OR (c.status='closed' AND chat_history_shared() AND EXISTS(
 SELECT 1 FROM conversations current JOIN conversation_assignments ca ON ca.id=current.current_assignment_id AND ca.tenant_id=current.tenant_id
 JOIN agent_group_members gm ON gm.tenant_id=current.tenant_id AND gm.group_id=c.service_group_id AND gm.membership_id=p_member
 WHERE current.tenant_id=c.tenant_id AND current.contact_id=c.contact_id AND current.service_space_id=c.service_space_id AND current.service_group_id=c.service_group_id
 AND current.current_agent_id=p_member AND current.status IN('assigned','active') AND ca.membership_id=p_member AND ca.released_at IS NULL AND (ca.expires_at IS NULL OR ca.expires_at>statement_timestamp())
 ))
 ))))
$$;
COMMENT ON FUNCTION chat_conversation_visible(uuid,uuid,boolean,uuid) IS '统一咨询可见范围，管理员审计、访客本人及有效当前接待；共享旧咨询只扩展同客户服务空间业务组';

CREATE FUNCTION chat_message_visible(p_member uuid,p_contact uuid,p_admin boolean,p_message uuid) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
SELECT EXISTS(SELECT 1 FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.tenant_id=m.tenant_id WHERE m.id=p_message
 AND chat_conversation_visible(p_member,p_contact,p_admin,c.id)
 AND (m.audience='public' OR p_contact IS NULL)
 AND (p_contact IS NOT NULL OR p_admin OR chat_history_shared()
 OR NOT EXISTS(SELECT 1 FROM conversation_assignments ever WHERE ever.conversation_id=c.id AND ever.tenant_id=c.tenant_id)
 OR EXISTS(SELECT 1 FROM conversation_assignments own WHERE own.conversation_id=c.id AND own.tenant_id=c.tenant_id AND own.membership_id=p_member AND own.visibility_after_seq IS NOT NULL AND m.event_seq>own.visibility_after_seq AND (own.visibility_through_seq IS NULL OR m.event_seq<=own.visibility_through_seq))))
$$;
COMMENT ON FUNCTION chat_message_visible(uuid,uuid,boolean,uuid) IS '关闭历史分享时仅展示本人精确接待段内消息，首次未分配咨询保留排队消息，旧未知段不泄漏';

CREATE FUNCTION chat_event_visible(p_member uuid,p_contact uuid,p_admin boolean,p_event uuid) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
SELECT EXISTS(SELECT 1 FROM conversation_events e WHERE e.id=p_event
 AND chat_conversation_visible(p_member,p_contact,p_admin,e.conversation_id)
 AND (e.audience='public' OR p_contact IS NULL)
 AND CASE WHEN e.event_type IN('message.created','message.withdrawn') THEN chat_message_visible(p_member,p_contact,p_admin,nullif(e.payload->>'id','')::uuid) ELSE true END)
$$;
COMMENT ON FUNCTION chat_event_visible(uuid,uuid,boolean,uuid) IS '事件补拉与实时推送共享消息可见检查，禁止旧message.created载荷绕过历史策略';

CREATE FUNCTION chat_history_scope(p_member uuid,p_contact uuid,p_admin boolean,p_conversation uuid) RETURNS text LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
 SELECT CASE WHEN p_contact IS NOT NULL THEN 'visitor' WHEN p_admin THEN 'admin' WHEN chat_history_shared() THEN 'shared'
 ELSE 'private:'||md5(coalesce((SELECT string_agg(id::text||':'||coalesce(visibility_after_seq::text,'unknown')||':'||coalesce(visibility_through_seq::text,'open'),',' ORDER BY id) FROM conversation_assignments WHERE conversation_id=p_conversation AND membership_id=p_member),'')) END
$$;
COMMENT ON FUNCTION chat_history_scope(uuid,uuid,boolean,uuid) IS '历史策略及本人接待段变化指纹，用于使旧恢复游标失效和清理客户端不再可见的历史缓存';
