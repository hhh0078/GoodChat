/** 文件作用说明：按各系统的最新公开版本展示真实下载链接，不显示虚构APK或过期静态版本。 */
"use client";
import { useTranslator } from "@/i18n/use-translator";
import { api, errorMessage } from "@/lib/api";
import {
  AndroidOutlined,
  AppleOutlined,
  DownloadOutlined,
  WindowsOutlined,
} from "@ant-design/icons";
import { Alert, Button, Spin } from "antd";
import { useEffect, useState } from "react";
import "./downloads.css";
type Release = {
  id: string;
  version: string;
  description: string;
  win_url: string;
  mac_url: string;
  apk_url: string;
};
/** ClientDownloads 从公开API读取发布列表，数字版本排序由服务端保证。 */
export function ClientDownloads() {
  const tr = useTranslator();
  const [items, setItems] = useState<Release[]>([]),
    [loading, setLoading] = useState(true),
    [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    api<{
      items: Release[];
    }>("/public/client-releases", { signal: controller.signal })
      .then((data) => setItems(data.items || []))
      .catch((problem) => {
        if (!controller.signal.aborted) setError(errorMessage(problem));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);
  return (
    <div className="download-page public-width">
      <span className="download-eyebrow">GOODCHAT DESKTOP</span>
      <h1>{tr("把接待工作，留在工作台。")}</h1>
      <p className="download-intro">
        {tr("使用桌面客户端接待客户。窗口最小化或关闭后，程序继续在系统托盘或菜单栏处理消息。 ")}
      </p>
      {error && <Alert type="error" title={error} />}
      <Spin spinning={loading}>
        <div className="download-platforms">
          {[
            {
              key: "mac_url",
              name: "Mac",
              icon: <AppleOutlined />,
              note: tr("macOS 12及以上 \u00B7 Apple芯片与Intel"),
            },
            {
              key: "win_url",
              name: "Windows",
              icon: <WindowsOutlined />,
              note: tr("Windows 10/11 \u00B7 64位 \u00B7 需要WebView2"),
            },
            { key: "apk_url", name: "Android", icon: <AndroidOutlined />, note: "Android APK" },
          ].map(({ key, name, icon, note }) => {
            const release = items.find((item) => !!item[key as keyof Release]);
            if (key === "apk_url" && !release) return null;
            return (
              <article className="download-platform" key={key}>
                <div className="download-icon">{icon}</div>
                <h2>{name}</h2>
                <p>{note}</p>
                {release ? (
                  <>
                    <span className="download-version">
                      {tr("版本 ")}
                      {release.version}
                    </span>
                    <Button
                      type="primary"
                      size="large"
                      icon={<DownloadOutlined />}
                      href={release[key as keyof Release]}
                    >
                      {tr("下载 ")}
                      {name}
                      {tr(" 客户端 ")}
                    </Button>
                    <p className="download-notes">{release.description}</p>
                  </>
                ) : (
                  <p>{tr("客户端即将提供，请稍后重试。")}</p>
                )}
              </article>
            );
          })}
        </div>
      </Spin>
      <aside className="download-help">
        <h2>{tr("安装与使用")}</h2>
        <p>
          {tr(
            "Mac打开DMG后将GoodChat拖入\u201C应用程序\u201D；Windows解压后运行GoodChat.exe。首次启动可确认或修改服务器地址，使用现有账号登录。 ",
          )}
        </p>
        <p>
          {tr(
            "客户端每次启动会检查当前服务器的最新版本。出现强制更新提示时，请下载安装新版，关闭旧程序后重新打开。系统睡眠、断网或彻底退出时，消息将在恢复连接后核对补齐。 ",
          )}
        </p>
        <p>
          {tr("当前为开发测试版本，尚未完成正式签名公证；Windows缺少WebView2时，请从")}{" "}
          <a
            href="https://developer.microsoft.com/microsoft-edge/webview2/"
            target="_blank"
            rel="noreferrer"
          >
            {tr("微软官方 ")}
          </a>{" "}
          {tr("安装运行时。 ")}
        </p>
      </aside>
    </div>
  );
}
