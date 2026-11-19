/** 文件作用说明：验证九语言公开目录、HTML/XML替代链接一致性和免费软件收费边界的结构化数据。 */
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { locales, messageKey } from "../src/i18n/preferences.ts";
import {
  publicLanguagePaths,
  publicPagePath,
  publicPagePaths,
  publicSitemap,
  publicSoftwareData,
} from "../src/lib/public-seo.ts";

test("XML完整覆盖九语言九类公开页面，排除业务与账号地址", () => {
  const origin = "https://goodchat.example/";
  const entries = publicSitemap(origin);
  assert.equal(entries.length, 81);
  assert.equal(new Set(entries.map((entry) => entry.url)).size, 81);
  for (const locale of locales) {
    for (const path of publicPagePaths) {
      const expected = new URL(publicPagePath(locale, path), origin).href;
      const entry = entries.find((row) => row.url === expected);
      assert.ok(entry);
      assert.deepEqual(
        entry.alternates.languages,
        Object.fromEntries(
          Object.entries(publicLanguagePaths(path)).map(([code, target]) => [
            code,
            new URL(target, origin).href,
          ]),
        ),
      );
      assert.equal(Object.keys(entry.alternates.languages).length, 10);
      assert.equal(entry.alternates.languages[locale], expected);
      assert.equal(
        entry.alternates.languages["x-default"],
        new URL(publicPagePath("en", path), origin).href,
      );
      assert.equal(new URL(entry.url).search, "");
      assert.doesNotMatch(entry.url, /\/console|\/platform|\/workspace|\/login|\/register|\/c\//);
    }
  }
});

test("同一页面的语言替代集双向一致，不因当前语言改变", () => {
  for (const path of publicPagePaths) {
    const alternatives = publicLanguagePaths(path);
    for (const locale of locales) {
      assert.equal(alternatives[locale], publicPagePath(locale, path));
      assert.deepEqual(Object.keys(alternatives).sort(), [...locales, "x-default"].sort());
    }
  }
});

test("九语言SEO和结构化数据明确系统免费、AI按量收费且不虚构评分", () => {
  for (const locale of locales) {
    const catalog = JSON.parse(
      fs.readFileSync(new URL(`../src/i18n/messages/${locale}.json`, import.meta.url), "utf8"),
    );
    const title = catalog[messageKey("AI 客服系统")];
    const terms = catalog[messageKey("系统与 2 个基础坐席免费，AI 调用按实际 Token 计费。")];
    const description =
      catalog[
        messageKey(
          "GoodChat AI 客服支持知识库自动回复、图片识别、媒体话术与人工接管。小团队基础系统免费，含 2 个客服坐席；AI 调用按实际 Token 计费。",
        )
      ];
    assert.ok(title && description && terms);
    assert.match(terms, /2/);
    assert.match(terms, /token|トークン|토큰/i);
    if (locale !== "zh-CN") assert.notEqual(title, "AI 客服系统");
    const data = publicSoftwareData("https://goodchat.example", locale, title, description, terms);
    const app = data["@graph"][1];
    assert.equal(app["@type"], "SoftwareApplication");
    assert.equal(app.url, `https://goodchat.example/${locale}`);
    assert.equal(app.offers?.price, "0");
    assert.equal(app.offers?.priceCurrency, "CNY");
    assert.equal(app.offers?.description, terms);
    assert.equal(app.offers?.url, `https://goodchat.example/${locale}/pricing`);
    assert.equal("aggregateRating" in app, false);
    assert.equal("review" in app, false);
  }
});
