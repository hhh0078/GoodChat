/** 文件作用说明：注入统一 Ant Design 中文语言、自然绿主题与交互反馈容器。 */
"use client";
import { App, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import type { ReactNode } from "react";

/** Providers 为所有页面提供一致组件风格，保持服务端 children 可正常渲染。 */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: "#3c7958",
          colorInfo: "#3c7958",
          colorText: "#26332c",
          colorTextSecondary: "#657269",
          colorBgLayout: "#f4f5f3",
          colorBorder: "#d8ded8",
          colorPrimaryBg: "#eef2ed",
          colorInfoBg: "#f5f7f3",
          colorInfoBorder: "#d8ded8",
          borderRadius: 6,
          fontFamily:
            "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', sans-serif",
          controlHeight: 36,
        },
        components: {
          Button: { primaryShadow: "none" },
          Table: { headerBg: "#f5f6f4" },
          Card: { paddingLG: 24 },
          Menu: { itemSelectedBg: "#eef2ed", itemSelectedColor: "#3c7958" },
        },
      }}
    >
      <App>{children}</App>
    </ConfigProvider>
  );
}
