/** 文件作用说明：在任意网页创建隔离样式的客服悬浮窗，聊天按需加载并保留收起前的会话。 */
"use strict";
const script = document.currentScript;
const registry = window.GoodChatWidgets || (window.GoodChatWidgets = Object.create(null));
if (registry[config.entry_key]) return;
const language = new URL(script.src).searchParams.get("lang") || document.documentElement.lang || navigator.language;
const texts = {
  "zh-CN": ["放大窗口", "还原窗口", "收起窗口", "新窗口打开", "在线客服", "留言", "实时连接已建立", "正在连接", "评价"],
  "zh-TW": ["放大視窗", "還原視窗", "收起視窗", "在新視窗開啟", "線上客服", "留言", "即時連線已建立", "正在連線", "評價"],
  en: ["Expand", "Restore", "Minimize", "Open in new window", "Customer support", "Message", "Connected", "Connecting", "Rate"],
  ja: ["拡大", "元に戻す", "最小化", "新しいウィンドウで開く", "カスタマーサポート", "メッセージ", "接続済み", "接続中", "評価"],
  ko: ["확대", "복원", "최소화", "새 창에서 열기", "고객 지원", "메시지", "연결됨", "연결 중", "평가"],
  es: ["Ampliar", "Restaurar", "Minimizar", "Abrir en otra ventana", "Atención al cliente", "Mensaje", "Conectado", "Conectando", "Valorar"],
  "pt-BR": ["Ampliar", "Restaurar", "Minimizar", "Abrir em outra janela", "Atendimento", "Mensagem", "Conectado", "Conectando", "Avaliar"],
  fr: ["Agrandir", "Restaurer", "Réduire", "Ouvrir une autre fenêtre", "Service client", "Message", "Connecté", "Connexion", "Évaluer"],
  de: ["Vergrößern", "Wiederherstellen", "Minimieren", "In neuem Fenster öffnen", "Kundenservice", "Nachricht", "Verbunden", "Verbindung wird hergestellt", "Bewerten"],
};
const locale = Object.keys(texts).find((key) => key.toLowerCase() === language.toLowerCase()) || (language.startsWith("zh") ? "zh-CN" : language.split("-")[0]);
const labels = texts[locale] || texts.en;
const autoKey = "goodchat.widget.auto." + config.entry_key;
const host = document.createElement("div");
host.id = "goodchat-widget-" + config.entry_key;
host.style.cssText = "all:initial;position:fixed;z-index:2147483000;" + config.side + ":" + config.horizontal_offset + "px;bottom:" + config.bottom_offset + "px";
const shadow = host.attachShadow({ mode: "open" });
const style = document.createElement("style");
style.textContent = `
  :host{font-family:system-ui,-apple-system,sans-serif;color:#17243b;color-scheme:light}:host([hidden]){display:none!important}
  *{box-sizing:border-box} [hidden]{display:none!important}
  button,a{-webkit-tap-highlight-color:transparent} button{cursor:pointer;font:inherit}
  button:focus-visible,a:focus-visible{outline:3px solid #17243b;outline-offset:3px}
  .launcher{display:flex;align-items:center;gap:10px;border:0;border-radius:999px;padding:10px 18px;min-height:56px;background:${config.color};color:#fff;box-shadow:0 4px 20px #17243b26;max-width:min(280px,calc(100vw - 32px));font-size:14px;font-weight:600}
  .launcher{touch-action:none;user-select:none;-webkit-user-select:none;transform:translate(var(--gc-drag-x,0px),var(--gc-drag-y,0px))}
  .launcher:hover{filter:brightness(.94)} .launcher:active{cursor:grabbing}
  .launcher img{width:36px;height:36px;object-fit:contain;border-radius:50%;background:white}
  .launcher svg{width:26px;height:26px;flex-shrink:0} .launcher span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  .panel{position:absolute;${config.side}:0;bottom:0;width:min(${config.width}px,calc(100vw - ${config.horizontal_offset + 8}px));height:min(${config.height}px,calc(100dvh - ${config.bottom_offset + 8}px));display:flex;flex-direction:column;background:#fff;box-shadow:0 8px 40px #17243b30;border:1px solid #e2e7ef;border-radius:16px;overflow:hidden}
  .panel.maximized{position:fixed;inset:16px;width:auto;height:auto}
  .bar{display:flex;align-items:center;gap:4px;padding:6px 8px 6px 16px;background:${config.color};color:#fff;min-height:56px}
  .brand-logo{width:28px;height:28px;object-fit:contain;border-radius:50%;background:#fff;margin-right:6px;flex:none}.connection{width:6px;height:6px;border-radius:50%;background:#fff;opacity:.5;flex:none;margin-right:4px}.connection.connected{background:#baf7d0;opacity:1}.bar svg{width:18px;height:18px;display:block;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}.bar .leave-message{min-width:40px;font-size:12px;padding:0 6px}.title{flex:1;min-width:0;font-size:14px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .bar button,.bar a{display:grid;place-items:center;border:0;border-radius:8px;background:transparent;color:inherit;text-decoration:none;min-width:44px;min-height:44px;font-size:20px}
  .bar button:hover,.bar a:hover{background:#ffffff25}.bar button:focus-visible,.bar a:focus-visible{outline-color:#fff;outline-offset:-3px}
  iframe{display:block;border:0;flex:1;width:100%;min-height:0;background:#fff}
  @media(max-width:600px){:host{${config.side}:12px!important;${config.side === "right" ? "left" : "right"}:auto!important;bottom:max(${config.bottom_offset}px,env(safe-area-inset-bottom))!important}.panel{position:fixed;inset:8px 8px max(8px,env(safe-area-inset-bottom));width:auto;height:auto;border-radius:12px}.panel.maximized{inset:0;border-radius:0}.launcher{max-width:calc(100vw - 24px)}}
`;
shadow.append(style);
const launcher = document.createElement("button");
launcher.type = "button";
launcher.className = "launcher";
launcher.setAttribute("aria-label", config.label);
launcher.setAttribute("aria-expanded", "false");
launcher.setAttribute("aria-controls", "chat-panel");
const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
icon.setAttribute("viewBox", "0 0 24 24");
icon.setAttribute("aria-hidden", "true");
const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
path.setAttribute("d", "M4 3h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H9l-6 4V4a1 1 0 0 1 1-1Zm3 5h10M7 12h7");
path.setAttribute("fill", "none");
path.setAttribute("stroke", "currentColor");
path.setAttribute("stroke-width", "1.8");
path.setAttribute("stroke-linecap", "round");
icon.append(path);
launcher.append(icon);
{
  const logo = document.createElement("img");
  logo.alt = "";
  logo.draggable = false;
  logo.src = config.logo_url || new URL("/logo/goodchat-mark.svg", config.origin).href;
  logo.referrerPolicy = "no-referrer";
  logo.addEventListener("error", () => { if (logo.dataset.fallback) { logo.remove(); icon.removeAttribute("hidden"); } else { logo.dataset.fallback = "1"; logo.src = new URL("/logo/goodchat-mark.svg", config.origin).href; } });
  icon.setAttribute("hidden", "");
  launcher.append(logo);
}
const caption = document.createElement("span");
caption.textContent = config.label;
launcher.append(caption);
const panel = document.createElement("section");
panel.id = "chat-panel";
panel.className = "panel";
panel.hidden = true;
panel.setAttribute("role", "region");
panel.setAttribute("aria-label", config.label);
const bar = document.createElement("div");
bar.className = "bar";
const title = document.createElement("span");
title.className = "title";
title.textContent = config.label;
const brandLogo = document.createElement("img");
brandLogo.className = "brand-logo"; brandLogo.alt = ""; brandLogo.hidden = true;
brandLogo.referrerPolicy = "no-referrer";
brandLogo.addEventListener("error", () => {brandLogo.hidden = true;});
const connection = document.createElement("span");
connection.className = "connection"; connection.title = labels[7];
connection.setAttribute("role", "img"); connection.setAttribute("aria-label", labels[7]);
bar.append(brandLogo, title, connection);
const leaveMessage = document.createElement("button");
leaveMessage.type = "button"; leaveMessage.className = "leave-message";
leaveMessage.textContent = labels[5]; leaveMessage.hidden = true;
leaveMessage.addEventListener("click", () => frame?.contentWindow?.postMessage({type:"goodchat:widget-action",entry_key:config.entry_key,action:leaveMessage.dataset.action || "leave-message"},new URL(config.origin).origin));
bar.append(leaveMessage);
const chatURL = new URL("/c/" + encodeURIComponent(config.entry_key), config.origin);
chatURL.searchParams.set("lang", texts[locale] ? locale : "en");
// 将设备时区作为公开偏好传入，服务时段始终由企业时区计算。
try { chatURL.searchParams.set("timezone", Intl.DateTimeFormat().resolvedOptions().timeZone); } catch { /* 浏览器无法检测时由聊天页确认。 */ }
const newWindow = document.createElement("a");
newWindow.href = chatURL.href;
newWindow.target = "_blank";
newWindow.rel = "noopener noreferrer";
newWindow.title = labels[3];
newWindow.setAttribute("aria-label", labels[3]);
newWindow.append(toolIcon("M14 4h6v6M20 4 10 14M10 4H4v16h16v-6"));
bar.append(newWindow);
/** toolIcon 使用同一画布、线宽和尺寸绘制窗口控件。 */
function toolIcon(shape) {
 const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
 svg.setAttribute("viewBox", "0 0 24 24"); svg.setAttribute("aria-hidden", "true");
 const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
 path.setAttribute("d", shape); svg.append(path); return svg;
}
/** control 创建带可访问名称的原生工具栏按钮。 */
function control(label, symbol, action) {
  const button = document.createElement("button");
  button.type = "button";
  button.title = label;
  button.setAttribute("aria-label", label);
  button.append(toolIcon(symbol));
  button.addEventListener("click", action);
  bar.append(button);
  return button;
}
let opened = false, expanded = false, frame = null, autoTimer = null, destroyed = false, autoDone = false;
try { autoDone = sessionStorage.getItem(autoKey) === "1"; } catch { /* 存储被禁用时仍在当前文档内只弹一次。 */ }
/** rememberAction 防止访客主动收起后在本次标签页访问中反复被自动弹窗打扰。 */
function rememberAction() {
  autoDone = true;
  clearTimeout(autoTimer);
  try { sessionStorage.setItem(autoKey, "1"); } catch { /* 不依赖第三方存储。 */ }
}
/** withinHours 根据企业时区与星期计算展示时段，跨午夜按开始日归属。 */
function withinHours(now = new Date()) {
  if (!config.schedule_enabled) return true;
  const parts = new Intl.DateTimeFormat("en-US", { timeZone: config.timezone, weekday: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).formatToParts(now);
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  let day = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"].indexOf(values.weekday);
  const clock = values.hour + ":" + values.minute;
  if (config.start > config.end && clock <= config.end) day = (day + 6) % 7;
  return config.weekdays.includes(day) && (config.start <= config.end ? clock >= config.start && clock <= config.end : clock >= config.start || clock <= config.end);
}
/** openChat 在访客展开时才加载真实聊天页，不提前创建会话或调用AI。 */
function openChat() {
  if (destroyed || (!opened && !withinHours())) return;
  rememberAction();
  if (!frame) {
    frame = document.createElement("iframe");
    frame.title = config.label || labels[4];
    const embeddedURL = new URL(chatURL.href);
    embeddedURL.searchParams.set("embed", "1");
    frame.src = embeddedURL.href;
    frame.referrerPolicy = "strict-origin-when-cross-origin";
    panel.append(frame);
  }
  opened = true;
  host.hidden = false;
  panel.hidden = false;
  launcher.hidden = true;
  launcher.setAttribute("aria-expanded", "true");
  minimize.focus({ preventScroll: true });
}
/** closeChat 收起界面并保留iframe，避免丢失输入草稿或重复创建访客身份。 */
function closeChat() {
  rememberAction();
  opened = false;
  panel.hidden = true;
  launcher.hidden = false;
  launcher.setAttribute("aria-expanded", "false");
  host.hidden = !withinHours();
  keepLauncherInView();
  if (!host.hidden) launcher.focus({ preventScroll: true });
}
/** setExpanded 切换全屏附近的放大视图，不重载正在接待的会话。 */
function setExpanded(value) {
  expanded = value;
  panel.classList.toggle("maximized", expanded);
  maximize.title = labels[expanded ? 1 : 0];
  maximize.setAttribute("aria-label", maximize.title);
  maximize.setAttribute("aria-pressed", String(expanded));
  maximize.replaceChildren(toolIcon(expanded ? "M8 4h12v12M4 8h12v12H4Z" : "M5 5h14v14H5Z"));
}
const maximize = control(labels[0], "M5 5h14v14H5Z", () => setExpanded(!expanded));
maximize.setAttribute("aria-pressed", "false");
const minimize = control(labels[2], "M5 12h14", closeChat);
/** onWidgetState 仅信任当前聊天子窗口的同源公开状态。 */
function onWidgetState(event) {
 const data = event.data;
 if (!frame || event.source !== frame.contentWindow || event.origin !== new URL(config.origin).origin || !data || data.type !== "goodchat:widget-state" || data.entry_key !== config.entry_key) return;
 if (typeof data.title === "string" && data.title.trim() && data.title.length <= 200) {title.textContent = data.title; panel.setAttribute("aria-label", data.title);}
 connection.classList.toggle("connected", data.connected === true);
 connection.title = labels[data.connected === true ? 6 : 7]; connection.setAttribute("aria-label", connection.title);
 leaveMessage.hidden = data.can_leave_message !== true && data.can_evaluate !== true;
 leaveMessage.dataset.action = data.can_leave_message === true ? "leave-message" : "evaluation";
 leaveMessage.textContent = labels[data.can_leave_message === true ? 5 : 8];
 if (typeof data.logo_url === "string" && data.logo_url.length <= 2048) {
  try {const url = new URL(data.logo_url || "/logo/goodchat-mark.svg", config.origin); if (url.protocol === "https:" || url.origin === new URL(config.origin).origin) {brandLogo.src = url.href; brandLogo.hidden = false;}} catch { /* 无效品牌地址不影响聊天。 */ }
 }
}
window.addEventListener("message", onWidgetState);
panel.append(bar);
shadow.append(launcher, panel);
let launcherPosition = { x: 0, y: 0 }, launcherDrag = null, suppressLauncherClick = false;
/** moveLauncher 将拖动后的按钮限制在可视区域，保留八像素边距。 */
function moveLauncher(x, y, origin) {
  const edge = 8, width = window.innerWidth, height = window.innerHeight;
  launcherPosition = {
    x: Math.max(edge - origin.left, Math.min(x, width - edge - origin.right)),
    y: Math.max(edge - origin.top, Math.min(y, height - edge - origin.bottom)),
  };
  launcher.style.setProperty("--gc-drag-x", launcherPosition.x + "px");
  launcher.style.setProperty("--gc-drag-y", launcherPosition.y + "px");
}
/** keepLauncherInView 在窗口缩放或横竖屏变化后收回超出屏幕的按钮。 */
function keepLauncherInView() {
  if (opened || host.hidden || destroyed) return;
  const rect = launcher.getBoundingClientRect();
  moveLauncher(launcherPosition.x, launcherPosition.y, {
    left: rect.left - launcherPosition.x, right: rect.right - launcherPosition.x,
    top: rect.top - launcherPosition.y, bottom: rect.bottom - launcherPosition.y,
  });
}
/** startLauncherDrag 仅处理主指针，拖动整个图文按钮而不影响页面其他手势。 */
function startLauncherDrag(event) {
  if (event.button !== 0 || event.isPrimary === false || launcherDrag) return;
  const rect = launcher.getBoundingClientRect();
  suppressLauncherClick = false;
  launcherDrag = { id: event.pointerId, x: event.clientX, y: event.clientY,
    position: { ...launcherPosition }, origin: {
      left: rect.left - launcherPosition.x, right: rect.right - launcherPosition.x,
      top: rect.top - launcherPosition.y, bottom: rect.bottom - launcherPosition.y,
    }, moved: false };
  launcher.setPointerCapture(event.pointerId);
}
/** dragLauncher 设定移动阈值，短按仍正常打开聊天，拖拽不误触发打开。 */
function dragLauncher(event) {
  if (!launcherDrag || event.pointerId !== launcherDrag.id) return;
  const dx = event.clientX - launcherDrag.x, dy = event.clientY - launcherDrag.y;
  if (!launcherDrag.moved && Math.hypot(dx, dy) < 6) return;
  launcherDrag.moved = true;
  moveLauncher(launcherDrag.position.x + dx, launcherDrag.position.y + dy, launcherDrag.origin);
}
/** finishLauncherDrag 结束或取消手势后吞掉其合成点击，键盘激活保持可用。 */
function finishLauncherDrag(event) {
  if (!launcherDrag || event.pointerId !== launcherDrag.id) return;
  suppressLauncherClick = launcherDrag.moved;
  launcherDrag = null;
  if (launcher.hasPointerCapture(event.pointerId)) launcher.releasePointerCapture(event.pointerId);
}
launcher.addEventListener("pointerdown", startLauncherDrag);
launcher.addEventListener("pointermove", dragLauncher);
launcher.addEventListener("pointerup", finishLauncherDrag);
launcher.addEventListener("pointercancel", finishLauncherDrag);
launcher.addEventListener("lostpointercapture", finishLauncherDrag);
launcher.addEventListener("click", (event) => {
  if (suppressLauncherClick && event.detail !== 0) { suppressLauncherClick = false; event.preventDefault(); return; }
  openChat();
});
window.addEventListener("resize", keepLauncherInView);
/** onEscape 允许从悬浮窗工具栏按Esc收起，避免劫持官网其他区域的键盘事件。 */
function onEscape(event) { if (event.key === "Escape" && opened) closeChat(); }
host.addEventListener("keydown", onEscape);
/** refreshHours 隐藏时段外入口，已打开的咨询允许继续直至访客主动收起。 */
function refreshHours() {
  if (destroyed) return;
  const visible = withinHours();
  host.hidden = !visible && !opened;
  if (!visible) { clearTimeout(autoTimer); autoTimer = null; }
  if (visible && !opened && !autoDone && config.auto_open_delay_seconds >= 0 && autoTimer === null) {
    autoTimer = setTimeout(() => { autoTimer = null; if (withinHours()) openChat(); }, config.auto_open_delay_seconds * 1000);
  }
}
const api = {
  open: openChat, close: closeChat, toggle: () => opened ? closeChat() : openChat(),
  maximize: () => { openChat(); setExpanded(true); }, restore: () => setExpanded(false),
  /** destroy 释放当前组件的DOM、定时器及注册，支持单页应用卸载。 */
  destroy() {
    destroyed = true;
    clearTimeout(showTimer); clearTimeout(autoTimer); clearInterval(hoursTimer);
    window.removeEventListener("message", onWidgetState);
    window.removeEventListener("resize", keepLauncherInView);
    host.remove(); delete registry[config.entry_key];
    if (window.GoodChatWidget === api) delete window.GoodChatWidget;
  },
};
registry[config.entry_key] = api;
if (!window.GoodChatWidget) window.GoodChatWidget = api;
host.hidden = true;
/** mount 等待body就绪后装载，避免脚本放在head时页面尚未解析。 */
function mount() { if (destroyed) return; document.body.append(host); refreshHours(); }
const showTimer = setTimeout(() => {
  if (document.body) mount();
  else document.addEventListener("DOMContentLoaded", mount, { once: true });
}, config.show_delay_seconds * 1000);
const hoursTimer = setInterval(() => { if (host.isConnected) refreshHours(); }, 30000);
