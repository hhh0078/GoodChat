/** 文件作用说明：验证访客提示只针对新人工消息，以及五段提示音为不同的真实1秒PCM资源。 */
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { reminderWatermark } from "../src/lib/message-reminders.ts";
import { notificationSound, notificationSounds } from "../src/lib/notification-sounds.ts";
import type { ChatMessage } from "../src/lib/types.ts";

/** message 生成测试消息元数据，不绕过真实业务发送或用于产品展示。 */
function message(seq: string, sender_kind = "agent", audience = "public") {
  return { id: seq, seq, sender_kind, audience } as ChatMessage;
}

test("初始快照与加载更早历史不响，新客服消息补拉一次只提示一次", () => {
  const initial = reminderWatermark([message("9007199254740993")], null);
  assert.equal(initial.notify, false);
  assert.equal(initial.latest, "9007199254740993");
  assert.equal(
    reminderWatermark([message("1"), message(initial.latest)], initial.latest).notify,
    false,
  );
  const fresh = reminderWatermark([message("9007199254740994")], initial.latest);
  assert.equal(fresh.notify, true);
  assert.equal(reminderWatermark([message("9007199254740994")], fresh.latest).notify, false);
});

test("访客本人、系统、内部和撤回消息不触发访客提示音", () => {
  for (const item of [
    message("2", "visitor"),
    message("2", "system"),
    message("2", "agent", "internal"),
    { ...message("2"), withdrawn_at: "2026-10-29T00:00:00Z" },
  ]) {
    assert.equal(reminderWatermark([item], "1").notify, false);
  }
});

test("五个受控提示音均为1秒PCM且内容各异，未知编号不形成任意网络路径", () => {
  assert.equal(notificationSound("https://bad.invalid/audio").id, "gentle");
  const hashes = new Set<string>();
  for (const sound of notificationSounds) {
    const raw = readFileSync(new URL("../public" + sound.url, import.meta.url));
    assert.equal(raw.subarray(0, 4).toString(), "RIFF");
    assert.equal(raw.subarray(8, 12).toString(), "WAVE");
    assert.equal(raw.readUInt16LE(20), 1);
    assert.equal(raw.readUInt16LE(22), 1);
    assert.equal(raw.readUInt32LE(40) / raw.readUInt32LE(28), 1);
    hashes.add(createHash("sha256").update(raw).digest("hex"));
  }
  assert.equal(hashes.size, 5);
});
