/** 文件作用说明：验证快捷回复个人优先、成功次数排序和富话术刷新重试保留版本及稳定编号。 */
import assert from "node:assert/strict";
import test from "node:test";
import { groupQuickReplies, type QuickReply } from "../src/lib/quick-replies.ts";
import { readPending, stagePending } from "../src/lib/pending-messages.ts";

/** reply 构造不包含真实客户或凭证的最小话术，仅测试分组及稳定排序。 */
function reply(
  id: string,
  scope: "personal" | "public",
  use_count: number,
  enabled = true,
): QuickReply {
  return {
    id,
    title: id,
    content: "文字",
    message_type: "text",
    options: [],
    scope,
    version: "1",
    use_count,
    enabled,
  };
}

test("个人先分区且各自按成功次数排序，同次稳定，空区和停用不产生伪造条目", () => {
  const source = [
    reply("公共常用", "public", 20),
    reply("个人新", "personal", 0),
    reply("个人常用", "personal", 3),
    reply("个人同次", "personal", 3),
    reply("停用", "personal", 99, false),
  ];
  const groups = groupQuickReplies(source);
  assert.deepEqual(
    groups.personal.map((item) => item.id),
    ["个人常用", "个人同次", "个人新"],
  );
  assert.deepEqual(
    groups.shared.map((item) => item.id),
    ["公共常用"],
  );
  assert.deepEqual(groupQuickReplies([source[0]]).personal, []);
  assert.equal(source[0].id, "公共常用");
});

test("快捷回复暂存保留原消息编号、话术版本及媒体预览，版本变化不能覆盖未确认请求", () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
  };
  const message = {
    client_message_id: "unchanged-client",
    content: "媒体说明",
    message_type: "audio",
    audience: "public",
    attachment_id: "preview-file",
    quick_reply_id: "11111111-2222-4333-8444-555555555555",
    quick_reply_version: "9007199254740993",
  };
  stagePending(storage, "scope", message);
  assert.deepEqual(readPending(storage, "scope"), [message]);
  assert.throws(
    () => stagePending(storage, "scope", { ...message, quick_reply_version: "2" }),
    /冲突/,
  );
  assert.equal(readPending(storage, "scope")[0].quick_reply_version, message.quick_reply_version);
  assert.throws(
    () => stagePending(storage, "broken", { ...message, quick_reply_version: "" }),
    /话术/,
  );
});
