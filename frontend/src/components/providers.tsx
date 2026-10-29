/** 文件作用说明：注入用户确认的银白钴蓝 Ant Design 主题、中文语言与交互反馈容器。 */
"use client";
import { App, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { useEffect, type ReactNode } from "react";
import { stopBrowserSymbolCompatibility } from "@/lib/browser-symbol-compat";

/** Providers 为所有页面提供一致组件风格，保持服务端 children 可正常渲染。 */
export function Providers({ children }: { children: ReactNode }) {
  useEffect(() => {
    stopBrowserSymbolCompatibility();
  }, []);
  return (
    <ConfigProvider
      locale={zhCN}
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
      <App>{children}</App>
    </ConfigProvider>
  );
}
