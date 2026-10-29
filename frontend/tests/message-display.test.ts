/** 文件作用说明：验证自动客服回复的双端展示、真实归属权限与装修头像授权，普通系统消息保持原义。 */
import assert from "node:assert/strict";
import test from "node:test";
import { isAutomaticReply, messageDisplay } from "../src/lib/message-display.ts";
import {
  publicBridgeMessage,
  requestBridgeAvatar,
  validateBridgeRequest,
  visibleBridgeAvatar,
} from "../src/lib/frame-bridge.ts";
import type { ChatMessage } from "../src/lib/types.ts";

/** automaticMessage 构造具有明确系统自动来源及历史客服身份的公开消息用于展示测试。 */
function automaticMessage(source = "welcome"): ChatMessage {
  return {
    id: "auto",
    sender_kind: "system",
    sender_id: "tenant",
    sender_name: "系统",
    source,
    content: "你好",
    message_type: "text",
    seq: "2",
    event_seq: "2",
    audience: "public",
    created_at: "2026-10-29T10:00:00Z",
    automation_agent: {
      id: "agent-a",
      name: "客服甲",
      avatar_url: "/images/avatars/default-02.png",
    },
  };
}

const transferredContext = {
  first_agent: { id: "agent-a", name: "客服甲", avatar_url: "/images/avatars/default-02.png" },
  agent_name: "客服乙",
  agent_avatar_url: "/images/avatars/default-03.png",
};

test("欢迎和关键词消息仅改变视觉阵营，客服右侧保留自动来源，访客左侧显示客服姓名头像", () => {
  for (const source of ["welcome", "keyword"]) {
    const item = automaticMessage(source);
    const agent = messageDisplay(item, { kind: "agent", actorId: "agent-a" }, transferredContext);
    const visitor = messageDisplay(
      item,
      { kind: "visitor", actorId: "visitor" },
      transferredContext,
    );
    assert.equal(agent.alignRight, true);
    assert.equal(agent.own, false);
    assert.match(agent.sourceLabel, /系统自动/);
    assert.equal(visitor.alignRight, false);
    assert.equal(visitor.own, false);
    assert.equal(visitor.senderName, "客服甲");
    assert.equal(visitor.avatarURL, "/images/avatars/default-02.png");
    assert.equal(visitor.sourceLabel, "");
    assert.equal(visitor.senderKind, "agent");
    assert.equal(item.sender_kind, "system");
    assert.equal(item.sender_name, "系统");
    assert.equal(item.source, source);
  }
});

test("普通状态系统消息、内部消息和带同名source的人工消息不会被伪装为客服自动回复", () => {
  for (const item of [
    automaticMessage("status"),
    automaticMessage(""),
    { ...automaticMessage(), audience: "internal" },
    { ...automaticMessage(), sender_kind: "agent" },
  ]) {
    assert.equal(isAutomaticReply(item), false);
    assert.equal(messageDisplay(item, { kind: "agent", actorId: "unrelated" }).alignRight, false);
  }
  const system = messageDisplay(automaticMessage("status"), {
    kind: "visitor",
    actorId: "visitor",
  });
  assert.equal(system.senderName, "系统");
  assert.equal(system.senderKind, "system");
  const spoofedId = messageDisplay(
    { ...automaticMessage(), sender_id: "agent-a" },
    { kind: "agent", actorId: "agent-a" },
  );
  assert.equal(spoofedId.own, false);
});

test("创建时尚未接单的欢迎使用首次接待身份兜底，转接不改历史客服，缺头像不混用后来客服头像", () => {
  const item = { ...automaticMessage(), automation_agent: null };
  assert.equal(
    messageDisplay(item, { kind: "visitor", actorId: "v" }, transferredContext).senderName,
    "客服甲",
  );
  assert.equal(messageDisplay(item, { kind: "visitor", actorId: "v" }).senderName, "客服");
  const noAvatar = { ...item, automation_agent: { id: "agent-a", name: "客服甲" } };
  assert.equal(
    messageDisplay(noAvatar, { kind: "visitor", actorId: "v" }, transferredContext).avatarURL,
    undefined,
  );
  const human = {
    ...automaticMessage(),
    sender_kind: "agent",
    sender_id: "agent-a",
    sender_name: "客服甲",
    sender_avatar_url: "/images/avatars/default-02.png",
  };
  assert.equal(messageDisplay(human, { kind: "agent", actorId: "agent-a" }).own, true);
});

test("装修桥向访客仅导出客服展示信息并隐藏自动来源，原始系统记录与撤回权限保留", () => {
  const item = automaticMessage("keyword");
  const result = publicBridgeMessage(item, "visitor", transferredContext);
  assert.equal(result.sender_kind, "agent");
  assert.equal(result.sender_name, "客服甲");
  assert.equal(result.sender_avatar_url, "/images/avatars/default-02.png");
  assert.equal(result.source, undefined);
  assert.equal(result.is_own, false);
  assert.equal("automation_agent" in result, false);
  assert.equal("sender_id" in result, false);
  assert.throws(
    () => validateBridgeRequest("withdrawMessage", { message_id: item.id }, [item], "visitor"),
    /本人/,
  );
  assert.equal(publicBridgeMessage(automaticMessage("status"), "visitor").source, "status");
});

test("装修自动消息头像仍绑定当前公开消息及受控图片，撤回、内部和危险地址拒绝读取", async () => {
  const item = automaticMessage();
  assert.equal(visibleBridgeAvatar([item], item.id), "/images/avatars/default-02.png");
  for (const invalid of [
    { ...item, audience: "internal" },
    { ...item, withdrawn_at: "2026-10-29T11:00:00Z" },
    {
      ...item,
      automation_agent: { name: "客服甲", avatar_url: "https://bad.invalid/private.png" },
    },
  ])
    assert.throws(() => visibleBridgeAvatar([invalid], item.id));
  const pending = { ...item, automation_agent: null };
  assert.equal(
    visibleBridgeAvatar([pending], item.id, transferredContext),
    "/images/avatars/default-02.png",
  );
  const original = globalThis.fetch;
  let context = transferredContext;
  try {
    globalThis.fetch = async (path) => {
      assert.equal(path, "/images/avatars/default-02.png");
      context = {
        ...transferredContext,
        first_agent: {
          id: "different",
          name: "另一个客服",
          avatar_url: "/images/avatars/default-04.png",
        },
      };
      return new Response(new Blob(["png"], { type: "image/png" }));
    };
    await assert.rejects(
      requestBridgeAvatar(
        item.id,
        () => [pending],
        undefined,
        () => context,
      ),
      /权限已变化/,
    );
  } finally {
    globalThis.fetch = original;
  }
});

test("客服没有头像时自动回复沿用姓名首字展示，装修桥不授权不存在的头像", () => {
  const item = { ...automaticMessage(), automation_agent: { id: "agent-a", name: "客服甲" } };
  const display = messageDisplay(item, { kind: "visitor", actorId: "visitor" }, transferredContext);
  assert.equal(display.senderName, "客服甲");
  assert.equal(display.avatarURL, undefined);
  assert.equal(
    publicBridgeMessage(item, "visitor", transferredContext).sender_avatar_url,
    undefined,
  );
  assert.throws(() => visibleBridgeAvatar([item], item.id, transferredContext), /头像不属于/);
  assert.throws(
    () =>
      validateBridgeRequest(
        "getAvatar",
        { message_id: item.id },
        [item],
        "visitor",
        transferredContext,
      ),
    /头像不属于/,
  );
  const unassigned = { ...automaticMessage(), automation_agent: null };
  assert.equal(
    messageDisplay(unassigned, { kind: "visitor", actorId: "visitor" }).avatarURL,
    undefined,
  );
});
