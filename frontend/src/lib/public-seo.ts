/** 文件作用说明：统一公开官网的语言网址、sitemap目录及与可见收费说明一致的结构化数据。 */
import { locales, type Locale } from "../i18n/preferences.ts";
export const publicPagePaths = [
  "/",
  "/features",
  "/solutions",
  "/pricing",
  "/help",
  "/downloads",
  "/privacy",
  "/terms",
] as const;

/** publicPagePath 返回语言独立且不含偏好参数的官网规范路径。 */
export function publicPagePath(locale: Locale, path: string): string {
  return `/${locale}${path === "/" ? "" : path}`;
}

/** publicLanguagePaths 为HTML与XML复用同一组含自身和默认语言的替代链接。 */
export function publicLanguagePaths(path: string): Record<string, string> {
  return {
    ...Object.fromEntries(locales.map((locale) => [locale, publicPagePath(locale, path)])),
    "x-default": publicPagePath("en", path),
  };
}

/** publicSitemap 只包含八类公开官网页面，禁止把账号、租户或访客资料放入目录。 */
export function publicSitemap(origin: string) {
  return locales.flatMap((locale) =>
    publicPagePaths.map((path) => ({
      url: new URL(publicPagePath(locale, path), origin).href,
      alternates: {
        languages: Object.fromEntries(
          Object.entries(publicLanguagePaths(path)).map(([code, target]) => [
            code,
            new URL(target, origin).href,
          ]),
        ),
      },
      changeFrequency: "monthly" as const,
      priority: path === "/" ? 1 : 0.7,
    })),
  );
}

/** publicSoftwareData 描述免费基础系统及其明确的AI按量费用，不虚构评分或免费模型额度。 */
export function publicSoftwareData(
  origin: string,
  locale: Locale,
  name: string,
  description: string,
  freeTerms: string,
) {
  const root = new URL("/", origin).href;
  const url = new URL(publicPagePath(locale, "/"), origin).href;
  return {
    "@context": "https://schema.org",
    "@graph": [
      { "@type": "WebSite", "@id": `${root}#website`, name: "GoodChat", url, inLanguage: locale },
      {
        "@type": "SoftwareApplication",
        "@id": `${root}#software`,
        name: `GoodChat · ${name}`,
        url,
        description,
        inLanguage: locale,
        applicationCategory: "BusinessApplication",
        operatingSystem: "Web",
        offers: {
          "@type": "Offer",
          price: "0",
          priceCurrency: "CNY",
          description: freeTerms,
          url: new URL(publicPagePath(locale, "/pricing"), origin).href,
        },
      },
    ],
  };
}
