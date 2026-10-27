/** 文件作用说明：为搜索引擎标明公开官网与私有工作端的抓取边界。 */
import type { MetadataRoute } from "next";
import { siteURL } from "@/lib/urls";
/** robots 输出官网抓取规则，鉴权仍由服务端执行。 */
export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: "*",
      allow: "/",
      disallow: [
        "/console",
        "/platform",
        "/workspace",
        "/c/",
        "/login",
        "/register",
        "/reset-password",
        "/accept-invitation",
        "/api/",
      ],
    },
    sitemap: `${siteURL()}/sitemap.xml`,
  };
}
