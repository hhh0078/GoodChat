"""文件作用说明：暂停唯一GoodChat应用后备份现有PostgreSQL独立库、私有附件与身份密钥，完成后恢复应用，不创建容器。"""
import argparse
import hashlib
import json
import os
import subprocess
import tarfile
from datetime import datetime
from pathlib import Path
from urllib.parse import unquote, urlsplit
from zoneinfo import ZoneInfo

ROOT = Path(__file__).resolve().parents[1]


def run(arguments, **options):
    """执行明确的Docker维护命令，失败时保留备份文件且不打印私有配置。"""
    return subprocess.run(arguments, check=True, **options)


def digest(path):
    """分块计算备份校验值，用于复制后检查完整性。"""
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def main():
    """显式静默业务写入后生成一致备份集；只验证归档可读，不冒称恢复演练。"""
    parser = argparse.ArgumentParser(description="备份GoodChat独立数据库与附件")
    parser.add_argument("--quiesce", action="store_true", help="允许短暂停止GoodChat应用以保持附件与数据库一致")
    arguments = parser.parse_args()
    if not arguments.quiesce:
        parser.error("请使用 --quiesce 明确允许短暂停止GoodChat应用；不会停止数据库或Redis")
    configuration = json.loads((ROOT / ".local/dev-database.json").read_text())
    database = unquote(urlsplit(configuration["database_url"]).path.lstrip("/"))
    if database != "goodchat_saas":
        raise RuntimeError("该本机备份脚本只允许GoodChat独立库goodchat_saas")
    key = ROOT / ".local/auth-secret"
    data = ROOT / ".local/container-data"
    if not key.is_file() or not data.is_dir():
        raise RuntimeError("应用密钥或持久目录不完整，拒绝生成缺项备份")
    target = ROOT / ".local/backups" / datetime.now(ZoneInfo("Asia/Shanghai")).strftime("%Y%m%d-%H%M%S")
    target.mkdir(mode=0o700, parents=True, exist_ok=False)
    state = subprocess.check_output(["docker", "inspect", "--format", "{{.State.Running}}", "goodchat"], text=True).strip()
    stopped = state == "true"
    try:
        if stopped:
            run(["docker", "stop", "--time", "15", "goodchat"], stdout=subprocess.DEVNULL)
        database_file = target / "database.dump"
        with os.fdopen(os.open(database_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as output:
            run(["docker", "exec", "local_postgres", "pg_dump", "-U", "postgres", "-d", database, "--format=custom"], stdout=output)
        with database_file.open("rb") as source:
            run(["docker", "exec", "-i", "local_postgres", "pg_restore", "--list"], stdin=source, stdout=subprocess.DEVNULL)
        files = target / "files.tar.gz"
        with os.fdopen(os.open(files, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as output:
            with tarfile.open(fileobj=output, mode="w:gz") as archive:
                archive.add(data, arcname="container-data")
                archive.add(key, arcname="auth-secret")
        metadata = {"database": database, "application_quiesced": stopped, "archive_readable": True, "restore_tested": False, "sha256": {path.name: digest(path) for path in [database_file, files]}}
        metadata_file = target / "metadata.json"
        with os.fdopen(os.open(metadata_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as output:
            json.dump(metadata, output, ensure_ascii=False, indent=2)
    finally:
        if stopped:
            run(["docker", "start", "goodchat"], stdout=subprocess.DEVNULL)
    print("备份集已生成且数据库归档可读，应用已恢复；尚未执行恢复演练。")
    print(target)


if __name__ == "__main__":
    main()
