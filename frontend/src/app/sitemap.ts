/** 文件作用说明：为九语言官网及已发布文章输出简洁标准XML目录，不收录账号或客户资料。 */
import { articleData, articlePath } from "@/lib/site-articles";
import type { SiteArticle } from "@/features/articles/types";
import { requestSiteURL } from "@/lib/site-origin";
import { publicSitemap } from "../lib/public-seo.ts";
import type { MetadataRoute } from "next";
export const dynamic = "force-dynamic";
/** sitemap 保留全部语言规范网址与文章更新时间，多语言替代关系由页面head声明。 */
export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const origin = await requestSiteURL();
  const data = await articleData<{ items: SiteArticle[] }>("articles/sitemap");
  const items = data?.items || [];
  return [
    ...publicSitemap(origin),
    ...items.map((a) => ({
      url: new URL(articlePath(a), origin).href,
      lastModified: new Date(a.updated_at),
    })),
  ];
}
