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
          colorPrimary: "#23684e",
          colorInfo: "#23684e",
          colorText: "#21342c",
          colorTextSecondary: "#68776e",
          colorBgLayout: "#f4f6f2",
          borderRadius: 10,
          fontFamily:
            "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', sans-serif",
          controlHeight: 38,
        },
        components: {
          Button: { primaryShadow: "none" },
          Table: { headerBg: "#f5f7f3" },
          Card: { paddingLG: 24 },
        },
      }}
    >
      <App>{children}</App>
    </ConfigProvider>
  );
}
