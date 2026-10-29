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
    this.fixedScrollHeight = 200; this.scrollPosition = 0; this.clientHeight = 400; this.listeners = {}; this.clicks = 0; this.style = {};
    this.ownText = "";
  }
  /** textContent 读取当前节点及子节点的可见文本。 */
  get textContent() { return this.ownText + this.children.map(child => child.textContent).join(""); }
  /** textContent 写入文本并清空子节点，避免测试替身把文本解析为HTML。 */
  set textContent(value) { this.ownText = String(value); this.replaceChildren(); }
  /** isConnected 判断节点是否仍属于测试文档树。 */
  get isConnected() { return this.connected || Boolean(this.parent?.isConnected); }
  /** parentElement 按真实DOM提供父节点，不创建额外状态。 */
  get parentElement() { return this.parent; }
  /** layoutHeight 给逻辑测试提供确定性行高，临时额外高度代表异步媒体撑开布局。 */
  get layoutHeight() { return this.hidden ? 0 : (this.dataset.messageId ? 100 : 36) + (this.extraHeight || 0); }
  /** scrollHeight 只在消息容器计算受控测试高度，不声称浏览器像素验收。 */
  get scrollHeight() { return this.autoLayout ? Math.max(this.clientHeight, this.children.reduce((sum, child) => sum + child.layoutHeight, 0)) : this.fixedScrollHeight; }
  /** scrollHeight 允许表单节点保留固定的替身测量值。 */
  set scrollHeight(value) { this.fixedScrollHeight = value; }
  /** scrollTop 返回受控测试滚动位置。 */
  get scrollTop() { return this.scrollPosition; }
  /** scrollTop 像真实滚动容器一样将位置裁剪到可滚动范围。 */
  set scrollTop(value) { this.scrollPosition = Math.max(0, Math.min(value, this.scrollHeight - this.clientHeight)); }
  /** getBoundingClientRect 以确定性布局验证稳定消息锚点逻辑。 */
  getBoundingClientRect() {
    if (this.autoLayout) return {top:0,bottom:this.clientHeight,height:this.clientHeight};
    const top = this.parent ? this.parent.getBoundingClientRect().top + this.parent.children.slice(0, this.parent.children.indexOf(this)).reduce((sum, child) => sum + child.layoutHeight, 0) - this.parent.scrollTop : 0;
    return {top,bottom:top+this.layoutHeight,height:this.layoutHeight};
  }
  /** append 按浏览器DOM规则关联子节点。 */
  append(...children) { for (const child of children) { child.parent = this; this.children.push(child); } }
  /** prepend 支持头像等节点在原内容前插入。 */
  prepend(...children) { for (const child of children) child.parent = this; this.children.unshift(...children); }
  /** replaceChildren 替换节点并断开旧节点连接。 */
  replaceChildren(...children) { for (const child of this.children) child.parent = null; this.children = []; this.append(...children); }
  /** matches 识别本测试所需的类、消息锚点属性和标签选择器。 */
  matches(selector) { return selector.split(",").some(part => part === "[data-message-id]" ? this.dataset.messageId !== undefined : part.startsWith(".") ? this.className.split(" ").includes(part.slice(1)) : this.tagName === part.toUpperCase()); }
  /** querySelectorAll 支持真实脚本使用的类、标签与消息锚点属性。 */
  querySelectorAll(selector) { return flatten(this).filter(node => node !== this && node.matches(selector)); }
  /** closest 查找消息选择操作所需的交互祖先。 */
  closest(selector) { return this.matches(selector) ? this : this.parent?.closest(selector) || null; }
  /** addEventListener 保存测试中需要触发的事件处理器。 */
  addEventListener(type, handler) { this.listeners[type] = handler; }
  /** setAttribute 保存辅助阅读属性，不执行任何浏览器行为。 */
  setAttribute(name, value) { this[name] = String(value); }
  /** click 记录用户动作转发并执行实际处理器，不打开系统文件窗口。 */
  click() { this.clicks++; if (!this.disabled) return this.onclick?.({target:this}); }
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
function fixture(kind = "schema", initialState = {}) {
  const elements = new Map(["gc-status", "gc-messages", "gc-text", "gc-send", "gc-file", "gc-add", "gc-attachment-menu", "gc-typing", "gc-error", "gc-reply-bar", "gc-reply-text", "gc-reply-cancel"].map(id => [id, new Element("div", true)]));
  elements.get("gc-messages").autoLayout = true; elements.get("gc-file").hidden = true; elements.get("gc-attachment-menu").hidden = true;
  for (const kind of ["image","file","video"]) { const option = new Element("button"); option.dataset.kind = kind; elements.get("gc-attachment-menu").append(option); }
  const calls = [], timers = new Map(); let counter = 0, subscriber;
  const state = {
    conversation: { id: "test-conversation", status: "active", agent_name: "测试客服" },
    messages: [
      { id: "system", seq: "9007199254740991", sender_kind: "system", text: "测试系统通知", created_at: "2026-10-27T01:00:00Z" },
      { id: "agent", seq: "9007199254740992", sender_kind: "agent", sender_name: "测试客服", text: "测试客服消息", created_at: "2026-10-27T01:01:00Z" },
      { id: "own", seq: "9007199254740993", sender_kind: "visitor", is_own: true, text: "测试本人消息", created_at: "2026-10-27T01:02:00Z" },
    ],
    is_typing: true, peer_read_seq: "9007199254740993",
    ...initialState,
  };
  const chat = {
    /** ready 返回仅用于测试的桥已准备状态。 */
    ready: async () => {},
    /** getState 返回当前测试态副本，避免脚本修改替身事实。 */
    getState: async () => structuredClone(state),
    /** loadOlder 记录当前会话上一页请求，具体延时和失败由用例单独控制。 */
    loadOlder: async () => { calls.push(["loadOlder"]); state.has_older = false; },
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
  const aliases = {status:"gc-status",messages:"gc-messages",text:"gc-text",send:"gc-send",file:"gc-file",add:"gc-add","attachment-menu":"gc-attachment-menu",typing:"gc-typing",error:"gc-error",reply:"gc-reply-bar","reply-text":"gc-reply-text","cancel-reply":"gc-reply-cancel"};
  const document = { visibilityState: "visible", getElementById: id => elements.get(aliases[id] || id), createElement: tag => new Element(tag), addEventListener() {} };
  const window = { GoodChat: chat, addEventListener() {} };
  const source = kind === "schema" ? readFileSync(new URL("./schema_chat.js", import.meta.url), "utf8") : runInNewContext(readFileSync(new URL("../../../frontend/src/features/decoration/starter-code.ts", import.meta.url), "utf8").match(/javascript:\s*(`[^`]*`)/)[1]);
  runInNewContext(source, {
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
  assert.equal(f.elements.get("gc-file").hidden, true);
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

/** pagedMessages 创建明确标记的纯逻辑消息，不写入实际租户或聊天。 */
function pagedMessages() { return Array.from({length:8}, (_, index) => ({id:`page-${index+1}`,seq:String(index+1),sender_kind:"agent",text:`分页逻辑消息${index+1}`,created_at:"2026-10-29T01:00:00Z"})); }

/** visibleAnchor 读取确定性替身布局里的首条可见消息和视口偏移。 */
function visibleAnchor(messages) { const row = messages.querySelectorAll("[data-message-id]").find(row => row.getBoundingClientRect().bottom > 0); return {id:row.dataset.messageId,offset:row.getBoundingClientRect().top}; }

for (const kind of ["schema", "starter"]) {
  test(`${kind} 首次只定位到底，上翻请求不并发，历史和并发新消息保持锚点且不误已读`, async () => {
    const f=fixture(kind,{messages:pagedMessages(),has_older:true}); await settle();
    const messages=f.elements.get("gc-messages");
    let resolve; let requests=0;
    f.chat.loadOlder=()=>{requests++;return new Promise(done=>{resolve=done;});};
    messages.listeners.scroll(); await settle();
    assert.equal(requests,0,"初始程序定位不应把历史全部拉完");
    messages.scrollTop=160; messages.listeners.scroll();
    f.calls.length=0;
    const start=button(messages,"加载更早消息").click();
    messages.scrollTop=260; messages.listeners.scroll();
    messages.scrollTop=250; messages.listeners.scroll();
    const movedAnchor=visibleAnchor(messages);
    assert.equal(requests,1);
    f.state.messages.push({id:"concurrent-new",seq:"9",sender_kind:"agent",text:"并发新消息",created_at:"2026-10-29T01:01:00Z"});f.publish();
    assert.deepEqual(visibleAnchor(messages),movedAnchor,"并发新增不能拉走历史阅读位置");
    assert.equal(f.calls.some(call=>call[0]==="read"&&call[1]==="9"),false,"未到底不能上报新消息已读");
    f.state.messages.unshift({id:"older-page",seq:"0",sender_kind:"agent",text:"更早分页记录",created_at:"2026-10-28T01:00:00Z"});f.state.has_older=false;
    resolve();await start;await settle();
    assert.deepEqual(visibleAnchor(messages),movedAnchor,"前插历史不能因尾部新消息而偏移");
    const oldest=messages.querySelectorAll("[data-message-id]").find(row=>row.dataset.messageId==="older-page");
    oldest.extraHeight=180;messages.listeners.load();
    assert.deepEqual(visibleAnchor(messages),movedAnchor,"慢图片撑高后仍保持同一消息偏移");
    messages.scrollTop=0;messages.listeners.scroll();await settle();assert.equal(requests,1,"没有更多历史后应停止请求");
  });

  test(`${kind} 顶部自动加载失败后显式重试，错误不是静默清空历史`, async () => {
    const f=fixture(kind,{messages:pagedMessages(),has_older:true}); await settle();
    const messages=f.elements.get("gc-messages");let requests=0;
    f.chat.loadOlder=async()=>{requests++;if(requests===1)throw new Error("测试分页网络失败 <script>text-only</script>");f.state.has_older=false;};
    messages.scrollTop=0;messages.listeners.scroll();await settle();
    assert.equal(requests,1);assert.match(messages.textContent,/测试分页网络失败/);
    assert.ok(messages.textContent.includes("<script>text-only</script>"));
    messages.scrollTop=1;messages.listeners.scroll();messages.scrollTop=0;messages.listeners.scroll();await settle();
    assert.equal(requests,1,"加载失败后不能自动死循环");
    button(messages,"重试加载更早消息").click();await settle();assert.equal(requests,2);
    assert.equal(messages.querySelectorAll("[data-message-id]").length,8,"失败和重试不得丢失已有历史");
    assert.equal(messages.querySelectorAll(kind==="schema"?".gc-history-control":".history-control")[0].hidden,true);
  });

  test(`${kind} 空选项不生成宽容器，底部跟随和慢媒体继续到最新`, async () => {
    const rows=pagedMessages();rows[0].options=[];
    const f=fixture(kind,{messages:rows,has_older:false});await settle();
    const messages=f.elements.get("gc-messages");
    assert.equal(messages.querySelectorAll(kind==="schema"?".gc-message-options":".message-options").length,0);
    f.state.messages.push({id:"latest",seq:"9",sender_kind:"agent",text:"新消息",created_at:"2026-10-29T01:01:00Z"});f.publish();await settle();
    assert.equal(messages.scrollHeight-messages.scrollTop-messages.clientHeight,0);
    messages.querySelectorAll("[data-message-id]")[0].extraHeight=160;messages.listeners.loadedmetadata();
    assert.equal(messages.scrollHeight-messages.scrollTop-messages.clientHeight,0);
  });

  test(`${kind} 历史请求期间用户回到底部后恢复跟随最新位置`, async () => {
    const f=fixture(kind,{messages:pagedMessages(),has_older:true});await settle();
    const messages=f.elements.get("gc-messages");let resolve;
    f.chat.loadOlder=()=>new Promise(done=>{resolve=done;});
    messages.scrollTop=0;messages.listeners.scroll();
    messages.scrollTop=messages.scrollHeight;messages.listeners.scroll();
    f.state.messages.unshift({id:"older-while-returning",seq:"0",sender_kind:"agent",text:"更早记录",created_at:"2026-10-28T01:00:00Z"});f.state.has_older=false;
    resolve();await settle();
    assert.equal(messages.scrollHeight-messages.scrollTop-messages.clientHeight,0,"用户新选择到底部后不能被旧历史锚点拉回");
  });
}
