/** 文件作用说明：定义九语服务端根布局、品牌图标、SEO 和个人语言时区环境。 */
import { Providers } from "@/components/providers";
import { LocaleProvider } from "@/i18n/locale-provider";
import type { Messages } from "@/i18n/runtime";
import { getPreferences, getTranslator } from "@/i18n/server";
import { browserSymbolCompatibilityScript } from "@/lib/browser-symbol-compat";
import { brandShareImages } from "@/lib/site-brand";
import { siteURL } from "@/lib/urls";
import { AntdRegistry } from "@ant-design/nextjs-registry";
import type { Metadata } from "next";
import { getMessages } from "next-intl/server";
import Script from "next/script";
import "./globals.css";
import "@/styles/console-redesign.css";
import "@/styles/public-redesign.css";
import "@/styles/studio-redesign.css";
import "@/styles/workbench-redesign.css";

/** generateMetadata 按请求语言生成文案，保留最新品牌图标与分享图片。 */
export async function generateMetadata(): Promise<Metadata> {
  const tr = await getTranslator();
  const preferences = await getPreferences();
  return {
    metadataBase: new URL(siteURL()),
    icons: {
      icon: [
        { url: "/favicon.svg?v=20261117-logo", type: "image/svg+xml" },
        { url: "/logo/favicon-32.png", sizes: "32x32", type: "image/png" },
        { url: "/logo/favicon-48.png", sizes: "48x48", type: "image/png" },
      ],
      shortcut: "/favicon.ico?v=20261117-logo",
      apple: { url: "/logo/apple-touch-icon.png", sizes: "180x180", type: "image/png" },
    },
    title: { default: tr("GoodChat · AI 客服与免费基础版"), template: "%s · GoodChat" },
    description: tr(
      "GoodChat AI 客服支持知识库自动回复、图片识别、媒体话术与人工接管。小团队基础系统免费，含 2 个客服坐席；AI 调用按实际 Token 计费。",
    ),
    openGraph: {
      type: "website",
      locale: preferences.locale.replace("-", "_"),
      siteName: "GoodChat",
      images: brandShareImages.map((image) => ({ ...image, alt: "GoodChat" })),
    },
    twitter: { card: "summary_large_image", images: ["/logo/goodchat-share.png"] },
  };
}
/** RootLayout 保留服务端正文，并为浏览器初始化单语言目录和时区。 */
export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const preferences = await getPreferences();
  const messages = (await getMessages()) as Messages;
  return (
    <html lang={preferences.locale}>
      <head>
        <Script id="goodchat-browser-symbol-compat" strategy="beforeInteractive">
          {browserSymbolCompatibilityScript}
        </Script>
      </head>
      <body>
        <AntdRegistry>
          <LocaleProvider initial={preferences} messages={messages}>
            <Providers>{children}</Providers>
          </LocaleProvider>
        </AntdRegistry>
      </body>
    </html>
  );
}
