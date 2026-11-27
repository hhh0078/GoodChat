/** 文件作用说明：验证登录返回地址防止外部跳转，并确认入口参数编码保持原义。 */
import test from "node:test";
import assert from "node:assert/strict";
import {
  safeReturnPath,
  entryURL,
  loginReturnURL,
  conversationFromQuery,
} from "../src/lib/urls.ts";
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
/** 验证未登录打开邮件后保留会话与语言时区，而非登录后退回通用工作台。 */
test("登录返回保留邮件会话定位和偏好，不接受外部返回地址", () => {
  const conversation = "11a1e08d-91f6-40ac-b173-4feddfdd39b9";
  const search = `?conversation=${conversation}&lang=zh-CN&timezone=Asia%2FShanghai`;
  const login = new URL(loginReturnURL("/workspace", search), "https://goodchat.example");
  assert.equal(login.pathname, "/login");
  assert.equal(safeReturnPath(login.searchParams.get("next")), `/workspace${search}`);
  assert.equal(
    new URL(loginReturnURL("//evil.example", ""), "https://goodchat.example").searchParams.get(
      "next",
    ),
    "/console",
  );
});
/** 验证会话定位仅允许单个UUID，不把重复参数或路径当作API请求目标。 */
test("会话深链校验UUID并拒绝重复或注入参数", () => {
  const conversation = "11a1e08d-91f6-40ac-b173-4feddfdd39b9";
  assert.equal(conversationFromQuery(conversation), conversation);
  assert.equal(conversationFromQuery(conversation.toUpperCase()), conversation);
  for (const value of [
    undefined,
    null,
    "",
    "../../other",
    `${conversation}?tenant=other`,
    [conversation],
    [conversation, conversation],
  ])
    assert.equal(conversationFromQuery(value), "");
});
