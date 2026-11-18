/** 文件作用说明：验证双端输入提示的固定200秒期限、重连恢复、人工接管与辅助模式可见性。 */
import test from "node:test";
import assert from "node:assert/strict";
import { peerTypingState } from "../src/lib/typing-state.ts";
import type { Conversation } from "../src/lib/types.ts";
const start = Date.parse("2026-11-19T00:00:00Z");
const conversation = {
  id: "chat",
  contact_id: "visitor",
  status: "queued",
  context: { ai_thinking: { mode: "auto", expires_at: new Date(start + 200000).toISOString() } },
} as Conversation;
test("AI在双端显示、刷新仍沿用原期限，200秒立即结束", () => {
  for (const kind of ["visitor", "agent"]) {
    assert.equal(peerTypingState(conversation, kind, 0, start).until, start + 200000);
    assert.equal(peerTypingState(conversation, kind, 0, start + 100000).until, start + 200000);
    assert.deepEqual(peerTypingState(conversation, kind, 0, start + 200000), {
      until: 0,
      isAIThinking: false,
    });
  }
});
test("人工接管、关闭或任务结束立即清除AI，辅助草稿只对客服显示", () => {
  assert.equal(peerTypingState({ ...conversation, ai_paused: true }, "visitor", 0, start).until, 0);
  assert.equal(
    peerTypingState({ ...conversation, status: "closed" }, "agent", start + 5000, start).until,
    0,
  );
  assert.equal(peerTypingState({ ...conversation, context: {} }, "visitor", 0, start).until, 0);
  const assistant = {
    ...conversation,
    context: {
      ai_thinking: { mode: "assistant", expires_at: new Date(start + 200000).toISOString() },
    },
  };
  assert.equal(peerTypingState(assistant, "visitor", 0, start).until, 0);
  assert.equal(peerTypingState(assistant, "agent", 0, start).isAIThinking, true);
  assert.equal(peerTypingState(conversation, "agent", start + 5000, start + 200000).until, 0);
});
