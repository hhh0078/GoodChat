#!/usr/bin/env python3
"""文件作用说明：从同一份代码构建Mac应用及DMG、Windows免安装ZIP，注入版本与默认服务器。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import struct
import subprocess
import tempfile
import urllib.parse
import zipfile

ROOT = Path(__file__).resolve().parents[2]
DESKTOP = ROOT / "desktop"
WEB = ROOT / "frontend"


def run(args, cwd=DESKTOP, env=None):
    """执行参数数组并在失败时停止，版本和地址永不作为Shell代码执行。"""
    subprocess.run([str(arg) for arg in args], cwd=cwd, env=env, check=True)


def validate_version(value):
    """仅接受可同时写入Mac和Windows元数据的三段正式版本号。"""
    if not re.fullmatch(r"\d{1,5}\.\d{1,5}\.\d{1,5}", value) or any(int(part) > 65535 for part in value.split(".")):
        raise argparse.ArgumentTypeError("版本号使用三段数字，例如 1.0.0，每段不超过65535")
    return ".".join(str(int(part)) for part in value.split("."))


def validate_server(value):
    """校验打包默认地址，不接受认证信息、页面路径、查询串或不安全远端HTTP。"""
    value = value.strip()
    parsed = urllib.parse.urlsplit(value)
    local = parsed.hostname in ("localhost", "127.0.0.1", "::1")
    if (not parsed.hostname or parsed.username is not None or parsed.password is not None
            or parsed.path not in ("", "/") or parsed.query or parsed.fragment
            or any(char.isspace() or char in '\\"\'' for char in value)
            or "?" in value or "#" in value
            or (parsed.scheme != "https" and not (parsed.scheme == "http" and local))):
        raise argparse.ArgumentTypeError("默认服务器应为HTTPS根地址，HTTP仅允许本机调试")
    try:
        parsed.port
    except ValueError as error:
        raise argparse.ArgumentTypeError("服务器端口无效") from error
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc.lower(), "", "", ""))


def build_frontend():
    """复用锁定的前端依赖并检查类型，附带必要头像与提示音静态资源。"""
    npm = "npm.cmd" if os.name == "nt" else "npm"
    registry = subprocess.check_output([npm, "config", "get", "registry"], cwd=WEB, text=True).strip()
    proxy = subprocess.check_output(["go", "env", "GOPROXY"], text=True).strip()
    print(f"npm registry: {registry}\nGOPROXY: {proxy}", flush=True)
    if not (WEB / "node_modules" / "vite").exists():
        raise SystemExit("请先检查国内镜像，再在frontend目录运行 npm ci")
    run([npm, "run", "desktop:typecheck"], cwd=WEB)
    run([npm, "run", "desktop:build"], cwd=WEB)
    public = WEB / "public"
    for source, destination in [(public / "sounds", DESKTOP / "frontend/dist/sounds"),
                                (public / "images/avatars", DESKTOP / "frontend/dist/images/avatars")]:
        shutil.copytree(source, destination, dirs_exist_ok=True)
    shutil.copy2(public / "favicon.svg", DESKTOP / "frontend/dist/favicon.svg")


def compile_binary(target_os, arch, output, version, server):
    """按目标架构编译客户端，Windows不依赖本机C编译器。"""
    env = os.environ.copy()
    env.update(GOOS=target_os, GOARCH=arch, CGO_ENABLED="1" if target_os == "darwin" else "0")
    if target_os == "darwin":
        env["MACOSX_DEPLOYMENT_TARGET"] = "12.0"
        env["CGO_CFLAGS"] = env.get("CGO_CFLAGS", "") + " -mmacosx-version-min=12.0"
        env["CGO_LDFLAGS"] = env.get("CGO_LDFLAGS", "") + " -mmacosx-version-min=12.0"
    flags = f"-s -w -X main.version={version} -X main.defaultServer={server}"
    if target_os == "windows":
        flags += " -H windowsgui"
    run(["go", "build", "-trimpath", "-tags", "production", "-ldflags", flags, "-o", output, "."], env=env)


def mac_package(target, output, version, server, signing_identity, notary_profile):
    """创建Mac应用、图标和磁盘映像；没有开发者身份时明确生成临时签名测试包。"""
    if platform.system() != "Darwin":
        raise SystemExit("Mac打包必须在macOS运行，可使用仓库的macOS构建任务")
    app = output / "GoodChat.app"
    contents = app / "Contents"
    (contents / "MacOS").mkdir(parents=True)
    (contents / "Resources").mkdir()
    arches = ["arm64", "amd64"] if target == "mac-universal" else [target.removeprefix("mac-")]
    binaries = []
    for arch in arches:
        binary = output / f"GoodChat-{arch}"
        compile_binary("darwin", arch, binary, version, server)
        binaries.append(binary)
    executable = contents / "MacOS/GoodChat"
    if len(binaries) == 2:
        run(["lipo", "-create", *binaries, "-output", executable])
    else:
        shutil.copy2(binaries[0], executable)
    for binary in binaries:
        binary.unlink()
    iconset = output / "GoodChat.iconset"
    iconset.mkdir()
    for size in (16, 32, 128, 256, 512):
        for scale in (1, 2):
            suffix = "@2x" if scale == 2 else ""
            run(["sips", "-s", "format", "png", "-z", str(size * scale), str(size * scale), WEB / "public/favicon.svg", "--out", iconset / f"icon_{size}x{size}{suffix}.png"])
    run(["iconutil", "-c", "icns", iconset, "-o", contents / "Resources/GoodChat.icns"])
    shutil.rmtree(iconset)
    info = {"CFBundleName": "GoodChat", "CFBundleDisplayName": "GoodChat", "CFBundleExecutable": "GoodChat",
            "CFBundleIdentifier": "cn.58it.goodchat.desktop", "CFBundlePackageType": "APPL", "CFBundleIconFile": "GoodChat.icns",
            "CFBundleShortVersionString": version, "CFBundleVersion": version, "LSMinimumSystemVersion": "12.0",
            "NSHighResolutionCapable": True, "NSMicrophoneUsageDescription": "用于录制并发送客服语音消息。",
            "NSAppTransportSecurity": {"NSAllowsLocalNetworking": True}}
    with (contents / "Info.plist").open("wb") as file:
        plistlib.dump(info, file)
    run(["codesign", "--force", "--options", "runtime", "--sign", signing_identity or "-", app])
    run(["codesign", "--verify", "--deep", "--strict", app])
    os.symlink("/Applications", output / "Applications")
    image = output.parent / f"GoodChat-{version}-{target}.dmg"
    run(["hdiutil", "create", "-volname", f"GoodChat {version}", "-srcfolder", output, "-ov", "-format", "UDZO", image])
    if notary_profile:
        if not signing_identity:
            raise SystemExit("公证需要指定Developer ID签名身份")
        run(["xcrun", "notarytool", "submit", image, "--keychain-profile", notary_profile, "--wait"])
        run(["xcrun", "stapler", "staple", image])
    return image


def windows_package(target, output, version, server):
    """生成带系统版本信息的Windows免安装包，WebView2运行时按系统共享使用。"""
    arch = target.removeprefix("win-")
    icon_data = (DESKTOP / "icon.png").read_bytes()
    icon = output / "GoodChat.ico"
    icon.write_bytes(struct.pack("<HHH", 0, 1, 1) + struct.pack("<BBBBHHII", 64, 64, 0, 0, 1, 32, len(icon_data), 22) + icon_data)
    resource = DESKTOP / f"rsrc_windows_{arch}.syso"
    try:
        run(["go", "run", "./cmd/resources", "-arch", arch, "-version", version, "-icon", icon, "-out", resource])
        compile_binary("windows", arch, output / "GoodChat.exe", version, server)
    finally:
        resource.unlink(missing_ok=True)
    icon.unlink()
    (output / "使用说明.txt").write_text(
        "GoodChat Windows免安装版\n解压后运行 GoodChat.exe。关闭窗口会保留在系统托盘；托盘菜单可彻底退出。\n"
        "支持Windows 10/11，需安装Microsoft Edge WebView2 Runtime。\n"
        "如果系统缺少WebView2，请通过微软官方下载，程序不会通过第三方镜像自动安装运行时：\n"
        "https://developer.microsoft.com/microsoft-edge/webview2/\n"
        "此构建未自动进行Windows代码签名；公开分发前请由发布者完成签名。\n", encoding="utf-8-sig")
    archive = output.parent / f"GoodChat-{version}-{target}.zip"
    with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as file:
        for path in output.rglob("*"):
            if path.is_file():
                file.write(path, path.relative_to(output))
    return archive


def main():
    """解析统一打包参数，每个产物附带版本、默认地址及SHA256说明。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", choices=["mac-arm64", "mac-amd64", "mac-universal", "win-amd64", "win-arm64"], required=True)
    parser.add_argument("--version", type=validate_version, required=True)
    parser.add_argument("--server", type=validate_server, default="https://goodchat.58it.cn")
    parser.add_argument("--skip-frontend", action="store_true", help="复用已构建的桌面前端")
    parser.add_argument("--signing-identity", default=os.environ.get("GOODCHAT_SIGN_IDENTITY", ""))
    parser.add_argument("--notary-profile", default="")
    args = parser.parse_args()
    if not args.skip_frontend:
        build_frontend()
    if not (DESKTOP / "frontend/dist/index.html").exists():
        raise SystemExit("缺少桌面前端，请先运行不带--skip-frontend的构建")
    dist = DESKTOP / "dist"
    dist.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="goodchat-package-", dir=dist) as temporary:
        output = Path(temporary)
        if args.target.startswith("mac-"):
            artifact = mac_package(args.target, output, args.version, args.server, args.signing_identity, args.notary_profile)
        else:
            artifact = windows_package(args.target, output, args.version, args.server)
        digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
        metadata = {"version": args.version, "default_server": args.server, "target": args.target,
                    "artifact": artifact.name, "bytes": artifact.stat().st_size, "sha256": digest,
                    "signing": "developer-id" if args.target.startswith("mac-") and args.signing_identity else "ad-hoc" if args.target.startswith("mac-") else "unsigned",
                    "notarized": bool(args.notary_profile)}
        artifact.with_suffix(artifact.suffix + ".json").write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(json.dumps(metadata, ensure_ascii=False, indent=2), flush=True)


if __name__ == "__main__":
    main()
