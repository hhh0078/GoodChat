/** 文件作用说明：验证消息合并的去重、撤回覆盖和超过安全整数范围的顺序。 */
import assert from "node:assert/strict";
import test from "node:test";
import { mergeMessages, withdrawMessages } from "../src/lib/chat-state.ts";
test("合并更新和重试不重复消息，允许内部事件造成的序号空洞", () => {
  const items = mergeMessages(
    [
      { id: "b", seq: "13", content: "旧正文" },
      { id: "a", seq: "2", content: "你好" },
    ],
    [
      { id: "b", seq: "13", content: "", withdrawn_at: "2026-10-26" },
      { id: "c", seq: "19", content: "回复" },
    ],
  );
  assert.deepEqual(
    items.map((item) => item.id),
    ["a", "b", "c"],
  );
  assert.equal(items[1].content, "");
});
test("超大字符串序号不转换成丢精度的数字", () => {
  const items = mergeMessages(
    [],
    [
      { id: "b", seq: "9007199254740993" },
      { id: "a", seq: "9007199254740992" },
    ],
  );
  assert.deepEqual(
    items.map((item) => item.id),
    ["a", "b"],
  );
});
test("确认撤回同时清除原文附件和所有已加载引用，保留其他消息", () => {
  const messages = [
    { id: "original", content: "敏感正文", attachment: { id: "file-a" } },
    {
      id: "reply",
      content: "回复",
      reply: { id: "original", content: "敏感正文", withdrawn: false },
    },
    { id: "other", content: "保留消息" },
  ];
  const result = withdrawMessages(messages, "original", "2026-10-27T08:00:00Z");
  assert.equal(result[0].content, "");
  assert.equal(result[0].attachment, undefined);
  assert.equal(result[0].withdrawn_at, "2026-10-27T08:00:00Z");
  assert.deepEqual(result[1].reply, { id: "original", content: "", withdrawn: true });
  assert.equal(result[2].content, "保留消息");
  assert.equal(messages[0].content, "敏感正文");
});
