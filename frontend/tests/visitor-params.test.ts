/** 文件作用说明：验证接入 JSON 类型保留、重复参数拒绝及展示资料不会生成身份字段。 */
import assert from "node:assert/strict";
import test from "node:test";
import { parseVisitorParameters, canRestartVisit } from "../src/lib/visitor-params.ts";
test("保留业务0和false，手机号只进入展示资料", () => {
  const result = parseVisitorParameters(
    new URLSearchParams({
      phone: "13800000000",
      name: "访客",
      context: JSON.stringify({ total: 0, paid: false }),
      attributes: JSON.stringify({ level: 0 }),
      utm_campaign: "秋季",
    }),
  );
  assert.equal(result.context.total, 0);
  assert.equal(result.context.paid, false);
  assert.equal(result.profile.phone, "13800000000");
  assert.equal(result.tracking.utm_campaign, "秋季");
  assert.equal("visitor_token" in result, false);
});
test("拒绝重复、冲突和非对象JSON", () => {
  assert.throws(() => parseVisitorParameters(new URLSearchParams("phone=1&phone=2")), /重复/);
  assert.throws(
    () => parseVisitorParameters(new URLSearchParams({ name: "A", display_name: "B" })),
    /冲突/,
  );
  for (const raw of ["null", "[]", "false", "{broken"]) {
    assert.throws(() => parseVisitorParameters(new URLSearchParams({ context: raw })), /JSON/);
  }
});

test("首次访问冲突只建议明确新访问，不把任意失败当身份恢复", () => {
  assert.equal(canRestartVisit({ status: 409, code: "visit_conflict" }), true);
  for (const error of [
    null,
    { status: 401, code: "visit_conflict" },
    { status: 409, code: "version_conflict" },
    { status: 500, code: "internal" },
  ])
    assert.equal(canRestartVisit(error), false);
});

test("中文字段与英文别名规范资料，性别兼容man且额外字段逐项保留", () => {
  const result = parseVisitorParameters(
    new URLSearchParams({
      姓名: "张三&+%#",
      手机号: "13800000000",
      头像: "https://example.com/头像.png",
      sex: "man",
      extra: JSON.stringify({ 有无房产: "有", 是否逾期: "无", 额度: 0, 已验证: false, 空值: null }),
      工单编号: "单号+A&B#C",
      tenant_id: "不得作为业务资料或身份",
      visitor_token: "不得从URL恢复身份",
    }),
  );
  assert.equal(result.profile.display_name, "张三&+%#");
  assert.equal(result.profile.phone, "13800000000");
  assert.equal(result.profile.avatar_url, "https://example.com/头像.png");
  assert.deepEqual(result.context, {
    有无房产: "有",
    是否逾期: "无",
    额度: 0,
    已验证: false,
    空值: null,
    性别: "男",
    工单编号: "单号+A&B#C",
  });
  assert.equal("visitor_token" in result.context, false);
  assert.equal("tenant_id" in result.context, false);
});

test("性别同义值可共存，冲突别名与损坏extra明确报错", () => {
  assert.equal(
    parseVisitorParameters(new URLSearchParams({ 性别: "男", sex: "male" })).context.性别,
    "男",
  );
  for (const [input, expected] of [
    ["woman", "女"],
    ["female", "女"],
    ["unknown", "未知"],
    ["0", "未知"],
  ])
    assert.equal(
      parseVisitorParameters(new URLSearchParams({ sex: input })).context.性别,
      expected,
    );
  const conflictingAliases: Record<string, string>[] = [
    { 姓名: "张三", name: "李四" },
    { 手机号: "1", phone: "2" },
    { 性别: "女", sex: "man" },
  ];
  for (const values of conflictingAliases)
    assert.throws(() => parseVisitorParameters(new URLSearchParams(values)), /冲突/);
  for (const extra of ["null", "[]", "false", "{broken"])
    assert.throws(() => parseVisitorParameters(new URLSearchParams({ extra })), /JSON/);
  assert.throws(
    () =>
      parseVisitorParameters(new URLSearchParams({ context: '{"等级":1}', extra: '{"等级":2}' })),
    /冲突/,
  );
});

test("特殊业务键不会污染对象原型，JSON与中文链接标准编码后能完整还原", () => {
  const input = new URLSearchParams({
    姓名: "张三 + & # %",
    extra: '{"__proto__":{"polluted":true},"constructor":"原文"}',
  });
  const result = parseVisitorParameters(new URLSearchParams(input.toString()));
  assert.equal(result.profile.display_name, "张三 + & # %");
  assert.equal(Object.prototype.hasOwnProperty.call(result.context, "__proto__"), true);
  assert.deepEqual(result.context["__proto__"], { polluted: true });
  assert.equal(({} as Record<string, unknown>).polluted, undefined);
});
