-- 文件作用说明：为平台公共模板及版本快照保存已发布译文，不修改租户私有正文和历史设计。
ALTER TABLE platform_templates ADD COLUMN translations jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(translations)='object');
COMMENT ON COLUMN platform_templates.translations IS '平台管理员发布的各语言模板名称与说明，不参与身份或租户授权';
ALTER TABLE platform_template_versions ADD COLUMN translations jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(translations)='object');
COMMENT ON COLUMN platform_template_versions.translations IS '本次平台模板版本冻结的名称与说明译文，后续编辑不改写旧版本';
