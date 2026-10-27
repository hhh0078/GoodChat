/** 文件作用说明：验证登录返回地址防止外部跳转，并确认入口参数编码保持原义。 */
import test from "node:test";
import assert from "node:assert/strict";
import { safeReturnPath, entryURL } from "../src/lib/urls.ts";
test("只允许站内返回地址", () => {
  for (const value of [
    "https://evil.example",
    "//evil.example",
    "/\\evil.example",
    "/\ninvalid",
    null,
  ])
    assert.equal(safeReturnPath(value), "/console");
  assert.equal(safeReturnPath("/console/members?tab=invite"), "/console/members?tab=invite");
});
test("入口参数通过标准URL编码而不拆散JSON", () => {
  const context = JSON.stringify({ name: "客户&订单", value: "a=b#c" });
  const url = new URL(entryURL("https://goodchat.example", "entry123", { context }));
  assert.equal(url.pathname, "/c/entry123");
  assert.equal(url.searchParams.get("context"), context);
  assert.equal(url.searchParams.size, 1);
});
