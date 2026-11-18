-- 文件作用说明：新增九语言邮件模板、事务通知队列、订阅与时区静默配置，保留现有认证和留言任务。
CREATE TABLE mail_notification_preferences (
 user_id uuid PRIMARY KEY REFERENCES users(id),
 business_enabled boolean NOT NULL DEFAULT true,
 digest_enabled boolean NOT NULL DEFAULT false,
 updates_enabled boolean NOT NULL DEFAULT false,
 quiet_start integer NOT NULL DEFAULT 22 CHECK(quiet_start BETWEEN 0 AND 23),
 quiet_end integer NOT NULL DEFAULT 8 CHECK(quiet_end BETWEEN 0 AND 23),
 updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE mail_notification_preferences IS '个人通知订阅与本地静默时间';
COMMENT ON COLUMN mail_notification_preferences.user_id IS '账号编号';
COMMENT ON COLUMN mail_notification_preferences.business_enabled IS '是否接收一般业务通知';
COMMENT ON COLUMN mail_notification_preferences.digest_enabled IS '是否订阅每周服务摘要';
COMMENT ON COLUMN mail_notification_preferences.updates_enabled IS '是否订阅平台产品公告';
COMMENT ON COLUMN mail_notification_preferences.quiet_start IS '静默开始本地小时';
COMMENT ON COLUMN mail_notification_preferences.quiet_end IS '静默结束本地小时，相同表示关闭';
COMMENT ON COLUMN mail_notification_preferences.updated_at IS '偏好最后更新时间';
CREATE TABLE notification_settings (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
 recipient_ids jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(recipient_ids)='array'),
 low_micro bigint NOT NULL DEFAULT 5000000 CHECK(low_micro BETWEEN 0 AND 1000000000000),
 critical_micro bigint NOT NULL DEFAULT 1000000 CHECK(critical_micro BETWEEN 0 AND low_micro),
 handoff_minutes integer NOT NULL DEFAULT 5 CHECK(handoff_minutes BETWEEN 1 AND 1440),
 service_enabled boolean NOT NULL DEFAULT true,
 updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE notification_settings IS '租户通知规则与已验证成员收件范围';
COMMENT ON COLUMN notification_settings.tenant_id IS '租户编号';
COMMENT ON COLUMN notification_settings.recipient_ids IS '额外接收业务通知的本租户管理员账号编号';
COMMENT ON COLUMN notification_settings.low_micro IS '低余额阈值，整数微元';
COMMENT ON COLUMN notification_settings.critical_micro IS '严重低余额阈值，整数微元';
COMMENT ON COLUMN notification_settings.handoff_minutes IS '转人工未处理提醒分钟数';
COMMENT ON COLUMN notification_settings.service_enabled IS '是否启用转人工与持续故障提醒';
COMMENT ON COLUMN notification_settings.updated_at IS '配置更新时间';
CREATE TABLE notification_templates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 kind text NOT NULL,
 locale text NOT NULL,
 version integer NOT NULL CHECK(version>0),
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 120),
 body text NOT NULL CHECK(length(body) BETWEEN 1 AND 6000),
 button text NOT NULL CHECK(length(button)<=100),
 created_by uuid REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE notification_templates IS '平台九语言邮件模板不可变发布版本';
COMMENT ON COLUMN notification_templates.id IS '版本编号';
COMMENT ON COLUMN notification_templates.kind IS '受控业务模板代码';
COMMENT ON COLUMN notification_templates.locale IS '系统语言代码';
COMMENT ON COLUMN notification_templates.version IS '同类语言递增版本';
COMMENT ON COLUMN notification_templates.subject IS '受限变量主题';
COMMENT ON COLUMN notification_templates.body IS '受限变量纯文本正文';
COMMENT ON COLUMN notification_templates.button IS '操作按钮文字，目标地址由后端决定';
COMMENT ON COLUMN notification_templates.created_by IS '发布平台管理员';
COMMENT ON COLUMN notification_templates.created_at IS '发布时间';
CREATE TABLE notification_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid REFERENCES tenants(id),
 user_id uuid NOT NULL REFERENCES users(id),
 kind text NOT NULL,
 event_key text NOT NULL,
 params jsonb NOT NULL DEFAULT '{}',
 urgent boolean NOT NULL DEFAULT false,
 status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','running','accepted','failed','unknown','cancelled','suppressed')),
 due_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 lease_until timestamptz,
 failure_kind text NOT NULL DEFAULT '',
 locale text NOT NULL DEFAULT '',
 timezone text NOT NULL DEFAULT '',
 template_version integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz
);
COMMENT ON TABLE notification_jobs IS '业务邮件持久任务与隐私最小化通知历史';
COMMENT ON COLUMN notification_jobs.id IS '通知编号';
COMMENT ON COLUMN notification_jobs.tenant_id IS '所属租户；账号安全可为空';
COMMENT ON COLUMN notification_jobs.user_id IS '目标账号编号';
COMMENT ON COLUMN notification_jobs.kind IS '通知模板代码';
COMMENT ON COLUMN notification_jobs.event_key IS '服务端业务去重标识';
COMMENT ON COLUMN notification_jobs.params IS '不包含验证码或客户正文的受控业务参数';
COMMENT ON COLUMN notification_jobs.urgent IS '紧急安全或服务中断通知，跳过静默';
COMMENT ON COLUMN notification_jobs.status IS '排队、执行、SMTP受理、失败、不确定、取消或抑制';
COMMENT ON COLUMN notification_jobs.due_at IS '下一次允许执行UTC时间';
COMMENT ON COLUMN notification_jobs.attempts IS '已执行次数';
COMMENT ON COLUMN notification_jobs.lease_until IS '工作器租约截止时间';
COMMENT ON COLUMN notification_jobs.failure_kind IS '脱敏错误分类';
COMMENT ON COLUMN notification_jobs.locale IS '发送时使用的收件人语言';
COMMENT ON COLUMN notification_jobs.timezone IS '发送时使用的IANA时区';
COMMENT ON COLUMN notification_jobs.template_version IS '实际发布模板版本，零为内置版';
COMMENT ON COLUMN notification_jobs.created_at IS '事件入队时间';
COMMENT ON COLUMN notification_jobs.finished_at IS '最终状态时间';
CREATE TABLE notification_attempts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 job_id uuid NOT NULL REFERENCES notification_jobs(id),
 attempt integer NOT NULL,
 status text NOT NULL,
 failure_kind text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE notification_attempts IS '每次SMTP受理结果审计，不保存敏感邮件载荷';
COMMENT ON COLUMN notification_attempts.id IS '记录编号';
COMMENT ON COLUMN notification_attempts.job_id IS '所属通知';
COMMENT ON COLUMN notification_attempts.attempt IS '执行次数';
COMMENT ON COLUMN notification_attempts.status IS '本次受理或失败分类状态';
COMMENT ON COLUMN notification_attempts.failure_kind IS '安全错误分类';
COMMENT ON COLUMN notification_attempts.created_at IS '执行结果记录时间';
CREATE TABLE notification_state (
 key text PRIMARY KEY,
 value jsonb NOT NULL DEFAULT '{}',
 updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE notification_state IS '通知调度扫描起点与余额边界状态';
COMMENT ON COLUMN notification_state.key IS '服务端状态键';
COMMENT ON COLUMN notification_state.value IS '扫描状态值';
COMMENT ON COLUMN notification_state.updated_at IS '状态更新时间';
CREATE UNIQUE INDEX notification_template_version ON notification_templates(kind,locale,version);
CREATE UNIQUE INDEX notification_job_event ON notification_jobs(user_id,kind,event_key);
CREATE UNIQUE INDEX notification_attempt_unique ON notification_attempts(job_id,attempt);
CREATE INDEX notification_due ON notification_jobs(due_at) WHERE status='queued';
INSERT INTO notification_state(key,value) VALUES('installed',jsonb_build_object('at',now()));
ALTER TABLE mail_notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE mail_notification_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY notice_access ON mail_notification_preferences USING(user_id=NULLIF(current_setting('app.notice_user',true),'')::uuid OR current_setting('app.notice_worker',true)='on') WITH CHECK(user_id=NULLIF(current_setting('app.notice_user',true),'')::uuid OR current_setting('app.notice_worker',true)='on');
ALTER TABLE notification_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY notice_access ON notification_settings USING(tenant_id=current_tenant_id() OR current_setting('app.notice_worker',true)='on') WITH CHECK(tenant_id=current_tenant_id() OR current_setting('app.notice_worker',true)='on');
ALTER TABLE notification_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY notice_access ON notification_jobs USING(tenant_id=current_tenant_id() OR user_id=NULLIF(current_setting('app.notice_user',true),'')::uuid OR current_setting('app.notice_worker',true)='on') WITH CHECK(tenant_id=current_tenant_id() OR user_id=NULLIF(current_setting('app.notice_user',true),'')::uuid OR current_setting('app.notice_worker',true)='on');
ALTER TABLE notification_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_attempts FORCE ROW LEVEL SECURITY;
CREATE POLICY notice_access ON notification_attempts USING(EXISTS(SELECT 1 FROM notification_jobs j WHERE j.id=job_id) OR current_setting('app.notice_worker',true)='on') WITH CHECK(EXISTS(SELECT 1 FROM notification_jobs j WHERE j.id=job_id) OR current_setting('app.notice_worker',true)='on');

-- 已发布模板保持不可变，回滚只能再发布新版本。
CREATE FUNCTION notification_template_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION '已发布邮件模板不可修改，请发布新版本'; END $$;
COMMENT ON FUNCTION notification_template_immutable() IS '保护邮件模板历史版本及失败重试冻结内容';
CREATE TRIGGER notification_template_immutable BEFORE UPDATE OR DELETE ON notification_templates FOR EACH ROW EXECUTE FUNCTION notification_template_immutable();
