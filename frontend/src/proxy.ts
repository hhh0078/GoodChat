/** 文件作用说明：支持官网语言路径与 URL 偏好，转发已验证语言而不修改租户设置。 */
import { NextRequest, NextResponse } from "next/server";
import { locales, normalizeLocale, validTimezone } from "./i18n/preferences";

/** proxy 将官网语言路径重写至现有页面，同时隔离每个请求的语言与时区。 */
export function proxy(request: NextRequest) {
  const url = request.nextUrl.clone();
  const head = new Headers(request.headers);
  const segment = url.pathname.split("/")[1];
  const pathLocale = locales.find((locale) => locale.toLowerCase() === segment?.toLowerCase());
  const queryLocale =
    url.searchParams.getAll("lang").length === 1
      ? normalizeLocale(url.searchParams.get("lang"))
      : undefined;
  const saved = normalizeLocale(request.cookies.get("goodchat.locale")?.value);
  const accepted = (request.headers.get("Accept-Language") || "")
    .split(",")
    .map((part) => normalizeLocale(part.split(";")[0]))
    .find(Boolean);
  const locale = queryLocale || pathLocale || saved || accepted || "en";
  head.set("X-GoodChat-Locale", locale);
  head.set("X-GoodChat-Pathname", request.nextUrl.pathname);
  const timezone =
    url.searchParams.getAll("timezone").length === 1
      ? validTimezone(url.searchParams.get("timezone"))
      : undefined;
  head.set(
    "X-GoodChat-Timezone",
    timezone || validTimezone(request.cookies.get("goodchat.timezone")?.value) || "UTC",
  );
  if (pathLocale) {
    url.pathname = url.pathname.slice(segment.length + 1) || "/";
    // 重写后 Next.js 可能重新解析内部请求，显式保留语言以免退回旧 Cookie。
    url.searchParams.set("lang", locale);
  }
  const response = pathLocale
    ? NextResponse.rewrite(url, { request: { headers: head } })
    : NextResponse.next({ request: { headers: head } });
  response.headers.set("Vary", "Accept-Language, Cookie");
  return response;
}
export const config = { matcher: ["/((?!api/|ws/|_next/|.*\\.[a-zA-Z0-9]+$).*)"] };
