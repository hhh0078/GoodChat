/** 文件作用说明：验证装修来源和桥接握手拒绝同源代码、错误窗口、版本和发布编号。 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  publicBridgeMessage,
  requestBridgeAvatar,
  requestBridgeAttachment,
  safeRenderURL,
  validBridgeHandshake,
  validateBridgeRequest,
  visibleBridgeAttachment,
  visibleBridgeAvatar,
} from "../src/lib/frame-bridge.ts";
import type { ChatMessage } from "../src/lib/types.ts";
import { ApiError } from "../src/lib/api.ts";

test("装修头像读取绑定当前公开消息，拒绝任意路径和越权消息", () => {
  const base = {
    id: "current",
    audience: "public",
    sender_avatar_url: "/images/avatars/default-01.png",
  } as ChatMessage;
  assert.equal(visibleBridgeAvatar([base], "current"), base.sender_avatar_url);
  assert.doesNotThrow(() =>
    validateBridgeRequest("getAvatar", { message_id: "current" }, [base], "visitor"),
  );
  for (const item of [
    { ...base, audience: "internal" },
    { ...base, withdrawn_at: "2026-10-28T10:00:00Z" },
    ...[
      "https://example.com/a.png",
      "/api/v1/attachments/private",
      "/images/avatars/../private",
      "/images/avatars/default-51.png",
      "/images/avatars/default-01.png?token=secret",
    ].map((sender_avatar_url) => ({ ...base, sender_avatar_url })),
  ])
    assert.throws(() => visibleBridgeAvatar([item], "current"));
  assert.throws(() => visibleBridgeAvatar([base], "other-conversation"));
  assert.throws(() =>
    validateBridgeRequest(
      "getAvatar",
      { message_id: "current", url: "/private" },
      [base],
      "visitor",
    ),
  );
});

test("头像桥只读取受控PNG且不携带身份凭证，下载完成再次验证可见性", async () => {
  const original = globalThis.fetch;
  const base = {
    id: "current",
    audience: "public",
    sender_avatar_url: "/api/v1/avatar-assets/11111111-1111-1111-1111-111111111111",
  } as ChatMessage;
  let messages = [base],
    requests = 0;
  try {
    globalThis.fetch = async (input, options) => {
      requests++;
      assert.equal(input, base.sender_avatar_url);
      assert.equal(options?.credentials, "omit");
      assert.equal(options?.redirect, "error");
      assert.equal(options?.headers, undefined);
      return new Response(new Blob(["controlled-png"], { type: "image/png" }));
    };
    assert.equal((await requestBridgeAvatar("current", () => messages)).type, "image/png");
    await assert.rejects(requestBridgeAvatar("other", () => messages));
    assert.equal(requests, 1);
    globalThis.fetch = async () =>
      new Response("<html>", { headers: { "Content-Type": "text/html" } });
    await assert.rejects(
      requestBridgeAvatar("current", () => messages),
      /格式/,
    );
    globalThis.fetch = async () =>
      new Response(new Blob([new Uint8Array(256 * 1024 + 1)], { type: "image/png" }));
    await assert.rejects(
      requestBridgeAvatar("current", () => messages),
      /格式/,
    );
    globalThis.fetch = async () => {
      messages = [{ ...base, withdrawn_at: "2026-10-28T10:00:00Z" }];
      return new Response(new Blob(["png"], { type: "image/png" }));
    };
    await assert.rejects(
      requestBridgeAvatar("current", () => messages),
      /可见/,
    );
  } finally {
    globalThis.fetch = original;
  }
});

test("装修消息仅公开安全头像地址，不导出私有附件、危险协议或带凭证URL", () => {
  const message = { id: "avatar-message", sender_kind: "agent", content: "你好" } as ChatMessage;
  for (const url of [
    "/images/avatars/default-01.png",
    "/api/v1/avatar-assets/11111111-1111-1111-1111-111111111111",
    "https://images.example.com/avatar.png",
  ]) {
    assert.equal(
      publicBridgeMessage({ ...message, sender_avatar_url: url }, "visitor").sender_avatar_url,
      url,
    );
  }
  for (const url of [
    "javascript:alert(1)",
    "data:image/svg+xml,x",
    "https://name:secret@example.com/avatar.png",
    "/api/v1/attachments/private",
    "//other.example.com/avatar.png",
  ]) {
    assert.equal(
      publicBridgeMessage({ ...message, sender_avatar_url: url }, "visitor").sender_avatar_url,
      undefined,
    );
  }
});
test("装修来源必须跨源且不接受危险协议或内嵌凭证", () => {
  assert.equal(
    safeRenderURL("https://code.example.net/render/release", "https://app.example.com")?.hostname,
    "code.example.net",
  );
  for (const value of [
    "https://app.example.com/render",
    "http://code.example.net/render",
    "javascript:alert(1)",
    "data:text/html,abc",
    "https://user:password@code.example.net/render",
  ]) {
    assert.equal(safeRenderURL(value, "https://app.example.com"), null);
  }
});
test("精确来源和窗口与协议均正确才允许握手", () => {
  const origin = "https://code.example.net";
  const input = {
    type: "goodchat:init",
    version: 1,
    nonce: "12345678-1234-1234-1234-123456789012",
    release_id: "release-a",
  };
  assert.equal(validBridgeHandshake(input, origin, origin, true, "release-a"), true);
  assert.equal(
    validBridgeHandshake(input, "https://evil.example.net", origin, true, "release-a"),
    false,
  );
  assert.equal(validBridgeHandshake(input, origin, origin, false, "release-a"), false);
  assert.equal(
    validBridgeHandshake({ ...input, version: 2 }, origin, origin, true, "release-a"),
    false,
  );
  assert.equal(validBridgeHandshake(input, origin, origin, true, "release-b"), false);
  assert.equal(
    validBridgeHandshake({ ...input, nonce: "short" }, origin, origin, true, "release-a"),
    false,
  );
});
test("装修消息只带本人标记和可见字段，撤回内容与附件凭证不会出站", () => {
  const message = {
    id: "message-a",
    seq: "9007199254740993",
    event_seq: "9007199254740993",
    sender_kind: "visitor",
    sender_id: "visitor-a",
    content: "消息内容",
    message_type: "text",
    created_at: "2026-10-27T08:00:00Z",
    secret: "不得输出",
    reply: { id: "reply-a", content: "已撤回内容", withdrawn: true },
    attachment: {
      id: "file-a",
      file_name: "附件.txt",
      content_type: "text/plain",
      size_bytes: 12,
      url: "带凭证的内部地址",
    },
  } as ChatMessage;
  const result = publicBridgeMessage(message, "visitor-a");
  assert.equal(result.is_own, true);
  assert.equal(result.seq, "9007199254740993");
  assert.equal("sender_id" in result, false);
  assert.equal("secret" in result, false);
  assert.equal("url" in (result.attachment || {}), false);
  assert.equal(result.reply?.content, "");
  assert.equal(
    publicBridgeMessage({ ...message, sender_kind: "agent" }, "visitor-a").is_own,
    false,
  );
  assert.equal(publicBridgeMessage(message, "visitor-b").is_own, false);
  const withdrawn = publicBridgeMessage(
    { ...message, withdrawn_at: "2026-10-27T08:01:00Z" },
    "visitor-a",
  );
  assert.equal(withdrawn.content, "");
  assert.equal(withdrawn.text, "");
  assert.equal(withdrawn.attachment, undefined);
});
test("RPC白名单拒绝任意路径、会话参数和跨会话引用或撤回", () => {
  const own = { id: "own", sender_kind: "visitor", sender_id: "visitor-a" } as ChatMessage;
  const other = { id: "other", sender_kind: "agent", sender_id: "agent-a" } as ChatMessage;
  const internal = { ...own, id: "internal", audience: "internal" };
  const withdrawn = { ...own, id: "withdrawn", withdrawn_at: "2026-10-27T08:00:00Z" };
  const messages = [own, other, internal, withdrawn];
  assert.doesNotThrow(() =>
    validateBridgeRequest(
      "sendText",
      { text: "回复", reply_to_id: "other" },
      messages,
      "visitor-a",
    ),
  );
  assert.doesNotThrow(() =>
    validateBridgeRequest("withdrawMessage", { message_id: "own" }, messages, "visitor-a"),
  );
  for (const [method, payload] of [
    ["request", { path: "/platform/tenants" }],
    ["constructor", {}],
    ["sendText", { text: "回复", path: "/conversations/other/messages" }],
    ["sendText", { text: "回复", conversation_id: "other" }],
    ["withdrawMessage", { message_id: "own", conversation_id: "other" }],
    ["sendText", { text: "回复", reply_to_id: "cross-conversation" }],
    ["sendText", { text: "回复", reply_to_id: "internal" }],
    ["sendText", { text: "回复", reply_to_id: "withdrawn" }],
    ["withdrawMessage", { message_id: "other" }],
    ["withdrawMessage", { message_id: "internal" }],
    ["withdrawMessage", { message_id: "withdrawn" }],
    ["withdrawMessage", { message_id: "cross-conversation" }],
  ] as [string, Record<string, unknown>][]) {
    assert.throws(() => validateBridgeRequest(method, payload, messages, "visitor-a"), Error);
  }
  assert.throws(
    () => validateBridgeRequest("withdrawMessage", { message_id: "own" }, messages, "visitor-b"),
    /本人/,
  );
});
test("附件预览和下载确认只接受当前公开未撤回附件，不接受路径或会话覆盖", () => {
  const file = {
    id: "message-file",
    audience: "public",
    attachment: { id: "file-a", file_name: "附件.txt", content_type: "text/plain", size_bytes: 8 },
  } as ChatMessage;
  const messages = [
    file,
    {
      ...file,
      id: "internal",
      audience: "internal",
      attachment: { ...file.attachment!, id: "internal-file" },
    },
    {
      ...file,
      id: "withdrawn",
      withdrawn_at: "2026-10-27T08:00:00Z",
      attachment: { ...file.attachment!, id: "withdrawn-file" },
    },
  ];
  assert.equal(visibleBridgeAttachment(messages, "file-a").file_name, "附件.txt");
  for (const method of ["download", "saveAttachment"]) {
    assert.doesNotThrow(() =>
      validateBridgeRequest(method, { attachment_id: "file-a" }, messages, "visitor-a"),
    );
    for (const attachment_id of ["cross-conversation", "internal-file", "withdrawn-file", null])
      assert.throws(() => validateBridgeRequest(method, { attachment_id }, messages, "visitor-a"));
    assert.throws(() =>
      validateBridgeRequest(
        method,
        { attachment_id: "file-a", path: "/platform/tenants" },
        messages,
        "visitor-a",
      ),
    );
    assert.throws(() =>
      validateBridgeRequest(
        method,
        { attachment_id: "file-a", conversation_id: "other" },
        messages,
        "visitor-a",
      ),
    );
  }
});
test("欢迎和客服选项只透出安全文案和稳定编号，撤回或访客消息不提供选项", () => {
  const item = {
    id: "welcome",
    sender_kind: "system",
    content: "请选择",
    options: [
      {
        id: "pricing",
        label: "<script>咨询价格</script>",
        value: "实际访客内容",
        client_message_id: "11111111-2222-4333-8444-555555555555",
        internal_secret: "不得输出",
      },
    ],
  } as unknown as ChatMessage;
  const result = publicBridgeMessage(item, "visitor-a");
  assert.equal(result.options?.[0].label, "<script>咨询价格</script>");
  assert.equal(result.options?.[0].value, "实际访客内容");
  assert.equal(result.options?.[0].client_message_id, "11111111-2222-4333-8444-555555555555");
  assert.equal("internal_secret" in (result.options?.[0] || {}), false);
  assert.equal(
    publicBridgeMessage({ ...item, sender_kind: "agent" }, "visitor-a").options?.[0].label,
    "<script>咨询价格</script>",
  );
  assert.equal(
    publicBridgeMessage({ ...item, sender_kind: "visitor" }, "visitor-a").options,
    undefined,
  );
  assert.equal(
    publicBridgeMessage({ ...item, withdrawn_at: "2026-10-27T08:00:00Z" }, "visitor-a").options,
    undefined,
  );
});
test("下载确认不预取文件，确认时附件已撤回则连网络请求也不发起", async () => {
  const original = globalThis.fetch;
  let requests = 0;
  try {
    globalThis.fetch = async () => {
      requests++;
      return new Response("file");
    };
    const file = {
      id: "message-file",
      audience: "public",
      attachment: {
        id: "file-a",
        file_name: "附件.txt",
        content_type: "text/plain",
        size_bytes: 4,
      },
    } as ChatMessage;
    let messages = [file];
    validateBridgeRequest("saveAttachment", { attachment_id: "file-a" }, messages, "visitor-a");
    assert.equal(requests, 0);
    messages = [{ ...file, withdrawn_at: "2026-10-27T08:00:00Z" }];
    await assert.rejects(
      requestBridgeAttachment("file-a", () => messages, "visitor-token"),
      /撤回/,
    );
    assert.equal(requests, 0);
  } finally {
    globalThis.fetch = original;
  }
});
test("真实下载只在请求头传访客凭证，并在取到正文后再次核对最新可见性", async () => {
  const original = globalThis.fetch;
  const file = {
    id: "message-file",
    audience: "public",
    attachment: { id: "file-a", file_name: "附件.txt", content_type: "text/plain", size_bytes: 4 },
  } as ChatMessage;
  let messages = [file];
  try {
    globalThis.fetch = async (input, init) => {
      assert.equal(input, "/api/v1/attachments/file-a");
      assert.equal(new Headers(init?.headers).get("Authorization"), "Bearer visitor-token");
      assert.equal(init?.cache, "no-store");
      return new Response("file");
    };
    const result = await requestBridgeAttachment("file-a", () => messages, "visitor-token");
    assert.equal(await result.blob.text(), "file");
    assert.equal(result.name, "附件.txt");
    globalThis.fetch = async () => {
      messages = [];
      return new Response("file");
    };
    await assert.rejects(
      requestBridgeAttachment("file-a", () => messages, "visitor-token"),
      /当前可见会话/,
    );
  } finally {
    globalThis.fetch = original;
  }
});
test("附件服务错误与取消保持真实状态，不能作为下载成功", async () => {
  const original = globalThis.fetch;
  const file = {
    id: "message-file",
    audience: "public",
    attachment: { id: "file-a", file_name: "附件.txt", content_type: "text/plain", size_bytes: 4 },
  } as ChatMessage;
  try {
    globalThis.fetch = async () =>
      new Response(
        JSON.stringify({
          code: "attachment_forbidden",
          message: "附件无权读取",
          request_id: "request-a",
        }),
        { status: 403, headers: { "Content-Type": "application/json" } },
      );
    await assert.rejects(
      requestBridgeAttachment("file-a", () => [file], "visitor-token"),
      (error) =>
        error instanceof ApiError &&
        error.status === 403 &&
        error.code === "attachment_forbidden" &&
        error.requestId === "request-a",
    );
    const controller = new AbortController();
    controller.abort();
    await assert.rejects(
      requestBridgeAttachment("file-a", () => [file], "visitor-token", controller.signal),
      (error) => error instanceof DOMException && error.name === "AbortError",
    );
  } finally {
    globalThis.fetch = original;
  }
});
