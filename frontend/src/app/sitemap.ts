/** 文件作用说明：只列出不含账号和客户资料的公开官网页面。 */
import type { MetadataRoute } from "next";
import { siteURL } from "@/lib/urls";
/** sitemap 生成部署域名下的公开信息页面目录。 */
export default function sitemap(): MetadataRoute.Sitemap {
  return ["", "features", "solutions", "pricing", "help", "privacy", "terms"].map((path) => ({
    url: `${siteURL()}/${path}`,
    changeFrequency: "monthly",
    priority: path ? 0.7 : 1,
  }));
}
