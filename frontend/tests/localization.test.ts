/** 文件作用说明：验证九语协商、URL 逐字段免确认、IANA 时区、语言隔离和安全插值。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  locales,
  messageKey,
  normalizeLocale,
  parseSavedPreferences,
  resolvePreferences,
  validTimezone,
} from "../src/i18n/preferences.ts";
import { createTextTranslator, localizeStatic } from "../src/i18n/runtime.ts";

test("九种语言接受浏览器地区别名，不把未知语言冒充英语", () => {
  assert.equal(locales.length, 9);
  assert.equal(messageKey("登录"), "mbad2af27");
  for (const locale of locales) assert.equal(normalizeLocale(locale), locale);
  assert.equal(normalizeLocale("zh-Hant-HK"), "zh-TW");
  assert.equal(normalizeLocale("en-GB"), "en");
  assert.equal(normalizeLocale("pt-PT"), "pt-BR");
  assert.equal(normalizeLocale("xx"), undefined);
});

test("URL 的有效语言与时区覆盖电脑和已确认设置，两项齐全不弹确认", () => {
  const value = resolvePreferences(
    new URLSearchParams("lang=de&timezone=Europe%2FBerlin"),
    { locale: "ja", timezone: "Asia/Tokyo" },
    { languages: ["zh-CN"], timezone: "Asia/Shanghai" },
  );
  assert.deepEqual(
    [value.locale, value.timezone, value.confirmLocale, value.confirmTimezone],
    ["de", "Europe/Berlin", false, false],
  );
});

test("URL 缺少哪项就独立检测并确认哪项，重复及无效参数不能跳过确认", () => {
  const device = { languages: ["xx", "ko-KR"], timezone: "America/New_York" };
  const language = resolvePreferences(new URLSearchParams("lang=en"), {}, device);
  assert.deepEqual(
    [language.locale, language.timezone, language.confirmLocale, language.confirmTimezone],
    ["en", "America/New_York", false, true],
  );
  const zone = resolvePreferences(new URLSearchParams("timezone=UTC"), {}, device);
  assert.deepEqual(
    [zone.locale, zone.timezone, zone.confirmLocale, zone.confirmTimezone],
    ["ko", "UTC", true, false],
  );
  const invalid = resolvePreferences(
    new URLSearchParams("lang=en&lang=de&timezone=UTC%2B8"),
    {},
    device,
  );
  assert.equal(invalid.confirmLocale, true);
  assert.equal(invalid.confirmTimezone, true);
});

test("已确认的字段不会重复确认，检测失败仍要求确认兜底值", () => {
  const saved = resolvePreferences(
    new URLSearchParams(),
    { locale: "fr", timezone: "Europe/Paris" },
    {},
  );
  assert.equal(saved.confirmLocale || saved.confirmTimezone, false);
  const missing = resolvePreferences(new URLSearchParams(), {}, {});
  assert.deepEqual(
    [missing.locale, missing.timezone, missing.confirmLocale, missing.confirmTimezone],
    ["en", "UTC", true, true],
  );
  assert.equal(validTimezone("Asia/Shanghai"), "Asia/Shanghai");
  assert.equal(validTimezone("GMT+8"), undefined);
  assert.equal(validTimezone("America/Unknown"), undefined);
});

test("各请求译文隔离，ICU 字面量与命名变量保留，静态翻译不修改源对象", () => {
  const key = messageKey("你好，{p0}");
  const first = createTextTranslator("en", { [key]: "Hello, {p0}" });
  const second = createTextTranslator("de", { [key]: "Hallo, {p0}" });
  assert.equal(first("你好，{p0}", { p0: "<张三>" }), "Hello, <张三>");
  assert.equal(second("你好，{p0}", { p0: "Alice" }), "Hallo, Alice");
  assert.equal(first("你好，{p0}", { p0: "Bob" }), "Hello, Bob");
  const original = { name: "你好，{p0}", code: "queued" };
  const result = localizeStatic(original, (source) => (source === "你好，{p0}" ? "Hello" : source));
  assert.deepEqual(original, { name: "你好，{p0}", code: "queued" });
  assert.equal(result.code, "queued");
});

test("夏令时按 IANA 时区解释同一时间点，不假定固定小时偏移", () => {
  const format = new Intl.DateTimeFormat("en", {
    timeZone: "America/New_York",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  });
  assert.equal(format.format(new Date("2026-03-08T06:30:00Z")), "01:30");
  assert.equal(format.format(new Date("2026-03-08T07:30:00Z")), "03:30");
});

test("空会话与非法存储不会覆盖已经确认的语言和时区", () => {
  const saved = { locale: "fr" as const, timezone: "Europe/Paris" };
  const merged = { ...saved, ...parseSavedPreferences({}) };
  assert.deepEqual(merged, saved);
  assert.deepEqual(parseSavedPreferences({ locale: "xx", timezone: "bad" }), {});
  assert.deepEqual(parseSavedPreferences(null), {});
  assert.deepEqual(parseSavedPreferences({ timezone: "UTC" }), { timezone: "UTC" });
});
