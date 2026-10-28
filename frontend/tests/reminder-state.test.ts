/** 文件作用说明：回归偏好GET/PATCH乱序和已读/转出后旧提醒领取失效，避免静音与已读之后迟到播放。 */
import assert from "node:assert/strict";
import test from "node:test";
import { createReminderRequests, reminderEventInvalidates } from "../src/lib/reminder-state.ts";

test("慢GET不能覆盖其开始后已经保存的新偏好，保存失败后只能重新读取", async () => {
  const requests = createReminderRequests();
  const beforeSave = requests.preferenceRead();
  const finishSave = requests.preferenceChange();
  assert.equal(requests.preferenceChanging, true);
  assert.equal(beforeSave(), false);
  assert.equal(requests.preferenceRead()(), false);
  await Promise.resolve();
  assert.equal(finishSave(), true);
  assert.equal(requests.preferenceChanging, false);
  assert.equal(beforeSave(), false);
  assert.equal(requests.preferenceRead()(), true);
  assert.equal(finishSave(), true);
  assert.equal(requests.preferenceChanging, false);
});

test("并发保存只接受最后一次结果，先完成一项不能放行旧偏好领取", () => {
  const requests = createReminderRequests();
  const first = requests.preferenceChange();
  const second = requests.preferenceChange();
  assert.equal(second(), true);
  assert.equal(requests.preferenceChanging, true);
  assert.equal(first(), false);
  assert.equal(requests.preferenceChanging, false);
});

test("停止待播后已在途的领取响应失效，新领取仍可播放", async () => {
  const requests = createReminderRequests();
  const oldClaim = requests.attentionRead();
  requests.invalidateAttention();
  await Promise.resolve();
  assert.equal(oldClaim(), false);
  assert.equal(requests.attentionRead()(), true);
  const beforeMute = requests.attentionRead();
  const finishMute = requests.preferenceChange();
  finishMute();
  assert.equal(beforeMute(), false);
});

test("只将本人客服已读视作取消，访客和其他客服已读不能静音本人提醒", () => {
  for (const [participant_kind, participant_id, expected] of [
    ["agent", "me", true],
    ["agent", "other", false],
    ["visitor", "me", false],
  ] as const)
    assert.equal(
      reminderEventInvalidates(
        { event_type: "read.updated", payload: { participant_kind, participant_id } },
        "me",
      ),
      expected,
    );
});

test("转出、拒绝、关闭和撤回使旧领取失效，本人接单和访客新消息继续核验", () => {
  for (const event of [
    { event_type: "assignment.changed", payload: { agent_id: "other" } },
    { event_type: "assignment.changed", payload: { agent_id: null } },
    { event_type: "assignment.changed", payload: { accepted: false } },
    { event_type: "assignment.changed", payload: { status: "queued" } },
    { event_type: "conversation.closed" },
    { event_type: "message.withdrawn" },
  ])
    assert.equal(reminderEventInvalidates(event, "me"), true);
  for (const event of [
    { event_type: "assignment.changed", payload: { agent_id: "me" } },
    { event_type: "assignment.changed", payload: { accepted: true } },
    { event_type: "message.created", payload: { sender_kind: "visitor" } },
  ])
    assert.equal(reminderEventInvalidates(event, "me"), false);
});
