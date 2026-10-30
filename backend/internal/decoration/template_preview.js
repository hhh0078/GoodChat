/** 文件作用说明：为固定访客SDK提供纯内存模拟会话，预览不握手、不请求真实聊天或业务接口。 */
(function () {
  "use strict";
  if (!window.__GoodChatBoot?.preview) return;
  const listeners = new Set(), attachments = new Map();
  let serial = 10;
  const time = Date.now();
  const avatar = new Blob([Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAIAAAAlC+aJAAAAw0lEQVR42u3YsRECMQxE0S2MKkiuUdqgCIJLqIIGOGNsHVqLP7MF/JdZ1uO5Lz0BAAAAAAAAAAAAAFALsN9vR3MHNNJPYuj39bEGpdQHGpRVH2UAMFEfYgAAAAAAAAB4SqwPWP41WuEeqHCRVbiJ+VYBMLPLdeufEeCr7nCJEtNDGEpPn2TIp37MIKv6AYOs0gcY8qzvN8i2vtPwB4DE+h6DzOs/GuRf3zbUBVjVNwxFAYb1RwYAhgDb+rcGAAAALA54AXOqJE8Q89D1AAAAAElFTkSuQmCC"), value => value.charCodeAt(0))], { type: "image/png" });
  const picture = new Blob([Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAeAAAADwCAIAAABXFyDtAAAG10lEQVR42u3dsY0UQRCG0Q6WAHDPw8QiBILBJw0wSQGkk9AKLTA7NzvzV9WTvgiqq57b6/uPb5KkwJYRSBKgJUmAliRAS5IALUmAliQBWpIEaEkCtCQJ0JIEaEkSoCVJgJYkQEuSAC1JgJYkAVqSAC1JArQkCdCSBGhJEqAlCdCSJEBLkgAtSYCWJAFakgAtSQK0JAFakgRoSRKgJQnQkiRASxKgJUmAliQBWpIALUkCtCQBWpK+fvm0PeMCtKQsl0kNaEnpLpMa0JJq6MxoQEsKpRnTgJaUrjOjAS0plGZMA1pSus6MBrSkXJ0ZDWhJuTozGtCScnVmNKAlARrQkujMaEBLaqMzowEtCdCAlkRnRgNaUhudGQ1oCdCABrQkQAMa0JJ66MxoQEuABjSgJQEa0ICW1ENnRgNaAjSgAS0J0IAGtCRAA1oSoAENaEmABrQkQAMa0JIADWhASwI0oCUBGtCAlgRoQEsCNKABLQnQgAa0BGhAA1oSoAENaEmdjB7+UoCWAA1oQEsCNKABLamB0Z4J0BKgAQ1oSYAGNKAlVTfaAwFaYjSdAS0J0IAGtKTSRnsUQEtKNNpzAFoSoAEtidF0BrSk0kZ7AkBLSjTa8AEtKdFoYwe0pDimjRrQkhKNNmRAS4pj2mABLSnRaCMFtKQsqQ0Q0JKypDYuQEsSoCVJgJYkQJuCJAFakgRoSQK0JAnQkm76+PnFEAAtKVFnQANaUqjOgAa0pESaAQ1oSbk6AxrQkkJ1fgvQ7z68f81gAS3pYJp3A/2b5tsMGdCSjtT5UaDv0oxpQEs6XuftQP+XZkwDWtJhNG8E+iGaMQ1oScfo/G+gd9OMaUBLeqvOfwP6EJoxDWhJO2m+C/ThNGMa0JL26HwL9FNpxjSgJTrvAfo0mjENaAnND3SJzpgGtETndKAxDWiJztFAYxrQEpqjgcY0oCU6RwONaUBLdI4GGtOAlkbTnA80pgEtzdW5BNCYBrQ0UedCQGMa0NIgmisCjWlAS1N0Lgo0pgEt9de5NNCYBrTUluYeQGMa0FJPndsAjWlAS910bgY0pu8AvePzdkmX09wVaEzfAZrRUjmdGwON6T+BZrRUS+f2QGN6PfSRu6QQmucAPZnptf0vd0lROo8CeibTa+N37pLSdB4I9DSm18ZP3SVF0TwZ6DlMry3/uksK1Hk40BOYXluWwHGKzoDGdCjQmBaaAY3paKAZLToDGtO5QDNadAY0pnOBxrTQDGhMRwPNaNEZ0JjOBZrRojOgMZ0LNKaFZkBjOhpoRqt0v462qM6Abs/0OmpR3Lkq0lxaZ0C3Z3oduy5uXmgGNKZDgWa0StDcQ2dAt2d6PWNpKKBkmtvoDOj2TK/nrQ4RhGZAYzoUaEYriuZ+OgO6PdPr2QvECCXQ3FJnQLdnep2zRrwQmgGN6VCgGa1LaO6tM6DbM73OXCaC6Eya2+sM6PZMr/NXiiZCM6AxHQo0o/VUmufoDOj2TK+rFosvegbNo3QGdHum17XrxRqhGdCYDgWa0TqE5pk6A7o90ythyeijt9A8VmdAt2d65awaiYRmQGM6FGhG6yGa6Qzo9kyvtIVjE5rpDGhMhwKNaTSjGdCYjgaa0WimM6AxvZKXj1xopjOgJzO98leQYmhGM6BnMr1KbCHOJtNMZ0CPZXpVWUSuzaSZzoCezPSqtY6MQ7MAPYfpVW4jYTeBZjoDGtMlgWZ0e5rpDGi9tuquJgHRLIQBmtE6g2Y6A1qtgGZ0G5rpDGg1BBrTaAa0AM1oHU8znQGtEUAzuhzNdAa0BgGNaTQDWoBmNJrpDGgBmtG9aKYzoAVoTKMZ0AI0o9FMZ0AL0IwuSjOdAS1AYxrNgBagGY1mOgNagGZ0UZrpDGgBGtNoBrQAzWg00xnQAjSji9JMZ0AL0JhGswANaEajmc6AFqAZXZRmOgNagMY0mgVoQDMazXQGtADN6KI00xnQAjSm0SxAA1pljT55XawEoAVoRsfRTGdAC9CYRrMALUAXNPqSFfH0gBagGR1HM50BLUBjGs0CtABdyugL18ITA1qAZnQczXQGtACNaTQL0AJ0EaMvXwVPCWgBmtFxNNMZ0AI0ptEsQAvQ8UaHPL8nA7QAzeg4mukMaAEa02gWoAXoYKOjntzTAFqA1ksazXQGtACtxBPyELZLgFbiFXkCqyVAK/GQDN9eCdCKOycDt1ECtBIvyqitkwCtxKMyZLskQCvutAzWFgnQSrwuI7VCArQSD8ww7Y8ArbgzM0CbI0Ar8dKMztoI0Eo8NkOzMwK04k7OoARoQCvx6oxIgAa0Eg/PcARoQCvu/AxEgAa0Ei/QKAToaf0Ew+b2o9n4b7cAAAAASUVORK5CYII="), value => value.charCodeAt(0))], { type: "image/png" });
  attachments.set("preview-image", picture);
  const state = { preview: true, conversation: { id: "preview", status: "active", agent_name: "服务顾问" }, messages: [
    { id: "preview-welcome", seq: "1", sender_kind: "agent", sender_name: "服务顾问", sender_avatar_url: "preview-agent", content: "您好，欢迎咨询。这里会展示真实聊天使用的消息样式。", created_at: new Date(time - 180000).toISOString(), options: [
      { label: "了解服务", value: "我想了解服务", client_message_id: "10000000-0000-4000-8000-000000000001" },
      { label: "联系顾问", value: "请帮我联系顾问", client_message_id: "10000000-0000-4000-8000-000000000002" }
    ] },
    { id: "preview-visitor", seq: "2", sender_kind: "visitor", sender_name: "你", is_own: true, sender_avatar_url: "preview-visitor", content: "你好，我想先了解一下。", created_at: new Date(time - 120000).toISOString() },
    { id: "preview-picture", seq: "3", sender_kind: "agent", sender_name: "服务顾问", sender_avatar_url: "preview-agent", content: "可以发送文字、图片和文件，也可以点击上面的快捷选项。", created_at: new Date(time - 60000).toISOString(), message_type: "image", attachment: { id: "preview-image", file_name: "服务介绍.png", content_type: "image/png" } }
  ], peer_read_seq: "2", is_typing: false, has_older: true, loading_older: false };

  const profiles = {
    finance: ["专属顾问", "您好，我是您的专属顾问。想先了解哪方面？", "想先了解办理流程。", "好的，我来为您介绍办理流程的相关信息。您可以告诉我更具体的需求，我会为您详细解答。", ["产品咨询", "办理流程", "联系顾问"]],
    commerce: ["服务顾问", "您好，订单、物流或退换货问题，都可以在这里聊。", "我想了解退换货流程。", "可以的，请先告诉我您的具体情况。", ["物流咨询", "退换货咨询"]],
    education: ["知序学习顾问", "你好呀，可以先聊聊你的学习目标。", "我想了解适合自己的学习方向。", "当然，告诉我你目前的阶段和希望提升的方面吧。", ["了解课程", "学习规划"]],
    medical: ["就诊服务顾问", "您好，想了解预约准备，还是就诊流程？", "第一次就诊，需要准备什么？", "可先准备身份证件和既往就诊资料，具体要求可留言确认。", ["了解就诊流程", "预约前需要准备什么"]],
    property: ["置业服务顾问", "您好，想先了解看房流程，还是聊聊居住需求？", "我想先了解看房流程。", "可以告诉我您的时间安排，顾问会与您沟通具体事宜。", ["了解看房流程", "与顾问沟通需求"]],
    business: ["服务顾问", "您好，请说说您的业务场景。", "想咨询企业服务方案。", "好的，我们从需求开始聊。", ["服务咨询", "项目沟通"]],
    gaming: ["黑曜服务顾问", "欢迎来到黑曜竞技，有什么可以帮您？", "想了解场馆和设备。", "可以，您更关注哪一方面？", ["场馆咨询", "设备沟通"]]
  };
  const profileKey = typeof document === "undefined" ? "" : document.body?.dataset?.gcPreviewIndustry;
  const profile = profiles[profileKey];
  if (profile) {
    const options = profile[4].map((label, index) => ({ label, value: label, client_message_id: "10000000-0000-4000-8000-00000000000" + (index + 1) }));
    state.conversation.agent_name = profile[0];
    state.has_older = false;
    state.messages = [
      { id: "preview-welcome", seq: "1", sender_kind: "agent", sender_name: profile[0], sender_avatar_url: "preview-agent", content: profile[1], created_at: new Date(time - 180000).toISOString(), options: ["medical", "business", "gaming"].includes(profileKey) ? undefined : options },
      { id: "preview-visitor", seq: "2", sender_kind: "visitor", sender_name: "你", is_own: true, sender_avatar_url: "preview-visitor", content: profile[2], created_at: new Date(time - 120000).toISOString() },
      { id: "preview-reply", seq: "3", sender_kind: "agent", sender_name: profile[0], sender_avatar_url: "preview-agent", content: profile[3], created_at: new Date(time - 60000).toISOString(), options: ["medical", "business", "gaming"].includes(profileKey) ? options : undefined }
    ];
  }
  /** 从平台内置照片生成纯内存头像，不访问任何网络或租户数据。 */
  function portrait(own) { const raw = own ? "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" : "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"; return new Blob([Uint8Array.from(atob(raw), value => value.charCodeAt(0))], { type: "image/jpeg" }); }
  if (profile && typeof document !== "undefined") {
    const panel = document.querySelector(".gc-conversation");
    if (panel) { const footer = document.createElement("div"); footer.className = "gc-preview-support"; footer.textContent = "由 GoodChat 提供在线客服服务"; panel.append(footer); }
  }

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
  /** 展示纯本地留言或评价说明；不会收集、上传或提交真实业务资料。 */
  function previewVisitorAction(kind) {
    if (typeof document === "undefined") return { opened: true, preview: true };
    const evaluation = kind === "evaluation", id = evaluation ? "gc-preview-evaluation" : "gc-preview-leave-message";
    let dialog = document.getElementById(id);
    if (!dialog) {
      dialog = document.createElement("dialog");
      dialog.id = id;
      dialog.className = "gc-dialog";
      const title = document.createElement("h2"), text = document.createElement("p"), close = document.createElement("button");
      title.textContent = evaluation ? "会话评价预览" : "留言功能预览";
      text.textContent = evaluation ? "正式咨询结束后会打开官方会话评价；此处仅演示按钮，不会提交真实评价。" : "正式咨询页面会打开官方留言表单；此处仅演示按钮，不会提交真实留言。";
      close.type = "button";
      close.textContent = "关闭预览";
      close.addEventListener("click", () => { if (typeof dialog.close === "function") dialog.close(); dialog.remove(); });
      dialog.addEventListener("close", () => dialog.remove(), { once: true });
      dialog.append(title, text, close);
      document.body.append(dialog);
    }
    if (!dialog.open) {
      if (typeof dialog.showModal === "function") dialog.showModal();
      else dialog.setAttribute("open", "");
    }
    return { opened: true, preview: true };
  }
  /** 仅执行固定模拟动作；未声明的方法始终拒绝。 */
  async function request(method, payload = {}) {
    if (method === "getState") return snapshot();
    if (method === "openLeaveMessage") return previewVisitorAction("leave-message");
    if (method === "openEvaluation") return previewVisitorAction("evaluation");
    if (method === "sendText") {
      if (typeof payload.text !== "string" || !payload.text.trim() || payload.text.length > 10000) throw new TypeError("消息不能为空且不能超过10000个字符");
      return append(payload.text.trim(), payload.client_message_id || crypto.randomUUID(), payload.reply_to_id);
    }
    if (method === "typing" || method === "read") return { accepted: true, preview: true };
    if (method === "loadOlder") { if (state.has_older) state.messages.unshift({ id: "preview-older", seq: "0", sender_kind: "system", content: "这是更早的模拟消息，预览操作不会产生真实咨询。", created_at: new Date(time - 3600000).toISOString() }); state.has_older = false; publish(); return snapshot(); }
    if (method === "getAvatar") { if (!state.messages.some(item => item.id === payload.message_id)) throw new Error("头像必须属于当前预览消息"); return { file: profile ? portrait(state.messages.find(item => item.id === payload.message_id)?.is_own === true) : avatar }; }
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
  const api = { preview: true, ready: () => Promise.resolve(), request, getState: () => request("getState"), openLeaveMessage: () => request("openLeaveMessage"), openEvaluation: () => request("openEvaluation"), loadOlder: () => request("loadOlder"), getAvatar: message_id => request("getAvatar", { message_id }), sendText: (text, client_message_id, reply_to_id) => request("sendText", { text, client_message_id, reply_to_id }), withdrawMessage: message_id => request("withdrawMessage", { message_id }), typing: active => request("typing", { active }), upload, download: attachment_id => request("download", { attachment_id }), saveAttachment: attachment_id => request("saveAttachment", { attachment_id }), read: last_read_seq => request("read", { last_read_seq }), subscribe: handler => { if (typeof handler !== "function") throw new TypeError("订阅函数不正确"); listeners.add(handler); handler(snapshot()); return () => listeners.delete(handler); } };
  Object.defineProperty(window, "GoodChat", { value: Object.freeze(api), writable: false, configurable: false });
})();
