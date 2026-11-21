/** 文件作用说明：按语言服务端渲染安全Markdown文章，标题、规范地址及结构化数据与内容一致。 */
import { getLocale } from "next-intl/server";
import { notFound } from "next/navigation";
import Link from "next/link";
import type { Metadata } from "next";
import { getTranslator } from "@/i18n/server";
import {
  articleAlternates,
  articlePath,
  articleStructuredData,
  getPublishedArticle,
} from "@/lib/site-articles";
import { requestSiteURL } from "@/lib/site-origin";
import "@/features/articles/articles.css";
type Props = { params: Promise<{ articleSlug: string }> };
/** generateMetadata 使用绝对标题避免站点模板追加，缺失语言不生成另一语言地址。 */
export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await getLocale();
  const { articleSlug } = await params;
  const value = await getPublishedArticle(locale, articleSlug);
  if (!value) return { robots: { index: false, follow: false } };
  const a = value.article;
  return {
    title: { absolute: a.title },
    description: a.summary,
    keywords: a.keywords,
    alternates: { canonical: articlePath(a), languages: articleAlternates(value.alternates) },
    openGraph: {
      type: "article",
      title: a.title,
      description: a.summary,
      url: articlePath(a),
      locale: a.language.replace("-", "_"),
      publishedTime: a.published_at || undefined,
      modifiedTime: a.updated_at,
    },
    twitter: { card: "summary", title: a.title, description: a.summary },
  };
}
/** ArticlePage 保留唯一主标题，正文HTML由后端CommonMark安全渲染而来。 */
export default async function ArticlePage({ params }: Props) {
  const locale = await getLocale();
  const tr = await getTranslator();
  const { articleSlug } = await params;
  const value = await getPublishedArticle(locale, articleSlug);
  if (!value) notFound();
  const a = value.article;
  return (
    <div className="public-width article-public">
      <article className="article-reading">
        <header className="article-public-heading">
          <Link href={`/${locale}/articles`}>← {tr("文章")}</Link>
          <h1>{a.title}</h1>
          <p>{a.summary}</p>
          <time dateTime={a.published_at || a.created_at}>
            {new Intl.DateTimeFormat(locale, { dateStyle: "long", timeZone: "UTC" }).format(
              new Date(a.published_at || a.created_at),
            )}
          </time>
        </header>
        <div className="article-prose" dangerouslySetInnerHTML={{ __html: a.body_html || "" }} />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: articleStructuredData(a, await requestSiteURL()) }}
        />
      </article>
    </div>
  );
}
