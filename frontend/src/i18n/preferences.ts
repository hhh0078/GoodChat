/** 文件作用说明：统一九种语言、IANA 时区和逐字段偏好优先级，供网页、桌面与测试复用。 */
export const locales = ["zh-CN", "en", "zh-TW", "ja", "ko", "es", "pt-BR", "fr", "de"] as const;
export type Locale = (typeof locales)[number];
export type Preferences = { locale: Locale; timezone: string };
export type SavedPreferences = Partial<Preferences>;

/** parseSavedPreferences 只保留实际有效的字段，避免空会话覆盖已经确认的持久偏好。 */
export function parseSavedPreferences(value: unknown): SavedPreferences {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const input = value as Record<string, unknown>;
  const locale = typeof input.locale === "string" ? normalizeLocale(input.locale) : undefined;
  const timezone = typeof input.timezone === "string" ? validTimezone(input.timezone) : undefined;
  return { ...(locale ? { locale } : {}), ...(timezone ? { timezone } : {}) };
}
export const languageNames: Record<Locale, string> = {
  "zh-CN": "简体中文",
  en: "English",
  "zh-TW": "繁體中文",
  ja: "日本語",
  ko: "한국어",
  es: "Español",
  "pt-BR": "Português (Brasil)",
  fr: "Français",
  de: "Deutsch",
};

/** normalizeLocale 将浏览器语言映射至已发布语言；不支持的语言返回空值。 */
export function normalizeLocale(value?: string | null): Locale | undefined {
  if (!value) return undefined;
  const code = value.replaceAll("_", "-").toLowerCase();
  if (code === "zh" || /^zh-(cn|sg|hans)(-|$)/.test(code)) return "zh-CN";
  if (/^zh-(tw|hk|mo|hant)(-|$)/.test(code)) return "zh-TW";
  if (/^pt(-|$)/.test(code)) return "pt-BR";
  return locales.find((locale) => locale.toLowerCase() === code || locale === code.split("-")[0]);
}

/** validTimezone 验证 IANA 名称，不接受固定偏移和未知时区。 */
export function validTimezone(value?: string | null): string | undefined {
  if (!value || value.length > 100 || (value !== "UTC" && !value.includes("/"))) return undefined;
  try {
    new Intl.DateTimeFormat("en", { timeZone: value }).format();
    return value;
  } catch {
    return undefined;
  }
}

/** availableTimezones 返回可搜索的 IANA 时区，并兼容缺少 supportedValuesOf 的浏览器。 */
export function availableTimezones(current = "UTC"): string[] {
  const fallback = [
    "UTC",
    "Asia/Shanghai",
    "Asia/Taipei",
    "Asia/Tokyo",
    "Asia/Seoul",
    "America/New_York",
    "America/Los_Angeles",
    "America/Sao_Paulo",
    "Europe/London",
    "Europe/Paris",
    "Europe/Berlin",
    "Europe/Madrid",
  ];
  try {
    return [...new Set(["UTC", current, ...Intl.supportedValuesOf("timeZone")])];
  } catch {
    return [...new Set([current, ...fallback])];
  }
}

/** resolvePreferences 分别应用 URL、已保存选择和设备检测，只有检测值需要确认。 */
export function resolvePreferences(
  query: URLSearchParams,
  saved: SavedPreferences,
  device: { languages?: readonly string[]; timezone?: string },
) {
  const locale = query.getAll("lang").length === 1 ? normalizeLocale(query.get("lang")) : undefined;
  const timezone =
    query.getAll("timezone").length === 1 ? validTimezone(query.get("timezone")) : undefined;
  const storedLocale = query.has("lang") && !locale ? undefined : normalizeLocale(saved.locale);
  const storedTimezone =
    query.has("timezone") && !timezone ? undefined : validTimezone(saved.timezone);
  return {
    locale: locale || storedLocale || device.languages?.map(normalizeLocale).find(Boolean) || "en",
    timezone: timezone || storedTimezone || validTimezone(device.timezone) || "UTC",
    confirmLocale: !locale && !storedLocale,
    confirmTimezone: !timezone && !storedTimezone,
    invalidLocale: query.has("lang") && !locale,
    invalidTimezone: query.has("timezone") && !timezone,
  };
}

/** messageKey 对 UTF-8 原文生成跨 Go、浏览器一致的稳定消息标识。 */
export function messageKey(source: string): string {
  let value = 2166136261;
  for (const byte of new TextEncoder().encode(source))
    value = Math.imul(value ^ byte, 16777619) >>> 0;
  return `m${value.toString(16).padStart(8, "0")}`;
}
