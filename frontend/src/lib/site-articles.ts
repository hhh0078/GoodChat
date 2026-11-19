/** 文件作用说明：服务端读取已发布文章，统一语言路径、元数据与站点地图的真实版本信息。 */
import { cache } from "react";
import type { SiteArticle } from "../features/articles/types.ts";
/** articlePath 生成不带偏好或追踪参数的语言独立地址。 */
export function articlePath(a: Pick<SiteArticle, "language" | "slug">) {
  return `/${a.language}/articles/${a.slug}`;
}
/** articleAlternates 仅为实际存在的已发布语言版本输出替代链接。 */
export function articleAlternates(items: SiteArticle[]) {
  return Object.fromEntries(items.map((a) => [a.language, articlePath(a)]));
}
/** articleData 直接向受控后端读取公开资源，错误不伪装为空文章或跨语言回退。 */
export async function articleData<T>(path: string): Promise<T | null> {
  const response = await fetch(
    `${process.env.BACKEND_URL || "http://127.0.0.1:18080"}/api/v1/public/${path}`,
    { cache: "no-store" },
  );
  if (response.status === 404) return null;
  if (!response.ok) throw new Error("Article service unavailable");
  const value = await response.json();
  return value.data as T;
}
/** getPublishedArticle 在同一服务端请求中复用正文与metadata查询。 */
export const getPublishedArticle = cache((language: string, slug: string) =>
  articleData<{ article: SiteArticle; alternates: SiteArticle[] }>(
    `articles/${encodeURIComponent(slug)}?language=${encodeURIComponent(language)}`,
  ),
);
/** articleStructuredData 使用与页面一致的标题、语言和日期生成可安全嵌入的JSON-LD。 */
export function articleStructuredData(a: SiteArticle, origin: string) {
  return JSON.stringify({
    "@context": "https://schema.org",
    "@type": "Article",
    headline: a.title,
    description: a.summary,
    inLanguage: a.language,
    datePublished: a.published_at,
    dateModified: a.updated_at,
    mainEntityOfPage: new URL(articlePath(a), origin).href,
    author: { "@type": "Organization", name: "GoodChat" },
    publisher: { "@type": "Organization", name: "GoodChat" },
  }).replaceAll("<", "\\u003c");
}
