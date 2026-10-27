/** 文件作用说明：在纯Node VM的最小DOM替身中验证装修聊天展示与SDK参数，不连接浏览器或真实会话。 */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { runInNewContext } from "node:vm";

/** Element 为纯渲染测试提供必要DOM行为，不模拟浏览器权限或文件选择器。 */
class Element {
  /** constructor 初始化独立测试节点及常用表单状态。 */
  constructor(tag = "div", connected = false) {
    this.tagName = tag.toUpperCase(); this.children = []; this.parent = null; this.connected = connected;
    this.className = ""; this.dataset = {}; this.value = ""; this.hidden = false; this.disabled = false;
    this.scrollHeight = 200; this.scrollTop = 0; this.clientHeight = 400; this.listeners = {}; this.clicks = 0;
    this.ownText = "";
  }
  /** textContent 读取当前节点及子节点的可见文本。 */
  get textContent() { return this.ownText + this.children.map(child => child.textContent).join(""); }
  /** textContent 写入文本并清空子节点，避免测试替身把文本解析为HTML。 */
  set textContent(value) { this.ownText = String(value); this.replaceChildren(); }
  /** isConnected 判断节点是否仍属于测试文档树。 */
  get isConnected() { return this.connected || Boolean(this.parent?.isConnected); }
  /** append 按浏览器DOM规则关联子节点。 */
  append(...children) { for (const child of children) { child.parent = this; this.children.push(child); } }
  /** replaceChildren 替换节点并断开旧节点连接。 */
  replaceChildren(...children) { for (const child of this.children) child.parent = null; this.children = []; this.append(...children); }
  /** querySelectorAll 仅支持本测试所需的类名选择器。 */
  querySelectorAll(selector) { return flatten(this).filter(node => node !== this && node.className.split(" ").includes(selector.slice(1))); }
  /** addEventListener 保存测试中需要触发的事件处理器。 */
  addEventListener(type, handler) { this.listeners[type] = handler; }
  /** setAttribute 保存辅助阅读属性，不执行任何浏览器行为。 */
  setAttribute(name, value) { this[name] = String(value); }
  /** click 记录用户动作转发并执行实际处理器，不打开系统文件窗口。 */
  click() { this.clicks++; return this.onclick?.({}); }
  /** focus 提供引用选择后的输入焦点调用。 */
  focus() { this.focused = true; }
  /** remove 从父节点中移除当前节点。 */
  remove() { if (this.parent) this.parent.children = this.parent.children.filter(child => child !== this); this.parent = null; }
}

/** flatten 读取所有后代节点用于断言展示内容和操作按钮。 */
function flatten(node) { return [node, ...node.children.flatMap(flatten)]; }

/** button 按真实按钮文案选择并验证测试中的操作入口。 */
function button(node, text) { const found = flatten(node).find(item => item.tagName === "BUTTON" && item.textContent === text); assert.ok(found, `未找到按钮 ${text}`); return found; }

/** settle 完成纯Promise调度，不启动网络或浏览器。 */
async function settle() { for (let i = 0; i < 8; i++) await Promise.resolve(); }

/** fixture 将真实装修脚本放入纯VM，SDK仅记录参数并返回明确标记的测试数据。 */
function fixture() {
  const elements = new Map(["gc-status", "gc-messages", "gc-text", "gc-send", "gc-file", "gc-typing", "gc-error", "gc-reply-bar", "gc-reply-text", "gc-reply-cancel"].map(id => [id, new Element("div", true)]));
  const calls = [], timers = new Map(); let counter = 0, subscriber;
  const state = {
    conversation: { id: "test-conversation", status: "active", agent_name: "测试客服" },
    messages: [
      { id: "system", seq: "9007199254740991", sender_kind: "system", text: "测试系统通知", created_at: "2026-10-27T01:00:00Z" },
      { id: "agent", seq: "9007199254740992", sender_kind: "agent", sender_name: "测试客服", text: "测试客服消息", created_at: "2026-10-27T01:01:00Z" },
      { id: "own", seq: "9007199254740993", sender_kind: "visitor", is_own: true, text: "测试本人消息", created_at: "2026-10-27T01:02:00Z" },
    ],
    is_typing: true, peer_read_seq: "9007199254740993",
  };
  const chat = {
    /** ready 返回仅用于测试的桥已准备状态。 */
    ready: async () => {},
    /** getState 返回当前测试态副本，避免脚本修改替身事实。 */
    getState: async () => structuredClone(state),
    /** subscribe 保存真实脚本的状态订阅。 */
    subscribe: handler => { subscriber = handler; queueMicrotask(() => handler(structuredClone(state))); return () => {}; },
    /** read 记录十进制字符串游标。 */
    read: async seq => { calls.push(["read", seq]); return { accepted: true }; },
    /** typing 记录输入标志，不接收草稿正文。 */
    typing: async active => { calls.push(["typing", active]); return { accepted: true }; },
    /** sendText 记录引用及幂等参数，模拟服务端保存后才产生的消息。 */
    sendText: async (text, id, replyID) => { calls.push(["sendText", text, id, replyID]); state.messages.push({ id: "sent", client_message_id: id, seq: "9007199254740994", sender_kind: "visitor", is_own: true, text, created_at: "2026-10-27T01:03:00Z", reply: replyID ? { id: replyID, content: "测试客服消息", withdrawn: false } : null }); },
    /** withdrawMessage 只记录消息编号并更新测试态中的撤回事实。 */
    withdrawMessage: async id => { calls.push(["withdrawMessage", id]); const item = state.messages.find(item => item.id === id); item.withdrawn_at = "2026-10-27T01:04:00Z"; item.text = ""; },
    /** upload 记录Blob与文件名，本测试不执行真实上传。 */
    upload: async (blob, name) => { calls.push(["upload", blob, name]); },
    /** download 只为渲染测试提供固定Blob，不代表实际附件鉴权。 */
    download: async id => { calls.push(["download", id]); return { file: new Blob(["测试附件"]), name: "test.txt" }; },
    /** saveAttachment 记录可信父层确认请求，不把本地替身当作实际文件保存。 */
    saveAttachment: async id => { calls.push(["saveAttachment", id]); return { confirmation_required: true }; },
  };
  const document = { visibilityState: "visible", getElementById: id => elements.get(id), createElement: tag => new Element(tag), addEventListener() {} };
  const window = { GoodChat: chat, addEventListener() {} };
  runInNewContext(readFileSync(new URL("./schema_chat.js", import.meta.url), "utf8"), {
    window, document, Date, Blob, BigInt, console, URL: { createObjectURL: () => "blob:test-only", revokeObjectURL() {} }, crypto: { randomUUID: () => "test-message-id" },
    setTimeout: handler => { timers.set(++counter, handler); return counter; }, clearTimeout: id => timers.delete(id),
  });
  return { elements, calls, state, chat, timers, publish: () => subscriber(structuredClone(state)) };
}

test("默认聊天区分本人客服系统并保留64位已读游标", async () => {
  const f = fixture(); await settle();
  const rows = f.elements.get("gc-messages").querySelectorAll(".gc-message-row");
  assert.equal(rows.length, 3);
  assert.ok(rows[0].className.includes("gc-system"));
  assert.match(rows[1].textContent, /客服 测试客服/);
  assert.ok(rows[2].className.includes("gc-own"));
  assert.match(rows[2].textContent, /你.*测试本人消息.*已读/);
  assert.ok(flatten(rows[2]).some(node => node.tagName === "TIME" && node.dateTime === "2026-10-27T01:02:00.000Z"));
  assert.equal(f.elements.get("gc-typing").textContent, "客服正在输入…");
  assert.ok(f.calls.some(call => call[0] === "read" && call[1] === "9007199254740993"));
  f.state.is_typing = false; f.publish(); assert.equal(f.elements.get("gc-typing").textContent, "");
});

test("引用发送与本人内联撤回传递受限SDK参数", async () => {
  const f = fixture(); await settle();
  const rows = f.elements.get("gc-messages").querySelectorAll(".gc-message-row");
  button(rows[1], "引用").click();
  assert.equal(f.elements.get("gc-reply-bar").hidden, false);
  f.elements.get("gc-text").value = "测试引用发送";
  await f.elements.get("gc-send").click(); await settle();
  assert.deepEqual(f.calls.find(call => call[0] === "sendText"), ["sendText", "测试引用发送", "test-message-id", "agent"]);
  assert.equal(f.elements.get("gc-reply-bar").hidden, true);
  const sent = f.elements.get("gc-messages").querySelectorAll(".gc-message-row").at(-1);
  assert.match(sent.textContent, /引用：测试客服消息/);
  button(sent, "撤回").click(); assert.equal(f.calls.some(call => call[0] === "withdrawMessage"), false);
  await button(sent, "确认撤回").click(); await settle();
  assert.deepEqual(f.calls.find(call => call[0] === "withdrawMessage"), ["withdrawMessage", "sent"]);
  assert.match(f.elements.get("gc-messages").textContent, /消息已撤回/);
});

test("原生文件输入处理附件且输入状态不传正文", async () => {
  const f = fixture(); await settle();
  assert.equal(f.elements.has("gc-upload"), false);
  assert.equal(f.elements.get("gc-file").hidden, false);
  assert.equal(f.elements.get("gc-file").clicks, 0);
  const blob = new Blob(["纯VM测试附件"], { type: "text/plain" });
  blob.name = "fixture.txt"; f.elements.get("gc-file").files = [blob];
  await f.elements.get("gc-file").onchange(); await settle();
  assert.equal(f.calls.find(call => call[0] === "upload")[2], "fixture.txt");
  f.elements.get("gc-text").value = "不会传给对方的草稿";
  f.elements.get("gc-text").listeners.input(); await settle();
  const typingCall = f.calls.find(call => call[0] === "typing" && call[1] === true);
  assert.deepEqual(typingCall, ["typing", true]);
  f.state.conversation.status = "closed"; f.publish();
  assert.equal(f.elements.get("gc-send").disabled, true);
  assert.equal(f.elements.get("gc-file").disabled, true);
});

test("文件请求可信父层下载而不在沙箱创建下载链接", async () => {
  const f = fixture(); await settle();
  f.state.messages.push({ id: "file-message", seq: "9007199254740994", sender_kind: "agent", text: "", created_at: "2026-10-27T01:04:00Z", attachment: { id: "authorized-file", file_name: "测试文件.txt", content_type: "text/plain" } });
  f.publish(); await settle();
  assert.equal(f.calls.some(call => call[0] === "download"), false);
  await button(f.elements.get("gc-messages"), "下载 测试文件.txt").click();
  assert.deepEqual(f.calls.find(call => call[0] === "saveAttachment"), ["saveAttachment", "authorized-file"]);
  assert.match(f.elements.get("gc-messages").textContent, /请在页面的下载确认中继续/);
  assert.equal(flatten(f.elements.get("gc-messages")).some(node => node.tagName === "A" && node.download), false);
});

test("图片预览失败显示安全纯文本原因并保留重试和可信下载", async () => {
  const f = fixture(); await settle();
  f.chat.download = async () => { throw new Error("附件已撤回或无权读取 <script>not-html</script>"); };
  f.state.messages.push({ id: "image-message", seq: "9007199254740994", sender_kind: "agent", text: "", created_at: "2026-10-27T01:04:00Z", attachment: { id: "image", file_name: "图片.png", content_type: "image/png" } });
  f.publish(); await settle();
  const reason = f.elements.get("gc-messages").querySelectorAll(".gc-attachment-error")[0];
  assert.match(reason.textContent, /附件加载失败：附件已撤回或无权读取/);
  assert.ok(reason.textContent.includes("<script>not-html</script>"));
  assert.ok(button(f.elements.get("gc-messages"), "重新加载附件"));
  assert.ok(button(f.elements.get("gc-messages"), "下载 图片.png"));
  assert.equal(flatten(f.elements.get("gc-messages")).some(node => node.tagName === "SCRIPT"), false);
});

test("欢迎选项安全展示并沿用持久UUID，连点不会发送第二条文字", async () => {
  const f = fixture(); await settle();
  const id = "11111111-2222-4333-8444-555555555555";
  f.state.messages.push({ id: "welcome", seq: "9007199254740994", sender_kind: "system", text: "请选择", created_at: "2026-10-27T01:04:00Z", options: [{ id: "pricing", label: "<script>咨询价格</script>", value: "我想了解价格", client_message_id: id }] });
  f.publish(); await settle();
  const option = button(f.elements.get("gc-messages"), "<script>咨询价格</script>");
  assert.equal(flatten(option).some(item => item.tagName === "SCRIPT"), false);
  const first = option.click(); option.click(); await first; await settle();
  const calls = f.calls.filter(call => call[0] === "sendText");
  assert.equal(calls.length, 1);
  assert.deepEqual(calls[0], ["sendText", "我想了解价格", id, undefined]);
  assert.equal(button(f.elements.get("gc-messages"), "<script>咨询价格</script>").disabled, true);
  f.state.conversation.status = "closed"; f.publish();
  assert.equal(button(f.elements.get("gc-messages"), "<script>咨询价格</script>").disabled, true);
});
