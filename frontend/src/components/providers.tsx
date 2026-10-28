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
          colorPrimary: "#167558",
          colorInfo: "#167558",
          colorText: "#22282d",
          colorTextSecondary: "#657078",
          colorTextTertiary: "#7b858c",
          colorBgLayout: "#f5f6f8",
          colorBorder: "#dce1e5",
          colorBorderSecondary: "#e9ecef",
          colorPrimaryBg: "#eaf4ef",
          colorInfoBg: "#f2f7f4",
          colorInfoBorder: "#d8e9df",
          borderRadius: 8,
          borderRadiusLG: 12,
          fontFamily: "'Helvetica Neue', 'PingFang SC', 'Microsoft YaHei', sans-serif",
          controlHeight: 36,
        },
        components: {
          Button: { primaryShadow: "0 1px 2px rgba(17, 69, 51, 0.12)", fontWeight: 500 },
          Table: { headerBg: "#f8f9fa", headerColor: "#657078", rowHoverBg: "#f7faf8" },
          Card: { paddingLG: 24 },
          Menu: {
            itemSelectedBg: "#eaf4ef",
            itemSelectedColor: "#167558",
            itemHoverBg: "#eef0f2",
            itemHeight: 40,
            itemBorderRadius: 8,
          },
          Input: { activeShadow: "0 0 0 3px rgba(22, 117, 88, 0.10)" },
          Select: { activeOutlineColor: "rgba(22, 117, 88, 0.10)" },
        },
      }}
    >
      <App>{children}</App>
    </ConfigProvider>
  );
}
