/** 文件作用说明：验证消息先暂存再发送所需的稳定编号、刷新恢复、原会话确认清理及存储失败边界。 */
import assert from "node:assert/strict";
import test from "node:test";
import { acknowledgePending, readPending, stagePending } from "../src/lib/pending-messages.ts";

/** memoryStorage 用隔离存储模拟同标签刷新前后共享数据。 */
function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
}
const first = {
  client_message_id: "stable-id",
  content: "刷新时不能丢失的正文",
  message_type: "text",
  audience: "public",
  reply_to_id: "reply-id",
};

test("发送前暂存完整消息，刷新恢复和重试保留同一编号及附件引用", () => {
  const storage = memoryStorage();
  stagePending(storage, "tenant.actor.conversation-a", first);
  stagePending(storage, "tenant.actor.conversation-a", first);
  const attachment = {
    ...first,
    client_message_id: "attachment-id",
    content: "",
    message_type: "audio",
    attachment_id: "authorized-file-id",
  };
  stagePending(storage, "tenant.actor.conversation-a", attachment);
  assert.deepEqual(readPending(storage, "tenant.actor.conversation-a"), [first, attachment]);
  assert.throws(
    () =>
      stagePending(storage, "tenant.actor.conversation-a", { ...first, content: "禁止覆盖原文" }),
    /冲突/,
  );
  assert.equal(readPending(storage, "tenant.actor.conversation-a")[0].content, first.content);
});
test("切会话后的旧请求确认只清理原会话；补拉确认也以原编号清理", () => {
  const storage = memoryStorage();
  stagePending(storage, "old-scope", first);
  stagePending(storage, "new-scope", { ...first, client_message_id: "new-id" });
  acknowledgePending(storage, "old-scope", ["stable-id"]);
  assert.deepEqual(readPending(storage, "old-scope"), []);
  assert.equal(readPending(storage, "new-scope")[0].client_message_id, "new-id");
  acknowledgePending(storage, "new-scope", ["unknown-server-id"]);
  assert.equal(readPending(storage, "new-scope").length, 1);
  acknowledgePending(storage, "new-scope", ["new-id"]);
  assert.equal(storage.getItem("new-scope"), null);
});
test("存储被禁用、配额不足和损坏数据明确报错，不覆盖待确认原文", () => {
  const disabled = {
    getItem: () => {
      throw new Error("denied");
    },
    setItem: () => {},
    removeItem: () => {},
  };
  assert.throws(() => stagePending(disabled, "scope", first), /禁止消息暂存/);
  const storage = memoryStorage();
  stagePending(storage, "scope", first);
  const full = {
    ...storage,
    setItem: () => {
      throw new Error("quota");
    },
  };
  assert.throws(
    () => stagePending(full, "scope", { ...first, client_message_id: "second" }),
    /当前草稿会保留/,
  );
  assert.deepEqual(readPending(storage, "scope"), [first]);
  storage.setItem("corrupt", "{invalid");
  assert.throws(() => stagePending(storage, "corrupt", first), /未覆盖原数据/);
  assert.equal(storage.getItem("corrupt"), "{invalid");
});
test("暂存只收集消息字段且限制数量和容量，绝不保存请求令牌", () => {
  const storage = memoryStorage();
  stagePending(storage, "scope", { ...first, token: "should-not-save" } as typeof first);
  assert.equal(storage.getItem("scope")?.includes("should-not-save"), false);
  for (let index = 1; index < 100; index++)
    stagePending(storage, "scope", { ...first, client_message_id: String(index) });
  assert.throws(
    () => stagePending(storage, "scope", { ...first, client_message_id: "over-limit" }),
    /暂存已满/,
  );
  assert.equal(readPending(storage, "scope").length, 100);
  assert.throws(
    () => stagePending(storage, "oversize", { ...first, content: "字".repeat(1024 * 1024) }),
    /暂存已满/,
  );
  assert.equal(storage.getItem("oversize"), null);
});
