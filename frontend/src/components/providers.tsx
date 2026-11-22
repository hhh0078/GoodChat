/** 文件作用说明：注入用户确认的银白钴蓝 Ant Design 主题、中文语言与交互反馈容器。 */
"use client";
import { RequestErrorDialog } from "./request-error-dialog";
import { antdLocales } from "@/i18n/antd-locales";
import { usePreferences } from "@/i18n/locale-provider";
import { stopBrowserSymbolCompatibility } from "@/lib/browser-symbol-compat";
import { App, ConfigProvider } from "antd";
import { useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";

/** Providers 为所有页面提供一致组件风格，保持服务端 children 可正常渲染。 */
export function Providers({ children }: { children: ReactNode }) {
  const preferences = usePreferences();
  const router = useRouter();
  useEffect(() => {
    /** refresh 对主动设置执行真实路由切换，避免刷新旧路由后恢复原语言。 */
    const refresh = (event: Event) => {
      const url = (event as CustomEvent<{ url?: string }>).detail?.url;
      if (url) {
        event.preventDefault();
        router.replace(url);
      } else {
        router.refresh();
      }
    };
    window.addEventListener("goodchat:preferences-changed", refresh);
    return () => window.removeEventListener("goodchat:preferences-changed", refresh);
  }, [router]);
  useEffect(() => {
    stopBrowserSymbolCompatibility();
  }, []);
  return (
    <ConfigProvider
      locale={antdLocales[preferences.locale]}
      theme={{
        token: {
          colorPrimary: "#1455ff",
          colorInfo: "#1455ff",
          colorText: "#111a2c",
          colorTextSecondary: "#5e6b83",
          colorTextTertiary: "#67748b",
          colorBgLayout: "#f4f7fc",
          colorBorder: "#d5dff0",
          colorBorderSecondary: "#e5ebf5",
          colorPrimaryBg: "#eaf0ff",
          colorInfoBg: "#f0f5ff",
          colorInfoBorder: "#dce6fc",
          borderRadius: 8,
          borderRadiusLG: 12,
          fontFamily: "'Helvetica Neue', 'PingFang SC', 'Microsoft YaHei', sans-serif",
          controlHeight: 36,
        },
        components: {
          Button: { primaryShadow: "0 3px 10px rgba(20, 85, 255, 0.14)", fontWeight: 500 },
          Table: { headerBg: "#f3f6fc", headerColor: "#52617b", rowHoverBg: "#f3f7ff" },
          Card: { paddingLG: 24 },
          Menu: {
            itemSelectedBg: "#eaf0ff",
            itemSelectedColor: "#1455ff",
            itemHoverBg: "#f1f5fc",
            itemHeight: 40,
            itemBorderRadius: 8,
          },
          Input: { activeShadow: "0 0 0 3px rgba(20, 85, 255, 0.10)" },
          Select: { activeOutlineColor: "rgba(20, 85, 255, 0.10)" },
        },
      }}
    >
      <App>
        {children}
        <RequestErrorDialog />
      </App>
    </ConfigProvider>
  );
}
