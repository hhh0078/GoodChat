-- 文件作用说明：新增链接级接待方式与多渠道标签，兼容原分配池规则和单主渠道统计口径。
ALTER TABLE access_links ADD COLUMN reception_mode text NOT NULL DEFAULT 'inherit' CHECK (reception_mode IN ('inherit','auto','manual'));
ALTER TABLE access_links ADD COLUMN channel_tags jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(channel_tags)='array');
COMMENT ON COLUMN access_links.reception_mode IS '链接接待方式：继承池规则、自动分配或客服自行领取';
COMMENT ON COLUMN access_links.channel_tags IS '入口渠道标签数组，主渠道仍使用channel_code供历史筛选统计';
ALTER TABLE access_links ALTER COLUMN allow_routing_override SET DEFAULT true;
-- 显式逐租户回填，兼容迁移角色不具备RLS绕过权限的部署。
DO $$ DECLARE current_id uuid; previous_context text := current_setting('app.tenant_id',true); BEGIN
FOR current_id IN SELECT id FROM tenants LOOP
PERFORM set_config('app.tenant_id',current_id::text,true);
UPDATE access_links SET allow_routing_override=true;
UPDATE access_links SET channel_tags=jsonb_build_array(channel_code) WHERE channel_code<>'';
END LOOP;
PERFORM set_config('app.tenant_id',coalesce(previous_context,''),true);
END $$;
