-- 文件作用说明：保存租户受控公开头像素材和成员固定头像引用；访客随机池配置沿用chat_settings，既有客户头像不改写。
ALTER TABLE tenant_memberships ADD COLUMN avatar_ref text NOT NULL DEFAULT '' CHECK(avatar_ref='' OR avatar_ref ~ '^default-(0[1-9]|[1-4][0-9]|50)$' OR avatar_ref ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$');
COMMENT ON COLUMN tenant_memberships.avatar_ref IS '成员在本租户固定展示的头像引用，空值使用姓名首字，平台库编号或本租户上传素材UUID';

CREATE UNIQUE INDEX IF NOT EXISTS membership_tenant_identity ON tenant_memberships(tenant_id,id);
CREATE TABLE avatar_uploads(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 uploaded_by uuid NOT NULL,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 content_type text NOT NULL DEFAULT 'image/png' CHECK(content_type='image/png'),
 width integer NOT NULL CHECK(width BETWEEN 1 AND 256),
 height integer NOT NULL CHECK(height BETWEEN 1 AND 256),
 size_bytes integer NOT NULL CHECK(size_bytes BETWEEN 1 AND 262144),
 image_data bytea NOT NULL CHECK(octet_length(image_data)=size_bytes),
 sha256 text NOT NULL CHECK(length(sha256)=64),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,uploaded_by) REFERENCES tenant_memberships(tenant_id,id)
);
COMMENT ON TABLE avatar_uploads IS '租户成员上传并经实际解码重编码的公开头像图片，元数据按租户隔离，图片字节可通过随机UUID公开展示';
COMMENT ON COLUMN avatar_uploads.id IS '公开头像素材随机编号';
COMMENT ON COLUMN avatar_uploads.tenant_id IS '素材所属租户编号';
COMMENT ON COLUMN avatar_uploads.uploaded_by IS '上传素材的租户成员编号';
COMMENT ON COLUMN avatar_uploads.name IS '经过长度和路径清理的图片显示名称';
COMMENT ON COLUMN avatar_uploads.content_type IS '规范化输出媒体类型，固定PNG';
COMMENT ON COLUMN avatar_uploads.width IS '重编码后图片宽度像素';
COMMENT ON COLUMN avatar_uploads.height IS '重编码后图片高度像素';
COMMENT ON COLUMN avatar_uploads.size_bytes IS '规范化图片字节数，最多256KiB';
COMMENT ON COLUMN avatar_uploads.image_data IS '已清除源文件元数据和主动内容的PNG字节';
COMMENT ON COLUMN avatar_uploads.sha256 IS '规范化图片SHA256摘要，用于公开资源缓存校验';
COMMENT ON COLUMN avatar_uploads.created_at IS '素材首次保存时间';
CREATE INDEX avatar_uploads_tenant_created ON avatar_uploads(tenant_id,created_at DESC,id);
ALTER TABLE avatar_uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE avatar_uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON avatar_uploads USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);

CREATE FUNCTION public_avatar_image(p_id uuid) RETURNS TABLE(image_data bytea,sha256 text,created_at timestamptz)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,public AS $$
 SELECT a.image_data,a.sha256,a.created_at FROM public.avatar_uploads a WHERE a.id=p_id
$$;
COMMENT ON FUNCTION public_avatar_image(uuid) IS '按随机头像编号只读取已重编码的公开图片字节与缓存信息，不返回租户、上传者或任何客户资料';
