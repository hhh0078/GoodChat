-- 文件作用说明：增加访客在线租约与按客户累计的幂等内部备注；旧会话内部消息保留原记录，由备注接口合并读取。
ALTER TABLE contacts ADD COLUMN online_until timestamptz NOT NULL DEFAULT '-infinity';
COMMENT ON COLUMN contacts.online_until IS '访客真实连接探活确认的在线租约截止时间，不代表客户身份已验证';

CREATE TABLE contact_notes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    contact_id uuid NOT NULL,
    author_id uuid NOT NULL,
    client_note_id uuid NOT NULL,
    content text NOT NULL CHECK(length(content) BETWEEN 1 AND 5000),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(tenant_id,contact_id,author_id,client_note_id),
    FOREIGN KEY(tenant_id,contact_id) REFERENCES contacts(tenant_id,id),
    FOREIGN KEY(tenant_id,author_id) REFERENCES tenant_memberships(tenant_id,id)
);
COMMENT ON TABLE contact_notes IS '仅授权客服和管理员可见的客户多条跟进备注，跨咨询累计';
COMMENT ON COLUMN contact_notes.id IS '内部备注记录编号';
COMMENT ON COLUMN contact_notes.tenant_id IS '所属租户编号';
COMMENT ON COLUMN contact_notes.contact_id IS '所属客户编号';
COMMENT ON COLUMN contact_notes.author_id IS '添加备注的租户成员编号';
COMMENT ON COLUMN contact_notes.client_note_id IS '客户端重试保持不变的幂等备注编号';
COMMENT ON COLUMN contact_notes.content IS '纯文本内部备注内容，不向访客发布';
COMMENT ON COLUMN contact_notes.created_at IS '备注首次成功保存时间';
CREATE INDEX contact_notes_history ON contact_notes(tenant_id,contact_id,created_at DESC,id DESC);
ALTER TABLE contact_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE contact_notes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON contact_notes
    USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
    WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
