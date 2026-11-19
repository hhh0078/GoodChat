/** 文件作用说明：为公开页面链接生成可索引的语言路径，私有页面保留既有路由。 */
"use client";
import { useLocale } from "next-intl";
import NextLink from "next/link";
import type { ComponentProps } from "react";
import { locales } from "./preferences";
const publicPaths = new Set([
  "/",
  "/features",
  "/solutions",
  "/pricing",
  "/help",
  "/downloads",
  "/articles",
  "/privacy",
  "/terms",
]);

/** localizedPath 为已发布官网页面附加当前语言，忽略外部地址和业务接入路径。 */
export function localizedHref(href: string, locale: string): string {
  if (!href.startsWith("/") || href.startsWith("//")) return href;
  const url = new URL(href, "https://goodchat.invalid");
  const first = url.pathname.split("/")[1];
  const base = locales.includes(first as (typeof locales)[number])
    ? url.pathname.slice(first.length + 1) || "/"
    : url.pathname;
  return publicPaths.has(base)
    ? `/${locale}${base === "/" ? "" : base}${url.search}${url.hash}`
    : href;
}

/** LocalizedLink 保留 Next 链接行为，并让爬虫和用户访问相同的语言版本。 */
export default function LocalizedLink(props: ComponentProps<typeof NextLink>) {
  const locale = useLocale();
  return (
    <NextLink
      {...props}
      href={typeof props.href === "string" ? localizedHref(props.href, locale) : props.href}
    />
  );
}
