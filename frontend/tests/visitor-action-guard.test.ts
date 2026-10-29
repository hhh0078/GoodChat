/** 文件作用说明：验证会话状态变化或模板重复调用时，官方表单不会丢失未完成的留言与评价。 */
import assert from "node:assert/strict";
import test from "node:test";
import { assertVisitorActionAvailable } from "../src/lib/visitor-action-guard.ts";

test("客服结束咨询后，模板评价调用不能清空仍打开的留言草稿", () => {
  assert.throws(
    () => assertVisitorActionAvailable("lead", "evaluation", "closed", false),
    /当前表单/,
  );
  assert.throws(
    () => assertVisitorActionAvailable("evaluation", "lead", "active", false),
    /当前表单/,
  );
});

test("重复打开同一表单不因提交中或会话变化重置草稿", () => {
  assert.doesNotThrow(() => assertVisitorActionAvailable("lead", "lead", "closed", true));
  assert.doesNotThrow(() =>
    assertVisitorActionAvailable("evaluation", "evaluation", "closed", true),
  );
});

test("空闲表单仍受留言与评价的会话状态限制", () => {
  assert.doesNotThrow(() => assertVisitorActionAvailable(null, "lead", "active", false));
  assert.doesNotThrow(() => assertVisitorActionAvailable(null, "evaluation", "closed", false));
  assert.throws(() => assertVisitorActionAvailable(null, "lead", "closed", false), /已结束/);
  assert.throws(() => assertVisitorActionAvailable(null, "evaluation", "active", false), /结束后/);
});

test("上传或提交进行中不能另开一个表单", () => {
  assert.throws(() => assertVisitorActionAvailable(null, "lead", "active", true), /当前操作/);
  assert.throws(() => assertVisitorActionAvailable(null, "evaluation", "closed", true), /当前操作/);
});
