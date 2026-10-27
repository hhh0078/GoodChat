-- 文件作用说明：建立访客主动共享的留言附件和默认关闭的可靠邮件提醒队列，不改变聊天撤回历史。
ALTER TABLE offline_leads ADD CONSTRAINT offline_leads_tenant_id UNIQUE(tenant_id,id);
CREATE TABLE offline_lead_attachments(
    tenant_id uuid NOT NULL,
    lead_id uuid NOT NULL,
    attachment_id uuid NOT NULL,
    position integer NOT NULL CHECK(position BETWEEN 0 AND 2),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(tenant_id,lead_id,attachment_id),
    UNIQUE(tenant_id,lead_id,position),
    FOREIGN KEY(tenant_id,lead_id) REFERENCES offline_leads(tenant_id,id),
    FOREIGN KEY(tenant_id,attachment_id) REFERENCES attachments(tenant_id,id)
);
CREATE TABLE lead_notification_settings(
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    enabled boolean NOT NULL DEFAULT false,
    email text NOT NULL DEFAULT '',
    version integer NOT NULL DEFAULT 1 CHECK(version>0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE lead_notification_jobs(
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    lead_id uuid NOT NULL,
    recipient text NOT NULL,
    settings_version integer NOT NULL,
    subject text NOT NULL,
    body text NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','running','accepted','failed','unknown')),
    attempts integer NOT NULL DEFAULT 0,
    lease_until timestamptz,
    started_at timestamptz,
    finished_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    history jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id,lead_id),
    FOREIGN KEY(tenant_id,lead_id) REFERENCES offline_leads(tenant_id,id)
);
CREATE INDEX lead_notification_pending ON lead_notification_jobs(tenant_id,created_at) WHERE status='queued';
DO $$ DECLARE t text; a record; d jsonb := '{"id":"任务主键编号","tenant_id":"所属租户编号","lead_id":"关联访客主动提交的留言编号","attachment_id":"访客主动共享给留言处理人的内部附件编号","position":"留言内附件显示顺序，零至二","created_at":"创建时间","enabled":"是否允许为新留言建立邮件提醒","email":"管理员主动配置的收件邮箱","version":"配置并发保护版本","updated_at":"最近配置更新时间","recipient":"本次通知创建时冻结的收件邮箱","settings_version":"领取任务时须重查的配置版本","subject":"仅含待处理提醒的邮件标题","body":"仅包含待处理提醒留言编号与官网登录入口的正文","status":"排队执行已受理失败或结果不确定状态","attempts":"持久领取世代及尝试次数","lease_until":"当前发送尝试租约，过期保守标为结果不确定","started_at":"最近领取时间","finished_at":"最近受理失败或不确定判定时间","last_error":"不含SMTP凭证与客户正文的安全分类","history":"逐次发送结果的不可覆盖追加记录"}'::jsonb;
BEGIN
FOREACH t IN ARRAY ARRAY['offline_lead_attachments','lead_notification_settings','lead_notification_jobs'] LOOP
EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t); EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = nullif(current_setting(''app.tenant_id'',true),'''')::uuid) WITH CHECK (tenant_id = nullif(current_setting(''app.tenant_id'',true),'''')::uuid)',t);
FOR a IN SELECT attname FROM pg_attribute WHERE attrelid=('public.'||t)::regclass AND attnum>0 AND NOT attisdropped LOOP
IF NOT d ? a.attname THEN RAISE EXCEPTION '字段缺少中文注释：%.%',t,a.attname; END IF;
EXECUTE format('COMMENT ON COLUMN %I.%I IS %L',t,a.attname,d->>a.attname);
END LOOP;
END LOOP;
END $$;
COMMENT ON TABLE offline_lead_attachments IS '独立于聊天撤回的访客主动留言附件共享';
COMMENT ON TABLE lead_notification_settings IS '默认关闭且由租户管理员配置的留言提醒邮箱';
COMMENT ON TABLE lead_notification_jobs IS '保留SMTP真实受理分类与租约世代的持久通知任务';
