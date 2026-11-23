/** 文件作用说明：验证目标成员与过期转接边界，避免向错误身份或不可接待同事显示确认操作。 */
import assert from "node:assert/strict";
import test from "node:test";
import { canTransferTo, isTransferRecipient, replyGate } from "../src/lib/assignment-state.ts";
test("回复前置动作与自动分配、手动领取和指定客服保持一致", () => {
  const conversation = { status: "queued", reception_mode: "auto", contact_online: false };
  assert.equal(replyGate(conversation, "me"), "offline");
  assert.equal(replyGate({ ...conversation, contact_online: true }, "me"), "auto");
  assert.equal(replyGate({ ...conversation, reception_mode: "manual" }, "me"), "claim");
  assert.equal(
    replyGate({ ...conversation, reception_mode: "manual", context: { ai_receiving: true } }, "me"),
    "ai",
  );
  assert.equal(
    replyGate(
      {
        ...conversation,
        reception_mode: "manual",
        context: { ai_receiving: true },
        ai_paused: true,
      },
      "me",
    ),
    "claim",
  );
  assert.equal(
    replyGate({ ...conversation, reception_mode: "manual", desired_agent_id: "other" }, "me"),
    "specified",
  );
  assert.equal(
    replyGate({ ...conversation, current_agent_id: "other", status: "active" }, "me"),
    "other",
  );
  assert.equal(
    replyGate({ ...conversation, current_agent_id: "me", status: "assigned" }, "me"),
    "accept",
  );
  assert.equal(
    replyGate({ ...conversation, current_agent_id: "me", status: "active" }, "me"),
    "ready",
  );
  assert.equal(replyGate({ ...conversation, status: "closed" }, "me"), "closed");
});
test("转接确认只对正确目标与有效预留显示", () => {
  const transfer = {
    status: "pending",
    target_agent_id: "member-b",
    expires_at: "2026-10-26T10:00:00Z",
  };
  const now = new Date("2026-10-26T09:59:59Z").getTime();
  assert.equal(isTransferRecipient(transfer, "member-b", now), true);
  assert.equal(isTransferRecipient(transfer, "member-a", now), false);
  assert.equal(isTransferRecipient(transfer, "member-b", now + 2000), false);
  assert.equal(isTransferRecipient({ ...transfer, status: "accepted" }, "member-b", now), false);
});
test("离线示忙与最后一个容量已占用的同事不可选为转接目标", () => {
  const member = { online: true, available: true, busy: false, current_load: 4, capacity: 5 };
  assert.equal(canTransferTo(member), true);
  for (const patch of [
    { online: false },
    { available: false },
    { busy: true },
    { current_load: 5 },
    { capacity: 0 },
    { current_load: -1 },
  ])
    assert.equal(canTransferTo({ ...member, ...patch }), false);
});
