"""文件作用说明：在现有PostgreSQL容器的临时数据库恢复本项目备份，核对附件及密钥完整性后删除本次临时库，不修改运行库或创建容器。"""
import argparse
import hashlib
import json
import re
import subprocess
import tarfile
import uuid
from datetime import datetime, timezone
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parents[1]
PREFIX = "goodchat_restore_check_"


def digest(stream):
    """分块计算文件流摘要，不把附件或身份密钥输出到终端。"""
    value = hashlib.sha256()
    for block in iter(lambda: stream.read(1024 * 1024), b""):
        value.update(block)
    return value.hexdigest()


def temporary_name(value):
    """仅接受本工具生成的临时库名，禁止清理业务库或注入SQL标识符。"""
    if not re.fullmatch(PREFIX + r"[0-9a-f]{24}", value):
        raise ValueError("只允许本次隔离恢复库名")
    return value


def sql(database, statement):
    """通过已有容器的本地维护连接执行固定SQL，不读取或输出连接密码。"""
    result = subprocess.run(
        ["docker", "exec", "local_postgres", "psql", "-U", "postgres", "-d", database,
         "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", statement],
        capture_output=True, text=True,
    )
    if result.returncode:
        raise RuntimeError("恢复核验数据库操作失败，运行库未被修改")
    return result.stdout.strip()


def archive_files(path):
    """只读取归档内普通文件的摘要，拒绝重复路径、链接或越界路径且不解包执行。"""
    files = {}
    with tarfile.open(path, "r:gz") as archive:
        for member in archive:
            parts = PurePosixPath(member.name)
            if parts.is_absolute() or ".." in parts.parts or "\\" in member.name:
                raise ValueError("备份文件路径不安全")
            if member.isdir():
                continue
            if not member.isfile() or member.name in files:
                raise ValueError("备份包含链接、特殊文件或重复路径")
            if member.name != "auth-secret" and not member.name.startswith("container-data/"):
                raise ValueError("备份包含未知文件范围")
            source = archive.extractfile(member)
            if source is None:
                raise ValueError("备份文件无法读取")
            with source:
                files[member.name] = {"size": member.size, "sha256": digest(source)}
    if files.get("auth-secret", {}).get("size", 0) < 32:
        raise ValueError("备份缺少有效身份密钥")
    return files


def main():
    """恢复到新建临时库并检查附件，始终只清理本次成功创建的库且保留核验记录。"""
    parser = argparse.ArgumentParser(description="隔离恢复并核对GoodChat备份")
    parser.add_argument("backup", help=".local/backups中的备份目录名称")
    parser.add_argument("--allow-temporary-database", action="store_true", help="允许在现有PG容器创建并清理临时恢复库")
    args = parser.parse_args()
    if not args.allow_temporary_database:
        parser.error("请显式使用 --allow-temporary-database；不会覆盖goodchat_saas或新增容器")
    base = (ROOT / ".local/backups").resolve()
    target = (base / args.backup).resolve()
    if target.parent != base or not target.is_dir():
        parser.error("只允许本项目私有backups目录中的一级备份目录")
    metadata = json.loads((target / "metadata.json").read_text())
    if metadata.get("database") != "goodchat_saas" or not metadata.get("application_quiesced"):
        raise ValueError("必须使用GoodChat静默写入后生成的一致备份集")
    for name in ["database.dump", "files.tar.gz"]:
        with (target / name).open("rb") as source:
            if digest(source) != metadata.get("sha256", {}).get(name):
                raise ValueError("备份校验值不一致，停止恢复")
    files = archive_files(target / "files.tar.gz")
    database = temporary_name(PREFIX + uuid.uuid4().hex[:24])
    created = False
    try:
        sql("postgres", 'CREATE DATABASE "' + database + '" TEMPLATE template0')
        created = True
        with (target / "database.dump").open("rb") as source:
            result = subprocess.run(
                ["docker", "exec", "-i", "local_postgres", "pg_restore", "-U", "postgres",
                 "--exit-on-error", "-d", database], stdin=source, capture_output=True,
            )
        if result.returncode:
            raise RuntimeError("隔离数据库恢复失败，运行库与备份保持不变")
        rows = json.loads(sql(database, "SELECT coalesce(json_agg(json_build_object('key',storage_key,'sha256',sha256,'size',size_bytes)),'[]'::json) FROM public.attachments"))
        for row in rows:
            if not re.fullmatch(r"[0-9a-f-]{36}", row["key"]):
                raise ValueError("恢复数据存在不安全的附件存储键")
            item = files.get("container-data/chat-attachments/" + row["key"])
            if not item or item["sha256"] != row["sha256"] or item["size"] != row["size"]:
                raise ValueError("恢复数据库引用的附件缺失或内容不一致")
        table_count = int(sql(database, "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'"))
        tenant_count = int(sql(database, "SELECT count(*) FROM public.tenants"))
        report = {"verified_at": datetime.now(timezone.utc).isoformat(), "database_restore": True,
                  "temporary_database_only": True, "table_count": table_count, "tenant_count": tenant_count,
                  "attachments_verified": len(rows), "auth_key_archive_verified": True,
                  "restored_application_started": False, "temporary_database_removed": False}
    finally:
        if created:
            temporary_name(database)
            sql("postgres", 'DROP DATABASE "' + database + '" WITH (FORCE)')
    report["temporary_database_removed"] = True
    output = target / "restore-verification.json"
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    output.chmod(0o600)
    print(json.dumps(report, ensure_ascii=False))
    print("隔离数据库恢复及附件/密钥归档核对通过，临时库已清理；未启动恢复后的应用或执行生产切换。")


if __name__ == "__main__":
    main()
