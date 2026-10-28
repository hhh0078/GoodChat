-- 文件作用说明：仅为升级前未知边界且仍有效的当前接待段记录升级水位，保守隐藏升级前历史，同时允许关闭分享后的新消息正常展示；不改写已知历史段。
DO $$ DECLARE tenant uuid; previous_context text:=current_setting('app.tenant_id',true); BEGIN
FOR tenant IN SELECT id FROM tenants LOOP
PERFORM set_config('app.tenant_id',tenant::text,true);
UPDATE conversation_assignments a SET visibility_after_seq=c.next_event_seq
FROM conversations c
WHERE a.tenant_id=tenant AND c.tenant_id=a.tenant_id AND c.id=a.conversation_id
 AND c.current_assignment_id=a.id AND c.current_agent_id=a.membership_id
 AND c.status IN('assigned','active') AND a.released_at IS NULL
 AND (a.expires_at IS NULL OR a.expires_at>statement_timestamp())
 AND a.visibility_after_seq IS NULL;
END LOOP;
PERFORM set_config('app.tenant_id',coalesce(previous_context,''),true);
END $$;
