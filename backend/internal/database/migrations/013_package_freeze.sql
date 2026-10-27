-- 文件作用说明：补强已发布代码包不可变性，文件只能在包创建事务内按冻结清单填充，既有资源包禁止追加。
-- 旧包creation_xid为零且永不匹配新的事务编号；原有字节与发布引用不变。
ALTER TABLE decoration_code_packages ADD COLUMN creation_xid xid8 NOT NULL DEFAULT '0'::xid8;
ALTER TABLE decoration_code_packages ALTER COLUMN creation_xid SET DEFAULT pg_current_xact_id();
COMMENT ON COLUMN decoration_code_packages.creation_xid IS '创建资源包的数据库事务编号，仅允许同一事务填充文件，历史包为零';
CREATE FUNCTION decoration_package_file_insert() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  IF NOT EXISTS(SELECT 1 FROM decoration_code_packages p WHERE p.tenant_id=NEW.tenant_id AND p.id=NEW.package_id AND p.creation_xid=pg_current_xact_id()
   AND EXISTS(SELECT 1 FROM jsonb_array_elements(p.manifest) f WHERE f->>'path'=NEW.path AND f->>'content_type'=NEW.content_type AND f->>'sha256'=NEW.sha256 AND NEW.sha256=encode(sha256(NEW.content),'hex') AND (f->>'size')::bigint=octet_length(NEW.content))) THEN
   RAISE EXCEPTION '资源文件只能在创建事务内按固定清单写入' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END
$$;
COMMENT ON FUNCTION decoration_package_file_insert() IS '防止在资源包提交或发布后追加文件，写入时必须匹配固定清单';
CREATE TRIGGER immutable_decoration_file_insert BEFORE INSERT ON decoration_package_files FOR EACH ROW EXECUTE FUNCTION decoration_package_file_insert();
