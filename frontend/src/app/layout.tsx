/** 文件作用说明：定义网站服务端根布局、SEO 基础信息和 Ant Design 首屏样式。 */
import type { Metadata } from "next";
import Script from "next/script";
import { AntdRegistry } from "@ant-design/nextjs-registry";
import { Providers } from "@/components/providers";
import { siteURL } from "@/lib/urls";
import { browserSymbolCompatibilityScript } from "@/lib/browser-symbol-compat";
import "./globals.css";
import "@/styles/workbench-redesign.css";
import "@/styles/console-redesign.css";
import "@/styles/public-redesign.css";
import "@/styles/studio-redesign.css";

export const metadata: Metadata = {
  metadataBase: new URL(siteURL()),
  icons: { icon: "/favicon.svg" },
  title: { default: "GoodChat · 在线客服与 AI 页面装修", template: "%s · GoodChat" },
  description:
    "面向商户的在线客服 SaaS。多链接接入、灵活分配、统一客服工作台，租户自主设计和发布自己的聊天页面。",
  openGraph: { type: "website", locale: "zh_CN", siteName: "GoodChat" },
};
/** RootLayout 保留语义化服务端 HTML，并注入客户端交互主题。 */
export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <head>
        <Script id="goodchat-browser-symbol-compat" strategy="beforeInteractive">
          {browserSymbolCompatibilityScript}
        </Script>
      </head>
      <body>
        <AntdRegistry>
          <Providers>{children}</Providers>
        </AntdRegistry>
      </body>
    </html>
  );
}
