/** 文件作用说明：按当前请求解析语言并按需加载单个语言目录，保留服务端渲染。 */
import { getRequestConfig } from "next-intl/server";
import { cookies, headers } from "next/headers";
import { normalizeLocale, validTimezone } from "./preferences";

export default getRequestConfig(async () => {
  const head = await headers();
  const stored = await cookies();
  const locale =
    normalizeLocale(head.get("X-GoodChat-Locale")) ||
    normalizeLocale(stored.get("goodchat.locale")?.value) ||
    "en";
  return {
    locale,
    messages: (await import(`./messages/${locale}.json`)).default,
    timeZone:
      validTimezone(head.get("X-GoodChat-Timezone")) ||
      validTimezone(stored.get("goodchat.timezone")?.value) ||
      "UTC",
  };
});
