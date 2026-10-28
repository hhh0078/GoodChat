/** 文件作用说明：验证客户参数零值保留、渠道兼容去重与备注联合游标数据合并。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  channelTags,
  customerFields,
  displayValue,
  mergeNotes,
  noteFailureState,
  insertReplyText,
} from "../src/lib/workspace-display.ts";

test("额外参数保留零与false，当前会话覆盖旧资料并显示结构化值", () => {
  assert.deepEqual(
    customerFields(
      { 余额: 100, 姓名: "旧值" },
      { 余额: 0, 是否逾期: false, 姓名: "新值", 房产: { 数量: 0 } },
    ),
    { 余额: 0, 是否逾期: false, 姓名: "新值", 房产: { 数量: 0 } },
  );
  assert.equal(displayValue(0), "0");
  assert.equal(displayValue(false), "false");
  assert.equal(displayValue({ 数量: 0 }), '{"数量":0}');
  assert.equal(displayValue(null), "—");
  assert.deepEqual(customerFields(null, []), {});
});

test("备注503或断网保留已加载历史和游标，失权清缓存并允许恢复后重建分页", () => {
  const history = {
    items: [{ id: "older-note" }],
    cursor: { before: "old-time", before_id: "old-id" },
    paged: true,
  };
  assert.equal(noteFailureState(history, 503), history);
  assert.equal(noteFailureState(history, 0), history);
  assert.equal(noteFailureState(history, 429), history);
  for (const status of [401, 403, 404])
    assert.deepEqual(noteFailureState(history, status), { items: [], cursor: null, paged: false });
  assert.equal(history.items.length, 1);
});

test("显式插入话术追加原草稿，不覆盖未发送正文", () => {
  assert.equal(insertReplyText("未发送的原始草稿", "快捷内容"), "未发送的原始草稿\n快捷内容");
  assert.equal(insertReplyText("", "快捷内容"), "快捷内容");
});
test("渠道多标签去重并兼容旧入口单渠道快照", () => {
  assert.deepEqual(channelTags({ entry: { channel_tags: [" 官网 ", "推广", "官网", 0, null] } }), [
    "官网",
    "推广",
  ]);
  assert.deepEqual(channelTags({ entry: { channel_code: "旧渠道" } }), ["旧渠道"]);
  assert.deepEqual(channelTags({ entry: { channel_tags: [], channel_code: "旧渠道" } }), [
    "旧渠道",
  ]);
  assert.deepEqual(channelTags(undefined), []);
});
test("备注新页刷新不会清掉旧分页，同时间按编号稳定去重排序", () => {
  const old = [
    { id: "b", created_at: "2026-10-28T12:00:00Z", content: "原文" },
    { id: "old", created_at: "2026-10-27T12:00:00Z", content: "旧分页" },
  ];
  const incoming = [
    { id: "a", created_at: "2026-10-28T12:00:00Z", content: "同秒" },
    { id: "b", created_at: "2026-10-28T12:00:00Z", content: "更新" },
  ];
  assert.deepEqual(
    mergeNotes(old, incoming).map((note) => [note.id, note.content]),
    [
      ["b", "更新"],
      ["a", "同秒"],
      ["old", "旧分页"],
    ],
  );
  assert.equal(old[0].content, "原文");
});
