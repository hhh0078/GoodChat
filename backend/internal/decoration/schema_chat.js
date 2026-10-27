/** 文件作用说明：在隔离装修页呈现真实SDK聊天状态，支持本人气泡、已读、输入状态、引用、撤回与授权附件。 */
(function () {
  "use strict";
  const chat = window.GoodChat;
  const status = document.getElementById("gc-status");
  const messages = document.getElementById("gc-messages");
  const input = document.getElementById("gc-text");
  const send = document.getElementById("gc-send");
  const file = document.getElementById("gc-file");
  const typing = document.getElementById("gc-typing");
  const error = document.getElementById("gc-error");
  const replyBar = document.getElementById("gc-reply-bar");
  const replyText = document.getElementById("gc-reply-text");
  if (!messages) return;
  let state = null, composing = false, ready = false, busy = false, readSeq = 0n;
  let urls = [], retry = null, reply = null, lastTyping = 0, typingTimer = null, messageSignature = "";

  /** 展示安全的业务错误文案。 */
  function showError(problem) { error.textContent = problem?.message || "操作未完成，请稍后重试"; }

  /** 把服务端十进制字符串转为精确序号，非法值不参与已读比较。 */
  function sequence(value) { const text = String(value ?? "0"); return /^\d+$/.test(text) ? BigInt(text) : 0n; }

  /** 依据可信父层提供的本人标记及角色显示作者，不使用任何身份令牌。 */
  function author(item) {
    if (item.is_own === true) return "你";
    if (item.sender_kind === "system") return "系统通知";
    if (item.sender_kind === "agent") return `客服 ${item.sender_name || state?.conversation?.agent_name || ""}`.trim();
    return item.sender_name || "访客";
  }

  /** 按当前设备时区展示真实消息时间，保留完整时间用于辅助阅读。 */
  function timeElement(value) {
    const element = document.createElement("time");
    const date = new Date(value);
    if (!Number.isNaN(date.getTime())) {
      element.dateTime = date.toISOString();
      element.textContent = date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
      element.title = date.toLocaleString("zh-CN", { hour12: false });
    }
    return element;
  }

  /** 根据当前会话及提交状态控制实际可执行的输入和附件按钮。 */
  function updateComposer() {
    const disabled = !ready || busy || state?.conversation?.status === "closed";
    input.disabled = disabled; send.disabled = disabled; file.disabled = disabled;
    for (const button of messages.querySelectorAll(".gc-welcome-option")) button.disabled = disabled || Boolean(state?.messages?.some(item => item.client_message_id === button.dataset.clientMessageId));
  }

  /** 清除待引用消息，不修改已经保存的任何消息。 */
  function cancelReply() { reply = null; replyBar.hidden = true; replyText.textContent = ""; }

  /** 选择当前可见消息作为下一条文字的引用。 */
  function chooseReply(item) {
    reply = { id: item.id, text: item.text || item.content || "附件消息" };
    replyText.textContent = `引用 ${author(item)}：${String(reply.text).slice(0, 100)}`;
    replyBar.hidden = false;
    input.focus();
  }

  /** 只在消息真实可见且滚动到底部时上报精确已读位置。 */
  async function read() {
    if (!state || document.visibilityState !== "visible" || messages.scrollHeight - messages.scrollTop - messages.clientHeight > 60) return;
    let last = 0n;
    for (const item of state.messages || []) { const value = sequence(item.seq || item.event_seq || item.sequence); if (value > last) last = value; }
    if (last <= readSeq) return;
    try { const result = await chat.read(last.toString()); if (result?.accepted !== false) readSeq = last; } catch { /* 网络恢复后再次读取，不把失败报告成已读。 */ }
  }

  /** 通过可信父层发起下载确认，不在禁止下载的子沙箱内创建下载链接。 */
  function attachmentSave(node, item) {
    const button = document.createElement("button"); button.type = "button"; button.className = "gc-file-save"; button.textContent = `下载 ${item.file_name || "附件"}`;
    const notice = document.createElement("span"); notice.className = "gc-file-notice"; notice.setAttribute("aria-live", "polite");
    button.onclick = async () => {
      button.disabled = true; notice.textContent = "";
      try { const result = await chat.saveAttachment(item.id); notice.textContent = result?.confirmation_required ? "请在页面的下载确认中继续" : result?.download_started ? "下载已发起" : "已请求下载，请按页面提示继续"; }
      catch (problem) { notice.textContent = String(problem?.message || "附件暂时无法下载，请稍后重试").slice(0, 500); }
      finally { button.disabled = false; }
    };
    node.append(button, notice);
  }

  /** 仅通过父层授权Blob加载图片和音频，失败原因以纯文本显示供重试定位。 */
  async function attachmentPreview(node, item) {
    node.textContent = "正在加载附件…";
    try {
      const result = await chat.download(item.id);
      if (!node.isConnected) return;
      const objectURL = URL.createObjectURL(result.file);
      urls.push(objectURL);
      let element;
      if (String(item.content_type || "").startsWith("image/")) { element = document.createElement("img"); element.alt = item.file_name || "图片消息"; element.src = objectURL; }
      else { element = document.createElement("audio"); element.controls = true; element.src = objectURL; }
      node.textContent = ""; node.append(element);
    } catch (problem) {
      if (!node.isConnected) return;
      node.textContent = "";
      const reason = document.createElement("span"); reason.className = "gc-attachment-error"; reason.textContent = `附件加载失败：${String(problem?.message || "暂时无法读取，请稍后重试").slice(0, 500)}`;
      const button = document.createElement("button"); button.type = "button"; button.textContent = "重新加载附件";
      button.onclick = () => { void attachmentPreview(node, item); };
      node.append(reason, button);
    }
  }

  /** 文件只请求父层下载，图片和音频另提供受控内联预览，均不暴露凭证。 */
  function attachment(node, item) {
    const container = document.createElement("div"); container.className = "gc-attachment"; node.append(container);
    attachmentSave(container, item);
    if (String(item.content_type || "").startsWith("image/") || String(item.content_type || "").startsWith("audio/") || item.content_type === "video/webm") {
      const preview = document.createElement("div"); preview.className = "gc-attachment-preview"; container.append(preview); void attachmentPreview(preview, item);
    }
  }

  /** 在气泡内二次确认后撤回本人消息，不开启沙箱弹窗或任意接口能力。 */
  function confirmWithdraw(actions, item) {
    actions.replaceChildren();
    const label = document.createElement("span"); label.textContent = "撤回这条消息？";
    const confirm = document.createElement("button"); confirm.type = "button"; confirm.textContent = "确认撤回";
    const cancel = document.createElement("button"); cancel.type = "button"; cancel.textContent = "取消";
    cancel.onclick = () => renderActions(actions, item);
    confirm.onclick = async () => {
      confirm.disabled = true; cancel.disabled = true; error.textContent = "";
      try { await chat.withdrawMessage(item.id); render(await chat.getState()); }
      catch (problem) { showError(problem); renderActions(actions, item); }
    };
    actions.append(label, confirm, cancel);
  }

  /** 仅为可见未撤回消息提供引用，为本人消息提供受服务端时间窗约束的撤回。 */
  function renderActions(actions, item) {
    actions.replaceChildren();
    if (item.withdrawn_at || item.recalled_at || item.is_recalled || item.sender_kind === "system") return;
    const quote = document.createElement("button"); quote.type = "button"; quote.textContent = "引用"; quote.onclick = () => chooseReply(item); actions.append(quote);
    if (item.is_own === true) { const withdraw = document.createElement("button"); withdraw.type = "button"; withdraw.textContent = "撤回"; withdraw.onclick = () => confirmWithdraw(actions, item); actions.append(withdraw); }
  }

  /** 欢迎选项按普通访客文字发送，沿用服务端持久UUID使连点和刷新保持幂等。 */
  function welcomeOptions(node, item) {
    if (item.sender_kind !== "system" || !Array.isArray(item.options)) return;
    const options = document.createElement("div");
    for (const option of item.options.slice(0, 8)) {
      if (typeof option.label !== "string" || typeof option.value !== "string" || !/^[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}$/i.test(option.client_message_id || "")) continue;
      const button = document.createElement("button"); button.type = "button"; button.className = "gc-file-save gc-welcome-option"; button.textContent = option.label; button.dataset.clientMessageId = option.client_message_id;
      button.onclick = async () => {
        if (button.disabled || busy || state?.conversation?.status === "closed") return;
        busy = true; updateComposer(); error.textContent = "";
        try { await chat.sendText(option.value, option.client_message_id); render(await chat.getState()); }
        catch (problem) { showError(problem); }
        finally { busy = false; updateComposer(); }
      };
      options.append(button);
    }
    node.append(options);
  }

  /** 对本人已保存消息展示真实对方已读进度，不把保存成功当作送达。 */
  function updateReceipts() {
    const peerRead = sequence(state?.peer_read_seq);
    for (const receipt of messages.querySelectorAll(".gc-read-status")) {
      const seq = sequence(receipt.dataset.seq);
      receipt.textContent = seq > 0n && seq <= peerRead ? "已读" : "已发送";
    }
  }

  /** 用文本节点绘制消息作者、时间、引用和附件，消息内容不作为HTML执行。 */
  function messageElement(item) {
    const own = item.is_own === true, system = item.sender_kind === "system";
    const row = document.createElement("div"); row.className = `gc-message-row${own ? " gc-own" : ""}${system ? " gc-system" : ""}`;
    const meta = document.createElement("div"); meta.className = "gc-message-meta";
    const name = document.createElement("span"); name.textContent = author(item); meta.append(name, timeElement(item.created_at)); row.append(meta);
    const bubble = document.createElement("div"); bubble.className = "gc-message";
    const withdrawn = item.withdrawn_at || item.recalled_at || item.is_recalled;
    if (withdrawn) bubble.textContent = "消息已撤回";
    else {
      if (item.reply) { const quote = document.createElement("blockquote"); quote.className = "gc-reply"; quote.textContent = `引用：${item.reply.withdrawn ? "消息已撤回" : item.reply.content || "附件消息"}`; bubble.append(quote); }
      const body = document.createElement("div"); body.className = "gc-message-body"; const text = item.text ?? item.content ?? ""; body.textContent = typeof text === "string" ? text : ""; bubble.append(body);
      if (item.attachment) attachment(bubble, item.attachment);
      welcomeOptions(bubble, item);
    }
    row.append(bubble);
    if (own && !withdrawn) { const receipt = document.createElement("div"); receipt.className = "gc-read-status"; receipt.dataset.seq = String(item.seq || item.event_seq || "0"); row.append(receipt); }
    const actions = document.createElement("div"); actions.className = "gc-message-actions"; renderActions(actions, item); row.append(actions);
    return row;
  }

  /** 合并父层实时状态；单纯输入状态或已读变化不重建附件和消息DOM。 */
  function render(next) {
    if (!next || !Array.isArray(next.messages)) return;
    state = next;
    status.textContent = next.conversation ? ({ queued: "正在排队，请稍候", assigned: "客服正在接入", active: "已连接客服", closed: "本次咨询已结束" }[next.conversation.status] || "已连接咨询服务") : "咨询服务已准备好";
    typing.textContent = next.is_typing && next.conversation?.status !== "closed" ? "客服正在输入…" : "";
    const signature = JSON.stringify(next.messages);
    if (signature !== messageSignature) {
      const atBottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight < 80;
      for (const objectURL of urls) URL.revokeObjectURL(objectURL);
      urls = []; messages.replaceChildren(...next.messages.map(messageElement)); messageSignature = signature;
      if (atBottom) messages.scrollTop = messages.scrollHeight;
    }
    if (reply && !next.messages.some(item => item.id === reply.id && !item.withdrawn_at)) cancelReply();
    updateReceipts(); updateComposer(); void read();
  }

  /** 保留同一消息编号和引用目标重试，只有服务端确认保存后才清空输入。 */
  async function submit() {
    const text = input.value.trim(); if (!text || send.disabled) return;
    busy = true; updateComposer(); error.textContent = "";
    const replyID = reply?.id;
    const operation = retry && retry.text === text && retry.replyID === replyID ? retry : { text, id: crypto.randomUUID(), replyID };
    retry = operation;
    try { await chat.sendText(operation.text, operation.id, operation.replyID); input.value = ""; retry = null; cancelReply(); void chat.typing(false).catch(() => {}); render(await chat.getState()); }
    catch (problem) { showError(problem); }
    finally { busy = false; updateComposer(); }
  }

  /** 仅上报输入与停止状态，不传递任何未发送正文。 */
  function inputActivity() {
    clearTimeout(typingTimer);
    if (!input.value) { void chat.typing(false).catch(() => {}); return; }
    if (Date.now() - lastTyping >= 2500) { lastTyping = Date.now(); void chat.typing(true).catch(() => {}); }
    typingTimer = setTimeout(() => { void chat.typing(false).catch(() => {}); }, 3000);
  }

  send.onclick = () => { void submit(); };
  document.getElementById("gc-reply-cancel").onclick = cancelReply;
  input.addEventListener("compositionstart", () => { composing = true; });
  input.addEventListener("compositionend", () => { composing = false; });
  input.addEventListener("keydown", event => { if (event.key === "Enter" && !event.shiftKey && !composing && !event.isComposing) { event.preventDefault(); void submit(); } });
  input.addEventListener("input", inputActivity);
  input.addEventListener("blur", () => { clearTimeout(typingTimer); void chat.typing(false).catch(() => {}); });
  file.onchange = async () => {
    const chosen = file.files?.[0]; if (!chosen) return;
    busy = true; updateComposer(); error.textContent = "";
    try { await chat.upload(chosen, chosen.name); render(await chat.getState()); }
    catch (problem) { showError(problem); }
    finally { busy = false; file.value = ""; updateComposer(); }
  };
  messages.addEventListener("scroll", () => { void read(); });
  document.addEventListener("visibilitychange", () => { void read(); });
  window.addEventListener("pagehide", () => { clearTimeout(typingTimer); for (const objectURL of urls) URL.revokeObjectURL(objectURL); }, { once: true });
  chat.subscribe(render);
  chat.ready().then(() => { ready = true; updateComposer(); return chat.getState(); }).then(render).catch(problem => { status.textContent = problem.message; updateComposer(); });
  updateComposer();
})();
