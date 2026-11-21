#!/usr/bin/env python3
"""文件作用说明：每分钟检查主分支，仅有服务代码变更时构建并健康切换GoodChat，复用现有数据库容器。"""
import argparse
import datetime
import fcntl
import json
import os
import re
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.request
import urllib.error
import urllib.parse
import xml.etree.ElementTree as ET


def publish_static(target, content):
    """同目录原子替换公开文件，相同内容保留修改时间，不暴露未写完的文件。"""
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o755)
    if target.exists() and target.read_bytes() == content:
        return
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=target.parent, prefix=".sitemap-", delete=False) as output:
            temporary = Path(output.name)
            output.write(content)
            output.flush()
            os.fsync(output.fileno())
        temporary.chmod(0o644)
        os.replace(temporary, target)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def public_origins(root):
    """读取本站运行配置中的主域名与允许的别名，不把私有环境内容输出到日志。"""
    values = dict(os.environ)
    config = root.parent / "private" / "app.env"
    if config.exists():
        for line in config.read_text().splitlines():
            if line and not line.lstrip().startswith("#") and "=" in line:
                key, value = line.split("=", 1)
                if key in {"SUPPORT_PUBLIC_URL", "SUPPORT_PUBLIC_ORIGINS"}:
                    values[key] = value.strip().strip("\"'")
    candidates = [values.get("SUPPORT_PUBLIC_URL", ""), *values.get("SUPPORT_PUBLIC_ORIGINS", "").split(",")]
    origins = []
    for value in candidates:
        url = urllib.parse.urlsplit(value.strip())
        if url.scheme == "https" and url.hostname and not url.username and not url.password and url.path in {"", "/"} and not url.query and not url.fragment:
            origin = f"{url.scheme}://{url.netloc}"
            if origin not in origins: origins.append(origin)
    if not origins:
        raise ValueError("未配置有效的本站公开域名")
    return origins


def refresh_sitemap(root):
    """按主域名及允许的别名分别校验和原子生成地图，避免海外或备用站点收录主站地址。"""
    origins = public_origins(root)
    for origin in origins:
        request = urllib.request.Request("http://127.0.0.1:13100/sitemap.xml", headers={"Accept-Encoding": "identity", "Host": urllib.parse.urlsplit(origin).netloc})
        with urllib.request.urlopen(request, timeout=15) as response:
            if response.status != 200:
                raise ValueError("地图生成失败")
            content = response.read(50_000_001)
        if len(content) > 50_000_000:
            raise ValueError("地图超过大小限制")
        namespace = "{http://www.sitemaps.org/schemas/sitemap/0.9}"
        tree = ET.fromstring(content)
        if tree.tag != namespace + "urlset":
            raise ValueError("地图根节点无效")
        urls = [row.findtext(namespace + "loc", "") for row in tree]
        if not 81 <= len(urls) <= 50000 or len(set(urls)) != len(urls):
            raise ValueError("地图目录不完整或重复")
        for address in urls:
            url = urllib.parse.urlsplit(address)
            if url.scheme != "https" or url.netloc != urllib.parse.urlsplit(origin).netloc or url.query or url.fragment:
                raise ValueError("地图包含非规范网址")
            if url.path.split("/")[1] not in {"zh-CN", "zh-TW", "en", "ja", "ko", "es", "pt-BR", "fr", "de"}:
                raise ValueError("地图包含非公开页面")
        target = root.parent / ".well-known"
        publish_static(target / f"sitemap-{urllib.parse.urlsplit(origin).hostname}.xml", content)
        if origin == origins[0]:
            publish_static(target / "sitemap.xml", content)
            publish_static(target / "sitemap-crawl-check-20261120.xml", content)


def refresh_indexnow_key(root):
    """同步已启用的公开验证键，停用后移除副本；临时错误不得覆盖有效键。"""
    target = root.parent / ".well-known" / "indexnow-key.txt"
    try:
        with urllib.request.urlopen("http://127.0.0.1:13100/indexnow-key.txt", timeout=10) as response:
            content = response.read(129)
            if response.status != 200 or not re.fullmatch(rb"[A-Za-z0-9-]{8,128}", content):
                raise ValueError("验证文件无效")
        publish_static(target, content)
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
        target.unlink(missing_ok=True)


def try_refresh_sitemap(root):
    """独立更新地图和公开验证键，失败仅记录类型且不阻塞应用发布。"""
    for update in (refresh_sitemap, refresh_indexnow_key):
        try:
            update(root)
        except Exception as error:
            print(f"公开文件更新失败，保留旧文件：{update.__name__} {type(error).__name__}", flush=True)


def run(args, root, capture=False, timeout=None):
    """执行固定参数数组，失败中止；不把环境文件展开到日志。"""
    return subprocess.run(args, cwd=root, check=True, text=True, timeout=timeout, stdout=subprocess.PIPE if capture else None).stdout


def healthy(port, attempts=60):
    """等待依赖已连通的真实健康检查，HTTP成功前不确认发布完成。"""
    for _ in range(attempts):
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/healthz", timeout=3) as response:
                if response.status == 200:
                    return True
        except Exception:
            pass
        time.sleep(2)
    return False


def exists(name):
    """只查询本项目明确命名的容器，禁止全局清理。"""
    return subprocess.run(["docker", "container", "inspect", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0


def start(root, private, image, name, port, code_port):
    """启动一个GoodChat应用，数据库与Redis只通过现有共享网络连接。"""
    run(["docker", "run", "-d", "--name", name, "--init", "--restart", "unless-stopped", "--network", "good-headhunter-data-services", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--env-file", str(private / "app.env"), "-v", f"{private / 'data'}:/data", "-p", f"127.0.0.1:{port}:3000", "-p", f"127.0.0.1:{code_port}:18081", "--log-opt", "max-size=10m", "--log-opt", "max-file=3", image], root)


def cleanup_images(root):
    """仅回收带GoodChat构建标签的中间层及无容器引用的旧应用，不触碰卷和其他项目。"""
    builds = run(["docker", "image", "ls", "-q", "--filter", "label=cn.goodchat.build-stage=true"], root, True).split()
    for image in set(builds):
        subprocess.run(["docker", "image", "rm", image], cwd=root, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    # docker image rm不加force，任何被正式、候选、回退或其他容器引用的镜像都会保留。
    images = run(["docker", "image", "ls", "goodchat", "--format", "{{.Repository}}:{{.Tag}}"], root, True).split()
    for image in images:
        if re.fullmatch(r"goodchat:[0-9a-f]{12}", image):
            used = run(["docker", "ps", "-aq", "--filter", "ancestor=" + image], root, True).strip()
            if not used:
                subprocess.run(["docker", "image", "rm", image], cwd=root, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def deploy(root, private, force):
    """快进主分支、构建候选、迁移并健康切换，按用户要求不自动备份。"""
    if run(["git", "status", "--porcelain"], root, True).strip():
        raise RuntimeError("服务器源码有未提交改动，拒绝覆盖")
    run(["git", "-c", "http.lowSpeedLimit=1024", "-c", "http.lowSpeedTime=30", "fetch", "origin", "main"], root, timeout=90)
    run(["git", "merge", "--ff-only", "origin/main"], root)
    commit = run(["git", "rev-parse", "HEAD"], root, True).strip()
    state_path = private / "release.json"
    previous = json.loads(state_path.read_text()) if state_path.exists() else {}
    if not force and previous.get("commit") == commit and exists("goodchat"):
        return
    if not force and previous.get("commit") and exists("goodchat"):
        changed = run(["git", "diff", "--name-only", previous["commit"], commit, "--", "backend", "frontend", "deploy", "Dockerfile", "desktop/frontend/src/drafts.ts"], root, True).strip()
        if not changed:
            previous["commit"] = commit
            state_path.write_text(json.dumps(previous))
            print("主分支更新不影响服务，跳过构建", flush=True)
            return
    for name in ("postgres", "redis"):
        if not exists(name):
            raise RuntimeError(f"缺少必须复用的现有容器：{name}；不会新建")
    image = "goodchat:" + commit[:12]
    print("构建候选", commit, flush=True)
    run(["docker", "build", "--force-rm", "--build-arg", "NEXT_PUBLIC_SITE_URL=" + public_origins(root)[0], "-t", image, "."], root)
    run(["docker", "run", "--rm", "--network", "good-headhunter-data-services", "--env-file", str(private / "app.env"), "--env-file", str(private / "migration.env"), "-v", f"{private / 'data'}:/data", image, "/app/migrate"], root)
    if exists("goodchat-candidate"):
        run(["docker", "rm", "-f", "goodchat-candidate"], root)
    try:
        start(root, private, image, "goodchat-candidate", 13101, 18182)
        if not healthy(13101):
            raise RuntimeError("候选健康检查失败，旧应用保持运行")
    finally:
        if exists("goodchat-candidate"):
            run(["docker", "rm", "-f", "goodchat-candidate"], root)
    if exists("goodchat-rollback"):
        run(["docker", "rm", "-f", "goodchat-rollback"], root)
    old = exists("goodchat")
    if old:
        run(["docker", "stop", "--time", "20", "goodchat"], root)
        run(["docker", "rename", "goodchat", "goodchat-rollback"], root)
    try:
        start(root, private, image, "goodchat", 13100, 18181)
        if not healthy(13100):
            raise RuntimeError("新应用检查失败，正在回退")
    except Exception:
        if exists("goodchat"):
            run(["docker", "rm", "-f", "goodchat"], root)
        if old:
            run(["docker", "rename", "goodchat-rollback", "goodchat"], root)
            run(["docker", "start", "goodchat"], root)
        raise
    state_path.write_text(json.dumps({"commit": commit, "image": image, "deployed_at": datetime.datetime.now().isoformat()}, indent=2))
    os.chmod(state_path, 0o600)
    print("部署成功", commit, flush=True)


def main():
    """使用文件锁防止一分钟任务与长构建重叠，凭据目录与源码分离。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--private", type=Path, default=Path(__file__).resolve().parents[2] / "private")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()
    args.private.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (args.private / "deploy.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print("上次构建尚未结束，本轮跳过", flush=True)
            return
        try:
            try_refresh_sitemap(args.root.resolve())
            deploy(args.root.resolve(), args.private.resolve(), args.force)
        finally:
            try_refresh_sitemap(args.root.resolve())
            cleanup_images(args.root.resolve())


if __name__ == "__main__":
    main()
