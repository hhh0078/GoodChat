/** 文件作用说明：服务端展示当前语言的已发布文章列表，提供可抓取分页及独立语言SEO。 */
import Link from "next/link";
import { getLocale } from "next-intl/server";
import { getTranslator } from "@/i18n/server";
import { publicMetadata } from "@/i18n/metadata";
import { articleData, articlePath } from "@/lib/site-articles";
import type { SiteArticle } from "@/features/articles/types";
import { publicArticleListPath } from "@/lib/public-seo";
import type { Locale } from "@/i18n/preferences";
import "@/features/articles/articles.css";
/** generateMetadata 为每页文章目录保留自身规范网址，游标不同的语言页面不伪装为译文。 */
export async function generateMetadata({
  searchParams,
}: {
  searchParams: Promise<{ cursor?: string }>;
}) {
  const { cursor } = await searchParams;
  const locale = (await getLocale()) as Locale;
  const metadata = await publicMetadata(
    "/articles",
    "使用指南与行业场景",
    "了解AI客服在不同行业的接入方法、知识库配置与使用场景。",
  );
  if (!cursor) return metadata;
  const canonical = publicArticleListPath(locale, cursor);
  return {
    ...metadata,
    alternates: { canonical },
    openGraph: { ...metadata.openGraph, url: canonical },
  };
}
/** ArticlesPage 在服务器过滤语言，切换语言时不会展示其他语言文章。 */
export default async function ArticlesPage({
  searchParams,
}: {
  searchParams: Promise<{ cursor?: string }>;
}) {
  const locale = (await getLocale()) as Locale;
  const tr = await getTranslator();
  const { cursor } = await searchParams;
  const query = new URLSearchParams({
    language: locale,
    limit: "12",
    ...(cursor ? { cursor } : {}),
  });
  const page = await articleData<{ items: SiteArticle[]; has_more: boolean; next_cursor: string }>(
    `articles?${query}`,
  );
  return (
    <div className="public-width article-public">
      <header className="article-public-heading">
        <span className="public-overline">GoodChat</span>
        <h1>{tr("使用指南与行业场景")}</h1>
        <p>{tr("了解AI客服在不同行业的接入方法、知识库配置与使用场景。")}</p>
      </header>
      <div className="article-public-grid">
        {page?.items.map((a) => (
          <article key={a.id} className="article-public-card">
            <time dateTime={a.published_at || a.created_at}>
              {new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone: "UTC" }).format(
                new Date(a.published_at || a.created_at),
              )}
            </time>
            <h2>
              <Link href={articlePath(a)}>{a.title}</Link>
            </h2>
            <p>{a.summary}</p>
            <Link href={articlePath(a)}>{tr("阅读全文")} →</Link>
          </article>
        ))}
      </div>
      {!page?.items.length && (
        <div className="article-public-empty">{tr("当前语言暂无已发布文章。")}</div>
      )}
      <nav className="article-public-pagination" aria-label={tr("文章分页")}>
        {cursor ? <Link href={publicArticleListPath(locale)}>{tr("返回首页")}</Link> : <span />}
        {page?.has_more && (
          <Link href={publicArticleListPath(locale, page.next_cursor)}>{tr("下一页")} →</Link>
        )}
      </nav>
    </div>
  );
}
