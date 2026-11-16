/** 文件作用说明：为组件绑定当前请求的语言与时区，避免服务端并发渲染或水合时串用全局偏好。 */
import { useLocale, useTimeZone } from "next-intl";
import { normalizeLocale, type Preferences } from "./preferences";

/** useFormats 返回请求隔离的时间数字格式器，业务报表仍可显式指定租户时区。 */
export function useFormats() {
  const preferences: Preferences = {
    locale: normalizeLocale(useLocale()) || "en",
    timezone: useTimeZone() || "UTC",
  };
  return {
    currentPreferences: () => preferences,
    numberFormatter: (options: Intl.NumberFormatOptions = {}) =>
      new Intl.NumberFormat(preferences.locale, options),
    dateTimeFormatter: (options: Intl.DateTimeFormatOptions = {}) =>
      new Intl.DateTimeFormat(preferences.locale, { timeZone: preferences.timezone, ...options }),
  };
}
