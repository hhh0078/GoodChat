// 文件作用说明：仅在隔离编辑预览中高亮作者节点，通过精确父来源和一次性标识传回选择信息。
let mode = "select";
let selectedID = "";
let selectedStyle = "";
let overlay = null;
let hover = null;
const idPattern = /^[A-Za-z][A-Za-z0-9_-]{0,127}$/;
const messageSelectors = Object.freeze([
  ".gc-message-row.gc-own .gc-message",
  ".gc-message-row:not(.gc-own):not(.gc-system) .gc-message",
  ".gc-message-row.gc-system .gc-message",
  ".gc-avatar", ".gc-message-meta", ".gc-read-status", ".gc-welcome-option",
  ".gc-reply", ".gc-file-save", ".gc-attachment-preview",
]);

/** send 仅向服务端确定的父来源报告当前短期快照，不携带源码、身份或凭证。 */
function send(type, detail = {}) {
  parent.postMessage({ type, release_id: cfg.release_id, nonce: cfg.nonce, ...detail }, cfg.origin);
}

/** authoredElement 将动态聊天子节点归到最近的作者源码节点，不把运行时内容序列化。 */
function authoredElement(target) {
  const element = target instanceof Element ? target.closest("[data-gc-edit-id]") : null;
  if (!element || !idPattern.test(element.getAttribute("data-gc-edit-id") || "")) return null;
  return element;
}

/** findElement 按静态作者编号查找，不将来自管理端的字符串拼成CSS选择器。 */
function findElement(id) {
  if (!idPattern.test(id)) return null;
  for (const element of document.querySelectorAll("[data-gc-edit-id]")) {
    if (element.getAttribute("data-gc-edit-id") === id) return element;
  }
  return null;
}

/** messageStyleTarget 将官方动态聊天元素映射为有限样式选择器，绝不把运行时消息正文带回源码。 */
function messageStyleTarget(target, source) {
  if (!(target instanceof Element) || source?.id !== "gc-messages") return null;
  for (const selector of messageSelectors.slice(3)) {
    const element = target.closest(selector);
    if (element && source.contains(element)) return { element, selector };
  }
  const bubble = target.closest(".gc-message");
  if (!bubble || !source.contains(bubble)) return null;
  const row = bubble.closest(".gc-message-row");
  const selector = row?.classList.contains("gc-own") ? messageSelectors[0] : row?.classList.contains("gc-system") ? messageSelectors[2] : messageSelectors[1];
  return { element: bubble, selector };
}

/** selectedElement 在消息刷新后重新定位同种样式的首个节点，不接收任意选择器或可执行表达式。 */
function selectedElement() {
  const source = findElement(selectedID);
  return source && selectedStyle && messageSelectors.includes(selectedStyle) ? source.querySelector(selectedStyle) || source : source;
}

/** drawOutline 用不拦截触摸和点击的覆盖层提示当前元素，不改写模板自己的CSS。 */
function drawOutline(element) {
  if (!overlay) return;
  if (mode !== "select" || !element) {
    overlay.hidden = true;
    return;
  }
  const rect = element.getBoundingClientRect();
  overlay.hidden = false;
  overlay.style.left = `${Math.max(0, rect.left)}px`;
  overlay.style.top = `${Math.max(0, rect.top)}px`;
  overlay.style.width = `${Math.max(0, Math.min(rect.width, innerWidth - Math.max(0, rect.left)))}px`;
  overlay.style.height = `${Math.max(0, Math.min(rect.height, innerHeight - Math.max(0, rect.top)))}px`;
}

/** refreshOutline 在滚动或窗口改变时让选择框跟随当前源节点。 */
function refreshOutline() {
  drawOutline(hover || selectedElement());
}

/** selectElement 只报告可映射回作者源码的标识和标签，核心聊天节点仍由管理端保护。 */
function selectElement(element, target = element) {
  const dynamic = messageStyleTarget(target, element);
  selectedID = element.getAttribute("data-gc-edit-id");
  selectedStyle = dynamic?.selector || "";
  hover = null;
  drawOutline(dynamic?.element || element);
  send("goodchat:editor-select", { element_id: selectedID, tag: element.tagName.toLowerCase(), ...(selectedStyle ? { style_selector: selectedStyle } : {}) });
}

/** receiveCommand 只接受当前父窗口、精确来源、快照与握手标识全部匹配的受限命令。 */
function receiveCommand(event) {
  const value = event.data;
  if (event.source !== parent || event.origin !== cfg.origin || !value || typeof value !== "object" || value.release_id !== cfg.release_id || value.nonce !== cfg.nonce) return;
  if (value.type === "goodchat:editor-mode" && (value.mode === "select" || value.mode === "interact")) {
    mode = value.mode;
    hover = null;
    refreshOutline();
  } else if (value.type === "goodchat:editor-highlight" && typeof value.element_id === "string") {
    const element = findElement(value.element_id);
    if (element) {
      if (selectedID !== value.element_id) selectedStyle = "";
      selectedID = value.element_id;
      if (typeof value.style_selector === "string" && messageSelectors.includes(value.style_selector)) selectedStyle = value.style_selector;
      hover = null;
      refreshOutline();
    }
  }
}

/** inspectClick 在选择模式先截获交互，避免编辑时意外发送消息或打开业务表单。 */
function inspectClick(event) {
  if (mode !== "select") return;
  event.preventDefault();
  event.stopImmediatePropagation();
  const element = authoredElement(event.target);
  if (element) selectElement(element, event.target);
}

/** inspectPointer 在选择模式阻止输入框提前获得焦点，避免选中元素后误触发快捷发送。 */
function inspectPointer(event) {
  if (mode !== "select") return;
  event.preventDefault();
  event.stopImmediatePropagation();
}

/** inspectHover 在选择模式显示指向元素，离开文档后恢复选中状态。 */
function inspectHover(event) {
  if (mode !== "select") return;
  const source = authoredElement(event.target);
  hover = messageStyleTarget(event.target, source)?.element || source;
  refreshOutline();
}

/** initialize 创建独立无事件覆盖层；从文档解析完成后开始报告编辑预览就绪。 */
function initialize() {
  const host = document.createElement("div");
  host.setAttribute("aria-hidden", "true");
  host.style.cssText = "position:fixed;inset:0;z-index:2147483647;pointer-events:none;";
  const shadow = host.attachShadow({ mode: "closed" });
  const style = document.createElement("style");
  style.textContent = ":host{pointer-events:none!important}div{position:fixed;box-sizing:border-box;border:2px solid #2563eb;background:rgba(37,99,235,.06);pointer-events:none;border-radius:3px}div[hidden]{display:none!important}";
  overlay = document.createElement("div");
  overlay.hidden = true;
  shadow.append(style, overlay);
  document.documentElement.appendChild(host);
  send("goodchat:editor-ready");
}

// 先于模板脚本绑定捕获监听，选择模式不会触发模板自己的业务动作。
window.addEventListener("message", receiveCommand);
window.addEventListener("click", inspectClick, true);
window.addEventListener("pointerdown", inspectPointer, true);
window.addEventListener("pointermove", inspectHover, true);
window.addEventListener("scroll", refreshOutline, true);
window.addEventListener("resize", refreshOutline);
window.addEventListener("DOMContentLoaded", initialize, { once: true });
