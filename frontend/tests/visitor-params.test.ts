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
