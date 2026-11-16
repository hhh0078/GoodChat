/** 文件作用说明：提供工具函数的客户端翻译、受控静态资源翻译及语言时区请求头。 */
import { createTranslator } from "next-intl";
import { messageKey, type Preferences } from "./preferences.ts";
export type Translator = (
  source: string,
  params?: Record<string, string | number | Date>,
) => string;
export type Messages = Record<string, string>;
let browserPreferences: Preferences = { locale: "zh-CN", timezone: "Asia/Shanghai" };
let browserTranslator: Translator = (source, params) => interpolate(source, params);

/** interpolate 在未加载翻译或消息缺失时保留原文，并安全插入文本变量。 */
function interpolate(source: string, params: Record<string, unknown> = {}): string {
  return source.replace(/\{(p\d+)\}/g, (match, key) => String(params[key] ?? match));
}

/** createTextTranslator 绑定当前请求或组件语言，不在服务器共享用户偏好。 */
export function createTextTranslator(locale: string, messages: Messages): Translator {
  const format = createTranslator({
    locale,
    messages,
    onError() {},
    getMessageFallback: ({ key }) => key,
  });
  return (source, params) => {
    const key = /^m[0-9a-f]{8}$/.test(source) ? source : messageKey(source);
    if (!Object.hasOwn(messages, key)) return interpolate(source, params);
    if (messages[key] === "") return "";
    let index = 0;
    return format(key, params).replace(/%(?:\[\d+\])?[-+#0 ]*\d*(?:\.\d+)?[sdvTf]/g, (match) =>
      String(params?.[`p${index++}`] ?? match),
    );
  };
}

/** setBrowserPreferences 只在浏览器更新工具函数环境，避免服务端跨请求串语言。 */
export function setBrowserPreferences(preferences: Preferences, messages: Messages): void {
  if (typeof window === "undefined") return;
  browserPreferences = preferences;
  browserTranslator = createTextTranslator(preferences.locale, messages);
}

/** translate 翻译事件回调及非 React 工具的系统文案，不用于任意业务内容。 */
export function translate(source: string, params?: Record<string, string | number | Date>): string {
  return browserTranslator(source, params);
}

/** currentPreferences 读取当前浏览器偏好；服务器调用应显式绑定请求偏好。 */
export function currentPreferences(): Preferences {
  return browserPreferences;
}

/** localizeStatic 仅翻译源码中受控的静态标签对象，不处理接口返回的用户数据。 */
export function localizeStatic<T>(value: T, tr: Translator): T {
  if (typeof value === "string") return tr(value) as T;
  if (Array.isArray(value)) return value.map((item) => localizeStatic(item, tr)) as T;
  if (value && typeof value === "object" && Object.getPrototypeOf(value) === Object.prototype)
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, localizeStatic(item, tr)]),
    ) as T;
  return value;
}

/** preferenceHeaders 将已经选定的语言和个人时区传给服务端，时区不改变租户业务规则。 */
export function preferenceHeaders(): Record<string, string> {
  return {
    "Accept-Language": browserPreferences.locale,
    "X-GoodChat-Locale": browserPreferences.locale,
    "X-GoodChat-Timezone": browserPreferences.timezone,
  };
}
