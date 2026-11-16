/** 文件作用说明：只列出不含账号和客户资料的公开官网页面。 */
import { siteURL } from "@/lib/urls";
import type { MetadataRoute } from "next";
import { locales } from "../i18n/preferences.ts";
/** sitemap 生成部署域名下的公开信息页面目录。 */
export default function sitemap(): MetadataRoute.Sitemap {
  const paths = ["", "features", "solutions", "pricing", "help", "downloads", "privacy", "terms"];
  return locales.flatMap((locale) =>
    paths.map((path) => ({
      url: `${siteURL()}/${locale}${path ? `/${path}` : ""}`,
      alternates: {
        languages: Object.fromEntries(
          locales.map((code) => [code, `${siteURL()}/${code}${path ? `/${path}` : ""}`]),
        ),
      },
      changeFrequency: "monthly" as const,
      priority: path ? 0.7 : 1,
    })),
  );
}
