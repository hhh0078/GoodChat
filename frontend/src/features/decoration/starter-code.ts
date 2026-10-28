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
  <div class="composer">
  <div class="capsule"><button id="add" type="button" aria-label="添加附件" aria-expanded="false">＋</button><textarea id="text" rows="1" aria-label="消息内容" placeholder="输入消息…"></textarea><button id="send" type="button" aria-label="发送消息">➤</button></div>
  <div id="attachment-menu" hidden><button type="button" data-kind="image">图片</button><button type="button" data-kind="file">文件</button><button type="button" data-kind="video">视频</button></div>
  </div>
  <input id="file" type="file" aria-label="选择附件" hidden />
</main>
`,
    css: `body {
  margin: 0;
  background: #f5f7fb;
  color: #182238;
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
  background: #165dff;
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
  border: 1px solid #dce5f1;
  border-radius: 12px;
  padding: 12px;
  margin-bottom: 14px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
article.own {
  margin-left: auto;
  background: #eaf2ff;
}
.sender-avatar { width: 32px; height: 32px; object-fit: cover; border-radius: 50%; float: left; margin: 0 8px 5px 0; }
article.own .sender-avatar { float: right; margin: 0 0 5px 8px; }
.receipt {
  text-align: right;
  margin-top: 5px;
}
blockquote {
  margin: 0 0 8px;
  padding-left: 8px;
  border-left: 2px solid #adc4e7;
  color: #65758f;
  font-size: 12px;
}
#typing {
  color: #65758f;
  padding: 0 18px;
  font-size: 12px;
  min-height: 1em;
}
#reply {
  padding: 8px 18px;
  color: #65758f;
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
  color: #165dff;
  padding: 2px 6px;
  font-size: 11px;
}
small {
  display: block;
  color: #65758f;
  font-size: 11px;
  margin-bottom: 5px;
}
textarea {
  margin: 0 18px;
  min-height: 64px;
  border: 1px solid #dce5f1;
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
  background: #165dff;
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
.composer {position:relative;}
.capsule { display:flex; align-items:flex-end; gap:7px; margin:10px 14px 14px; padding:7px; border:1px solid #dce5f1; border-radius:28px; background:white; }
.capsule textarea {flex:1;min-width:0;min-height:0;max-height:120px;margin:0;padding:8px 2px;border:0;outline:none;border-radius:0;resize:none;line-height:24px;font-size:14px;}
#add,#send {flex-shrink:0;width:38px;height:38px;border-radius:50%;padding:0;font-size:20px;}
#add { background:transparent;color:inherit;font-size:26px; }
#attachment-menu {position:absolute;z-index:10;left:14px;bottom:calc(100% + 2px);display:flex;gap:6px;padding:8px;background:#fff;border:1px solid #dce5f1;border-radius:12px;box-shadow:0 8px 24px #172b4d18;}
#attachment-menu button {font-size:13px;padding:6px 12px;}
#attachment-menu[hidden],.message-actions[hidden] {display:none;}
article:focus-visible {outline:2px solid #165dff;outline-offset:3px;}
.message-attachment img,.message-attachment video {max-width:100%;max-height:280px;border-radius:10px;}
.message-options {display:flex;flex-wrap:wrap;gap:6px;margin-top:8px;}
.message-options button,.message-file-save {font-size:12px;padding:6px 10px;}

`,
    javascript: `const chat = window.GoodChat,
  input = document.getElementById("text"),
  send = document.getElementById("send"),
  add = document.getElementById("add"),
  attachmentMenu = document.getElementById("attachment-menu"),
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
const avatarFiles = new Map();
let avatarURLs = [];
let selectedMessage = "";
/** render 展示当前授权消息，避免执行访客提交的HTML。 */
function render(next) {
  state = next;
  if (replyTo && !(state.messages || []).some((item) => item.id === replyTo && !item.withdrawn_at))
    clearReply();
  const bottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight < 80;
  for (const url of avatarURLs) URL.revokeObjectURL(url);
  avatarURLs = [];
  messages.replaceChildren();
  for (const item of state.messages || []) {
    const row = document.createElement("article"),
      label = document.createElement("small");
    row.className = item.is_own ? "own" : "";
    row.dataset.clientMessageId = item.client_message_id || "";
    row.dataset.messageId = item.id;
    const date = new Date(item.created_at || ""),
      time = Number.isNaN(date.getTime())
        ? ""
        : date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
    label.textContent =
      (item.is_own ? "我" : item.sender_kind === "system" ? "系统" : "客服") +
      (item.sender_name ? " · " + item.sender_name : "") +
      (time ? " · " + time : "");
    if (item.sender_avatar_url && typeof chat.getAvatar === "function") {
      const avatar = document.createElement("img");
      avatar.className = "sender-avatar";
      avatar.width = avatar.height = 32;
      avatar.alt = (item.sender_name || "聊天参与者") + "头像";
      avatar.referrerPolicy = "no-referrer";
      row.append(avatar);
      if (!avatarFiles.has(item.sender_avatar_url)) {
        if (avatarFiles.size >= 50) avatarFiles.delete(avatarFiles.keys().next().value);
        avatarFiles.set(item.sender_avatar_url, chat.getAvatar(item.id).then((result) => result.file));
      }
      avatarFiles.get(item.sender_avatar_url).then((file) => {
        if (!avatar.isConnected) return;
        const url = URL.createObjectURL(file);
        avatarURLs.push(url);
        avatar.src = url;
      }).catch(() => { avatar.remove(); avatarFiles.delete(item.sender_avatar_url); });
    }
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
    if (!item.withdrawn_at && item.attachment) {
      const media = document.createElement("div"); media.className = "message-attachment";
      const save = document.createElement("button"); save.type="button"; save.className="message-file-save"; save.textContent="下载 "+(item.attachment.file_name||"附件");
      save.onclick=async()=>{save.disabled=true;try{await chat.saveAttachment(item.attachment.id);}catch(problem){error.textContent=problem.message;}finally{save.disabled=false;}};
      media.append(save); row.append(media);
      if (["image","audio","video"].some(kind=>String(item.attachment.content_type||"").startsWith(kind+"/"))) chat.download(item.attachment.id).then(result=>{
        if(!media.isConnected)return;
        const objectURL=URL.createObjectURL(result.file);avatarURLs.push(objectURL);
        const kind=item.attachment.content_type.startsWith("image/")?"img":item.attachment.content_type.startsWith("audio/")||item.message_type==="audio"?"audio":"video";
        const element=document.createElement(kind);element.src=objectURL;element.alt=item.attachment.file_name||"图片消息";element.controls=true;element.playsInline=true;media.append(element);
      }).catch(problem=>{if(media.isConnected)error.textContent=problem.message;});
    }
    if (!item.withdrawn_at && ["system","agent"].includes(item.sender_kind) && Array.isArray(item.options)) {
      const options=document.createElement("div");options.className="message-options";
      for(const option of item.options.slice(0,8)){
        if(typeof option.label!=="string"||typeof option.value!=="string"||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(option.client_message_id||""))continue;
        const button=document.createElement("button");button.type="button";button.textContent=option.label;
        button.disabled=state.conversation?.status==="closed"||state.messages.some(message=>message.client_message_id===option.client_message_id);
        button.onclick=async()=>{button.disabled=true;try{await chat.sendText(option.value,option.client_message_id);}catch(problem){button.disabled=false;error.textContent=problem.message;}};options.append(button);
      }row.append(options);
    }
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
      actions.hidden = selectedMessage !== item.id;
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
    /** selectMessage 只选择消息本身，媒体播放器、选项和动作按钮保留自己的点击行为。 */
    function selectMessage(event) {
      if(item.withdrawn_at||event?.target?.closest?.("button,audio,video,a,input,.message-attachment"))return;
      selectedMessage=selectedMessage===item.id?"":item.id;
      for(const menu of messages.querySelectorAll(".message-actions"))menu.hidden=menu.parentElement.dataset.messageId!==selectedMessage;
    }
    row.tabIndex=item.withdrawn_at?-1:0;row.onclick=selectMessage;
    row.onkeydown=event=>{if(event.target===row&&(event.key==="Enter"||event.key===" ")){event.preventDefault();selectMessage(event);}};
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
    const saved=chat.sendText(operation.text, operation.id, operation.reply);
    if(typeof chat.animateSend==="function")chat.animateSend({input,messages,client_message_id:operation.id,text:operation.text,after:saved});
    await saved;
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
add.onclick=()=>{attachmentMenu.hidden=!attachmentMenu.hidden;add.setAttribute("aria-expanded",String(!attachmentMenu.hidden));};
for(const button of attachmentMenu.querySelectorAll("button"))button.onclick=()=>{file.accept=button.dataset.kind==="image"?"image/*":button.dataset.kind==="video"?"video/*":"";attachmentMenu.hidden=true;add.setAttribute("aria-expanded","false");file.click();};
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
input.addEventListener("input", () => { input.style.height="auto";input.style.height=Math.min(input.scrollHeight,120)+"px";chat.typing(!!input.value).catch(() => {}); });
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
