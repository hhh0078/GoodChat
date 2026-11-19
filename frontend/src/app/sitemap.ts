/** 文件作用说明：为九语言全部公开官网页面输出与页面head一致的XML目录，不收录账号或客户资料。 */
import { articleData, articlePath, articleAlternates } from "@/lib/site-articles";
import type { SiteArticle } from "@/features/articles/types";
import { siteURL } from "@/lib/urls";
import { publicSitemap } from "../lib/public-seo.ts";
import type { MetadataRoute } from "next";
/** sitemap 复用官网页面与语言规则，避免新增语言后目录遗漏或替代链接冲突。 */
export const dynamic = "force-dynamic";
export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const origin = siteURL();
  const data = await articleData<{ items: SiteArticle[] }>("articles/sitemap");
  const items = data?.items || [];
  const groups = new Map<string, SiteArticle[]>();
  for (const a of items) groups.set(a.group_id, [...(groups.get(a.group_id) || []), a]);
  return [
    ...publicSitemap(origin),
    ...items.map((a) => ({
      url: new URL(articlePath(a), origin).href,
      lastModified: new Date(a.updated_at),
      alternates: {
        languages: Object.fromEntries(
          Object.entries(articleAlternates(groups.get(a.group_id) || [])).map(([lang, path]) => [
            lang,
            new URL(path, origin).href,
          ]),
        ),
      },
    })),
  ];
}
