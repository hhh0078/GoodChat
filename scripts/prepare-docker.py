"""文件作用说明：为唯一GoodChat应用容器生成私有环境，仅读取现有容器网络，不创建数据库或Redis容器。"""
import json
import os
import subprocess
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit

root = Path(__file__).resolve().parents[1]
private = root / ".local"
private.mkdir(mode=0o700, exist_ok=True)


def inspect(name):
    """读取指定现有容器元数据，不返回或打印环境密钥。"""
    return json.loads(subprocess.check_output(["docker", "inspect", name]))[0]


# 使用Docker Desktop稳定宿主入口，容器重启后不依赖变化的bridge IP。
for name in ("local_postgres", "jijie-redis"):
    if not inspect(name)["State"]["Running"]:
        raise RuntimeError(name + "尚未运行，请启动现有服务")
database = json.loads((private / "dev-database.json").read_text())
smtp = json.loads((private / "smtp.json").read_text())
key = (private / "auth-secret").read_text()
parts = urlsplit(database["database_url"])
authority = parts.netloc.rsplit("@", 1)[0] + "@host.docker.internal:5432"
database_url = urlunsplit((parts.scheme, authority, parts.path, parts.query, parts.fragment))
migration_parts = urlsplit(database["migration_url"])
migration_url = urlunsplit((migration_parts.scheme, migration_parts.netloc.rsplit("@", 1)[0] + "@host.docker.internal:5432", migration_parts.path, migration_parts.query, migration_parts.fragment))
values = {"GOODCHAT_UID": str(os.getuid()), "GOODCHAT_GID": str(os.getgid()), "DATABASE_URL": database_url, "MIGRATION_DATABASE_URL": migration_url, "SUPPORT_REDIS_URL": "redis://host.docker.internal:56379/0", "SUPPORT_PUBLIC_URL": "http://127.0.0.1:13000", "SUPPORT_CODE_ORIGIN": "http://localhost:18081", "SUPPORT_AUTH_SECRET": key, "SUPPORT_GATEWAY_SECRET": key, "APP_ENV": "development", "SUPPORT_SMTP_HOST": smtp["host"], "SUPPORT_SMTP_PORT": smtp.get("port", "587"), "SUPPORT_SMTP_USERNAME": smtp["username"], "SUPPORT_SMTP_PASSWORD": smtp["password"], "SUPPORT_SMTP_FROM": smtp["from"], "SUPPORT_SMTP_ALLOW_PLAIN": "false"}
ai_path = private / "ai.json"
ai = json.loads(ai_path.read_text()) if ai_path.exists() else {}
for name, field in {"SUPPORT_AI_ENDPOINT": "endpoint", "SUPPORT_AI_KEY": "key", "SUPPORT_AI_MODEL": "model", "SUPPORT_AI_DAILY_LIMIT": "daily_limit"}.items():
    value = os.environ.get(name) or ai.get(field)
    if value is not None:
        values[name] = str(value)
for value in values.values():
    if "\n" in value or "\r" in value:
        raise RuntimeError("环境值含不支持的换行")
target = private / "docker.env"
target.write_text("# 文件作用说明：本机Docker私有配置，禁止提交或打印。\n" + "\n".join(name + "='" + value.replace("'", "\\'") + "'" for name, value in values.items()) + "\n")
os.chmod(target, 0o600)
(private / "container-data").mkdir(mode=0o700, exist_ok=True)
print("Docker私有环境已准备，只配置现有PostgreSQL和Redis，未创建容器。")
