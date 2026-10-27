/** 文件作用说明：提供使用受限 GoodChat SDK 的代码装修初稿，消息以 textContent 安全呈现。 */
import type { Design } from "./templates";
/** starterCodeDesign 返回连接真实聊天的代码装修起点，凭证始终由可信父页面持有。 */
export function starterCodeDesign(): Design {
  return {
    html: `<main>
  <header>
    <h1>欢迎咨询</h1>
    <p id="status">正在连接客服</p>
  </header>
  <div id="messages" role="log" aria-live="polite"></div>
  <p id="typing" role="status" aria-live="polite"></p>
  <p id="error" role="alert"></p>
  <div id="reply" hidden>
    <span id="reply-text"></span><button id="cancel-reply" type="button">取消引用</button>
  </div>
  <textarea id="text" aria-label="消息内容" placeholder="输入消息，Enter 发送"></textarea>
  <div class="actions">
    <input id="file" type="file" aria-label="上传附件" /><button id="send" type="button">
      发送
    </button>
  </div>
</main>
`,
    css: `body {
  margin: 0;
  background: #f4f6f2;
  color: #21342c;
  font-family: system-ui;
}
main {
  max-width: 720px;
  margin: auto;
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}
header {
  padding: 18px 22px;
  background: #23684e;
  color: white;
}
h1 {
  font-size: 20px;
  margin: 0;
}
header p {
  font-size: 12px;
  margin: 6px 0 0;
}
#messages {
  height: 55vh;
  overflow: auto;
  padding: 18px;
}
article {
  max-width: 85%;
  background: white;
  border: 1px solid #dfe6dc;
  border-radius: 12px;
  padding: 12px;
  margin-bottom: 14px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
article.own {
  margin-left: auto;
  background: #e2eddd;
}
.receipt {
  text-align: right;
  margin-top: 5px;
}
blockquote {
  margin: 0 0 8px;
  padding-left: 8px;
  border-left: 2px solid #a7b99f;
  color: #68776e;
  font-size: 12px;
}
#typing {
  color: #68776e;
  padding: 0 18px;
  font-size: 12px;
  min-height: 1em;
}
#reply {
  padding: 8px 18px;
  color: #68776e;
  font-size: 12px;
}
.message-actions {
  margin-top: 8px;
  display: flex;
  gap: 6px;
}
.message-actions button,
#cancel-reply {
  background: transparent;
  color: #23684e;
  padding: 2px 6px;
  font-size: 11px;
}
small {
  display: block;
  color: #68776e;
  font-size: 11px;
  margin-bottom: 5px;
}
textarea {
  margin: 0 18px;
  min-height: 64px;
  border: 1px solid #dfe6dc;
  border-radius: 10px;
  padding: 12px;
  font: inherit;
}
.actions {
  padding: 12px 18px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
button {
  background: #23684e;
  color: white;
  border: 0;
  padding: 10px 22px;
  border-radius: 8px;
  cursor: pointer;
}
button:disabled {
  opacity: 0.5;
}
#error {
  color: #b63d35;
  padding: 0 18px;
  font-size: 12px;
}
`,
    javascript: `const chat = window.GoodChat,
  input = document.getElementById("text"),
  send = document.getElementById("send"),
  messages = document.getElementById("messages"),
  status = document.getElementById("status"),
  error = document.getElementById("error"),
  file = document.getElementById("file"),
  typing = document.getElementById("typing"),
  replyBox = document.getElementById("reply"),
  replyText = document.getElementById("reply-text"),
  cancelReply = document.getElementById("cancel-reply");
let composing = false,
  retry = null,
  state = null,
  replyTo = undefined,
  readSeq = 0n;
/** clearReply 取消当前引用，避免引用已撤回或已离开当前状态的消息。 */
function clearReply() {
  replyTo = undefined;
  replyBox.hidden = true;
  replyText.textContent = "";
}
/** render 展示当前授权消息，避免执行访客提交的HTML。 */
function render(next) {
  state = next;
  if (replyTo && !(state.messages || []).some((item) => item.id === replyTo && !item.withdrawn_at))
    clearReply();
  const bottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight < 80;
  messages.replaceChildren();
  for (const item of state.messages || []) {
    const row = document.createElement("article"),
      label = document.createElement("small");
    row.className = item.is_own ? "own" : "";
    const date = new Date(item.created_at || ""),
      time = Number.isNaN(date.getTime())
        ? ""
        : date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
    label.textContent =
      (item.is_own ? "我" : item.sender_kind === "system" ? "系统" : "客服") +
      (item.sender_name ? " · " + item.sender_name : "") +
      (time ? " · " + time : "");
    row.append(label);
    if (!item.withdrawn_at && item.reply) {
      const quote = document.createElement("blockquote");
      quote.textContent =
        "引用：" + (item.reply.withdrawn ? "消息已撤回" : item.reply.content || "附件消息");
      row.append(quote);
    }
    const text = document.createElement("span");
    text.textContent = item.withdrawn_at
      ? "消息已撤回"
      : item.content || item.text || (item.attachment ? "附件消息" : "");
    row.append(text);
    const seq = String(item.seq || "0"),
      peer = String(state.peer_read_seq || "0");
    if (item.is_own && !item.withdrawn_at && /^\\d+$/.test(seq) && /^\\d+$/.test(peer)) {
      const receipt = document.createElement("small");
      receipt.className = "receipt";
      receipt.textContent = BigInt(seq) > 0n && BigInt(peer) >= BigInt(seq) ? "对方已读" : "已发送";
      row.append(receipt);
    }
    if (!item.withdrawn_at && state.conversation?.status !== "closed") {
      const actions = document.createElement("div"),
        quote = document.createElement("button");
      actions.className = "message-actions";
      quote.type = "button";
      quote.textContent = "引用";
      quote.onclick = () => {
        replyTo = item.id;
        replyText.textContent = "引用：" + (item.content || item.text || "附件消息").slice(0, 100);
        replyBox.hidden = false;
        input.focus();
      };
      actions.append(quote);
      if (item.is_own && !Number.isNaN(date.getTime()) && Date.now() - date.getTime() < 120000) {
        const withdraw = document.createElement("button"),
          cancel = document.createElement("button");
        let confirmed = false;
        withdraw.type = cancel.type = "button";
        withdraw.textContent = "撤回";
        cancel.textContent = "取消";
        cancel.hidden = true;
        cancel.onclick = () => {
          confirmed = false;
          withdraw.textContent = "撤回";
          cancel.hidden = true;
        };
        withdraw.onclick = async () => {
          if (!confirmed) {
            confirmed = true;
            withdraw.textContent = "确认撤回";
            cancel.hidden = false;
            return;
          }
          withdraw.disabled = true;
          try {
            await chat.withdrawMessage(item.id);
          } catch (problem) {
            error.textContent = problem.message;
          } finally {
            withdraw.disabled = false;
          }
        };
        actions.append(withdraw);
        actions.append(cancel);
      }
      row.append(actions);
    }
    messages.append(row);
  }
  if (bottom) messages.scrollTop = messages.scrollHeight;
  status.textContent =
    state.conversation?.status === "closed"
      ? "本次咨询已结束"
      : state.connected
        ? "已连接客服"
        : "正在恢复连接";
  typing.textContent = state.is_typing ? "客服正在输入…" : "";
  input.disabled = state.conversation?.status === "closed";
  file.disabled = input.disabled;
  send.disabled = input.disabled;
  void markRead();
}
/** markRead 只在消息可见并位于底部时提交精确字符串已读游标。 */
async function markRead() {
  if (
    !state ||
    document.visibilityState !== "visible" ||
    messages.scrollHeight - messages.scrollTop - messages.clientHeight > 80
  )
    return;
  let seq = 0n;
  for (const item of state.messages || []) {
    const value = String(item.seq || "0");
    if (/^\\d+$/.test(value) && BigInt(value) > seq) seq = BigInt(value);
  }
  if (seq <= readSeq) return;
  try {
    const result = await chat.read(seq.toString());
    if (result?.accepted !== false) readSeq = seq;
  } catch {
    /* 下次状态更新可重试。 */
  }
}
/** submit 发送消息，失败重试沿用同一client_message_id。 */
async function submit() {
  if (composing || send.disabled || !input.value.trim()) return;
  const operation =
    retry && retry.text === input.value && retry.reply === replyTo
      ? retry
      : { text: input.value, id: crypto.randomUUID(), reply: replyTo };
  retry = operation;
  send.disabled = true;
  error.textContent = "";
  try {
    await chat.sendText(operation.text, operation.id, operation.reply);
    if (input.value === operation.text) input.value = "";
    retry = null;
    clearReply();
    chat.typing(false).catch(() => {});
  } catch (problem) {
    error.textContent = problem.message;
  } finally {
    send.disabled = false;
  }
}
send.onclick = submit;
cancelReply.onclick = clearReply;
input.addEventListener("compositionstart", () => {
  composing = true;
});
input.addEventListener("compositionend", () => {
  composing = false;
});
input.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.shiftKey && !composing && !event.isComposing) {
    event.preventDefault();
    submit();
  }
});
input.addEventListener("input", () => chat.typing(!!input.value).catch(() => {}));
file.onchange = async () => {
  const chosen = file.files?.[0];
  if (!chosen) return;
  try {
    await chat.upload(chosen, chosen.name);
  } catch (problem) {
    error.textContent = problem.message;
  } finally {
    file.value = "";
  }
};
messages.addEventListener("scroll", markRead);
document.addEventListener("visibilitychange", markRead);
chat.subscribe(render);
chat.ready().catch((problem) => {
  status.textContent = problem.message;
  send.disabled = true;
});
`,
  };
}
