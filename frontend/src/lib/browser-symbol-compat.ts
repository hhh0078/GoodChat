/** 文件作用说明：在首次水合前定点安置浏览器扩展的零尺寸 SVG 定义容器，保留扩展节点且不忽略 React 校验。 */

// 独立脚本文本不引用模块闭包，生产压缩后也可由 beforeInteractive 原样执行。
export const browserSymbolCompatibilityScript = String.raw`(function () {
  "use strict";
  const stopKey = "__goodchatStopBrowserSymbolCompat";
  if (typeof window[stopKey] === "function") window[stopKey]();
  let stopped = false, bodyObserver = null, rootObserver = null;

  /** 只把抢占 body 首位的指定扩展定义容器完整移到尾部，保留所有 SVG 全局引用。 */
  function placeSymbols() {
    const body = document.body;
    const node = body && body.firstElementChild;
    if (stopped || !node || node.parentElement !== body || node.tagName !== "DIV" ||
        node.id !== "__PHOENIX_THEMED_EMPTY_SYMBOL_DEFS_ID__" || !node.nextElementSibling) return;
    const zero = /^(?:0|0px)$/;
    const rect = node.getBoundingClientRect();
    const svg = node.firstElementChild;
    if (!zero.test(node.style.width) || !zero.test(node.style.height) || rect.width !== 0 || rect.height !== 0 ||
        node.children.length !== 1 || !svg || svg.namespaceURI !== "http://www.w3.org/2000/svg" ||
        svg.tagName.toLowerCase() !== "svg" || !svg.querySelector("defs, symbol")) return;
    body.appendChild(node);
  }

  /** body 解析完成后只观察其直接插入，不持续扫描文档或业务子树。 */
  function observeBody() {
    if (stopped || bodyObserver || !document.body) return;
    if (rootObserver) rootObserver.disconnect();
    document.removeEventListener("DOMContentLoaded", observeBody);
    bodyObserver = new MutationObserver(placeSymbols);
    bodyObserver.observe(document.body, { childList: true });
    placeSymbols();
  }

  /** 应用完成首次水合后停止兼容观察，后续业务 DOM 由各自所有者管理。 */
  function stop() {
    stopped = true;
    if (bodyObserver) bodyObserver.disconnect();
    if (rootObserver) rootObserver.disconnect();
    document.removeEventListener("DOMContentLoaded", observeBody);
    if (window[stopKey] === stop) delete window[stopKey];
  }

  window[stopKey] = stop;
  if (document.body) observeBody();
  else {
    rootObserver = new MutationObserver(observeBody);
    rootObserver.observe(document.documentElement || document, { childList: true });
    document.addEventListener("DOMContentLoaded", observeBody, { once: true });
  }
})();`;

/** stopBrowserSymbolCompatibility 在客户端首次 effect 后断开短期观察，服务端调用不做处理。 */
export function stopBrowserSymbolCompatibility(): void {
  if (typeof window === "undefined") return;
  const stop = (window as Window & { __goodchatStopBrowserSymbolCompat?: () => void })
    .__goodchatStopBrowserSymbolCompat;
  if (typeof stop === "function") stop();
}
