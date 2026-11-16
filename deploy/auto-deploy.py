#!/usr/bin/env python3
"""文件作用说明：每分钟检查主分支，仅有服务代码变更时构建并健康切换GoodChat，复用现有数据库容器。"""
import argparse
import datetime
import fcntl
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request


def run(args, root, capture=False):
    """执行固定参数数组，失败中止；不把环境文件展开到日志。"""
    return subprocess.run(args, cwd=root, check=True, text=True, stdout=subprocess.PIPE if capture else None).stdout


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


def deploy(root, private, force):
    """快进主分支、构建候选、迁移并健康切换，按用户要求不自动备份。"""
    if run(["git", "status", "--porcelain"], root, True).strip():
        raise RuntimeError("服务器源码有未提交改动，拒绝覆盖")
    run(["git", "fetch", "origin", "main"], root)
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
    run(["docker", "build", "--build-arg", "NEXT_PUBLIC_SITE_URL=https://goodchat.58it.cn", "-t", image, "."], root)
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
    parser.add_argument("--private", type=Path, default=Path("/www/wwwroot/goodchat.58it.cn/private"))
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()
    args.private.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (args.private / "deploy.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print("上次构建尚未结束，本轮跳过", flush=True)
            return
        deploy(args.root.resolve(), args.private.resolve(), args.force)


if __name__ == "__main__":
    main()
