/** 文件作用说明：为九种官网语言生成独立规范地址、标题和双向语言替代链接。 */
import { brandShareImages } from "@/lib/site-brand";
import type { Metadata } from "next";
import { getLocale } from "next-intl/server";
import { locales } from "./preferences";
import { getTranslator } from "./server";

/** publicMetadata 只发布实际支持语言的 SEO 信息，不将不同语言规范化到同一个中文地址。 */
export async function publicMetadata(
  path: string,
  title: string,
  description?: string,
): Promise<Metadata> {
  const locale = await getLocale();
  const tr = await getTranslator();
  const suffix = path === "/" ? "" : path;
  const canonical = `/${locale}${suffix}`;
  return {
    title: tr(title),
    description: description ? tr(description) : undefined,
    alternates: {
      canonical,
      languages: {
        ...Object.fromEntries(locales.map((code) => [code, `/${code}${suffix}`])),
        "x-default": `/en${suffix}`,
      },
    },
    openGraph: {
      title: tr(title),
      description: description ? tr(description) : undefined,
      locale: locale.replace("-", "_"),
      url: canonical,
      images: brandShareImages.map((image) => ({ ...image, alt: "GoodChat" })),
    },
  };
}
