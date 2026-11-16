-- 文件作用说明：保存平台发布的客户端下载和强制更新版本；不修改租户消息数据。
CREATE TABLE client_releases (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 version text NOT NULL UNIQUE,
 major integer NOT NULL CHECK(major>=0),
 minor integer NOT NULL CHECK(minor>=0),
 patch integer NOT NULL CHECK(patch>=0),
 description text NOT NULL DEFAULT '',
 win_url text NOT NULL DEFAULT '',
 mac_url text NOT NULL DEFAULT '',
 apk_url text NOT NULL DEFAULT '',
 published boolean NOT NULL DEFAULT false,
 created_by uuid NOT NULL REFERENCES users(id),
 updated_by uuid NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE client_releases IS '平台统一客户端版本与各系统下载地址';
COMMENT ON COLUMN client_releases.id IS '客户端发布唯一标识';
COMMENT ON COLUMN client_releases.version IS '三段数字语义版本号';
COMMENT ON COLUMN client_releases.major IS '用于数字排序的主版本';
COMMENT ON COLUMN client_releases.minor IS '用于数字排序的次版本';
COMMENT ON COLUMN client_releases.patch IS '用于数字排序的修订版本';
COMMENT ON COLUMN client_releases.description IS '向用户展示的更新说明';
COMMENT ON COLUMN client_releases.win_url IS 'Windows客户端下载HTTPS地址';
COMMENT ON COLUMN client_releases.mac_url IS 'Mac客户端下载HTTPS地址';
COMMENT ON COLUMN client_releases.apk_url IS 'Android APK下载地址，未开发时为空';
COMMENT ON COLUMN client_releases.published IS '是否公开发布并参与更新检查';
COMMENT ON COLUMN client_releases.created_by IS '创建版本的平台管理员';
COMMENT ON COLUMN client_releases.updated_by IS '最后编辑的平台管理员';
COMMENT ON COLUMN client_releases.created_at IS '创建时间';
COMMENT ON COLUMN client_releases.updated_at IS '最后编辑时间';
CREATE INDEX client_releases_latest ON client_releases(major DESC,minor DESC,patch DESC) WHERE published;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goodchat_saas_app') THEN GRANT SELECT,INSERT,UPDATE ON client_releases TO goodchat_saas_app; END IF; END $$;
