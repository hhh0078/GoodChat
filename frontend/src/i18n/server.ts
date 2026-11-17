/** 文件作用说明：为异步官网组件和元数据绑定当前请求的翻译器与偏好。 */
import { getLocale, getMessages } from "next-intl/server";
import { cookies, headers } from "next/headers";
import { normalizeLocale, validTimezone, type Preferences } from "./preferences";
import { createTextTranslator, type Messages } from "./runtime";

/** getTranslator 读取请求语言并返回可翻译静态内容的隔离函数。 */
export async function getTranslator() {
  return createTextTranslator(await getLocale(), (await getMessages()) as Messages);
}

/** getPreferences 只读取有效 URL 转发值及持久偏好，不在服务器猜测设备时区。 */
export async function getPreferences(): Promise<Preferences> {
  const head = await headers();
  const stored = await cookies();
  return {
    locale: normalizeLocale(await getLocale()) || "en",
    timezone:
      validTimezone(head.get("X-GoodChat-Timezone")) ||
      validTimezone(stored.get("goodchat.timezone")?.value) ||
      "UTC",
  };
}
