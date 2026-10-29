/** 文件作用说明：为固定访客SDK提供纯内存模拟会话，预览不握手、不请求真实聊天或业务接口。 */
(function () {
  "use strict";
  if (!window.__GoodChatBoot?.preview) return;
  const listeners = new Set(), attachments = new Map();
  let serial = 10;
  const time = Date.now();
  const avatar = new Blob([Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAIAAAAlC+aJAAAAw0lEQVR42u3YsRECMQxE0S2MKkiuUdqgCIJLqIIGOGNsHVqLP7MF/JdZ1uO5Lz0BAAAAAAAAAAAAAFALsN9vR3MHNNJPYuj39bEGpdQHGpRVH2UAMFEfYgAAAAAAAAB4SqwPWP41WuEeqHCRVbiJ+VYBMLPLdeufEeCr7nCJEtNDGEpPn2TIp37MIKv6AYOs0gcY8qzvN8i2vtPwB4DE+h6DzOs/GuRf3zbUBVjVNwxFAYb1RwYAhgDb+rcGAAAALA54AXOqJE8Q89D1AAAAAElFTkSuQmCC"), value => value.charCodeAt(0))], { type: "image/png" });
  const picture = new Blob([Uint8Array.from(atob("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"), value => value.charCodeAt(0))], { type: "image/png" });
  attachments.set("preview-image", picture);
  const state = { preview: true, conversation: { id: "preview", status: "active", agent_name: "服务顾问" }, messages: [
    { id: "preview-welcome", seq: "1", sender_kind: "agent", sender_name: "服务顾问", sender_avatar_url: "preview-agent", content: "您好，欢迎咨询。这里会展示真实聊天使用的消息样式。", created_at: new Date(time - 180000).toISOString(), options: [
      { label: "了解服务", value: "我想了解服务", client_message_id: "10000000-0000-4000-8000-000000000001" },
      { label: "联系顾问", value: "请帮我联系顾问", client_message_id: "10000000-0000-4000-8000-000000000002" }
    ] },
    { id: "preview-visitor", seq: "2", sender_kind: "visitor", sender_name: "你", is_own: true, sender_avatar_url: "preview-visitor", content: "你好，我想先了解一下。", created_at: new Date(time - 120000).toISOString() },
    { id: "preview-picture", seq: "3", sender_kind: "agent", sender_name: "服务顾问", sender_avatar_url: "preview-agent", content: "可以发送文字、图片和文件，也可以点击上面的快捷选项。", created_at: new Date(time - 60000).toISOString(), message_type: "image", attachment: { id: "preview-image", file_name: "服务介绍.png", content_type: "image/png" } }
  ], peer_read_seq: "2", is_typing: false, has_older: true, loading_older: false };

  /** 复制预览状态，调用方不能修改模拟会话的内部数据。 */
  function snapshot() { return JSON.parse(JSON.stringify(state)); }
  /** 通知同一个订阅接口，保持预览与真实聊天渲染一致。 */
  function publish() { const value = snapshot(); for (const handler of listeners) { try { handler(value); } catch { /* 单个UI订阅失败不影响其他订阅。 */ } } }
  /** 在内存里去重并追加一条访客消息，不发送任何网络请求。 */
  function append(text, id, reply, attachment) {
    const existing = state.messages.find(item => item.client_message_id === id);
    if (existing) return existing;
    const item = { id: "preview-" + (++serial), seq: String(serial), client_message_id: id, sender_kind: "visitor", sender_name: "你", is_own: true, sender_avatar_url: "preview-visitor", content: text, created_at: new Date().toISOString() };
    if (attachment) item.attachment = attachment;
    if (reply) { const quoted = state.messages.find(value => value.id === reply); if (quoted) item.reply = { content: quoted.content || "附件消息" }; }
    state.messages.push(item); state.peer_read_seq = item.seq; publish(); return item;
  }
  /** 仅执行固定模拟动作；未声明的方法始终拒绝。 */
  async function request(method, payload = {}) {
    if (method === "getState") return snapshot();
    if (method === "sendText") {
      if (typeof payload.text !== "string" || !payload.text.trim() || payload.text.length > 10000) throw new TypeError("消息不能为空且不能超过10000个字符");
      return append(payload.text.trim(), payload.client_message_id || crypto.randomUUID(), payload.reply_to_id);
    }
    if (method === "typing" || method === "read") return { accepted: true, preview: true };
    if (method === "loadOlder") { if (state.has_older) state.messages.unshift({ id: "preview-older", seq: "0", sender_kind: "system", content: "这是更早的模拟消息，预览操作不会产生真实咨询。", created_at: new Date(time - 3600000).toISOString() }); state.has_older = false; publish(); return snapshot(); }
    if (method === "getAvatar") { if (!state.messages.some(item => item.id === payload.message_id)) throw new Error("头像必须属于当前预览消息"); return { file: avatar }; }
    if (method === "download" || method === "saveAttachment") { const file = attachments.get(payload.attachment_id); if (!file) throw new Error("预览附件不存在"); return method === "download" ? { file } : { preview: true, confirmation_required: true }; }
    if (method === "withdrawMessage") { const item = state.messages.find(value => value.id === payload.message_id && value.is_own); if (!item) throw new Error("只能撤回本人预览消息"); item.withdrawn_at = new Date().toISOString(); publish(); return { accepted: true }; }
    throw new Error("预览不支持此操作");
  }
  /** 验证附件并将文件仅保存在当前预览内存中。 */
  async function upload(file, name) {
    if (!(file instanceof Blob) || file.size === 0 || file.size > 20 * 1024 * 1024) throw new TypeError("请选择不超过20MB的有效文件");
    const filename = name || file.name || "附件";
    if (typeof filename !== "string" || !filename.trim() || filename.length > 240) throw new TypeError("附件名称不正确");
    const id = "preview-upload-" + crypto.randomUUID(); attachments.set(id, new Blob([await file.arrayBuffer()], { type: file.type }));
    return append("", crypto.randomUUID(), null, { id, file_name: filename, content_type: file.type || "application/octet-stream" });
  }
  const api = { preview: true, ready: () => Promise.resolve(), request, getState: () => request("getState"), loadOlder: () => request("loadOlder"), getAvatar: message_id => request("getAvatar", { message_id }), sendText: (text, client_message_id, reply_to_id) => request("sendText", { text, client_message_id, reply_to_id }), withdrawMessage: message_id => request("withdrawMessage", { message_id }), typing: active => request("typing", { active }), upload, download: attachment_id => request("download", { attachment_id }), saveAttachment: attachment_id => request("saveAttachment", { attachment_id }), read: last_read_seq => request("read", { last_read_seq }), subscribe: handler => { if (typeof handler !== "function") throw new TypeError("订阅函数不正确"); listeners.add(handler); handler(snapshot()); return () => listeners.delete(handler); } };
  Object.defineProperty(window, "GoodChat", { value: Object.freeze(api), writable: false, configurable: false });
})();
