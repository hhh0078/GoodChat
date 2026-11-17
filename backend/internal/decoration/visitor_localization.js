/** 文件作用说明：兼容已发布的固定聊天脚本，仅翻译受控系统控件与时间，不处理客户正文、姓名和商户品牌。 */
(function () {
  "use strict";
  const configuration = window.GoodChatI18n;
  if (!configuration) return;
  const messages = configuration.messages;
  const patterns = (configuration.patterns || []).map((item) => ({
    ...item,
    matcher: new RegExp("^" + item.source.split(/(\{p\d+\})/).map((part) => /^\{p\d+\}$/.test(part) ? "(.+?)" : part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("") + "$"),
    names: [...item.source.matchAll(/\{(p\d+)\}/g)].map((match) => match[1]),
  }));

  /** translate 只对明确的系统文案查表，保留插值参数原文。 */
  function translate(source, params = {}) {
    let hash = 2166136261;
    for (const byte of new TextEncoder().encode(source)) hash = Math.imul(hash ^ byte, 16777619) >>> 0;
    const key = "m" + hash.toString(16).padStart(8, "0");
    let text = Object.hasOwn(messages, key) ? messages[key] : source;
    if (text === source && source.length < 2000) {
      for (const pattern of patterns) {
        const matched = source.match(pattern.matcher);
        if (!matched) continue;
        text = pattern.text;
        params = Object.fromEntries(pattern.names.map((name, index) => [name, matched[index + 1]]));
        break;
      }
    }
    return text.replace(/\{(p\d+)\}/g, (match, name) => String(params[name] ?? match));
  }
  window.gcTranslate = translate;
  let scheduled = false;
  const clock = new Intl.DateTimeFormat(configuration.locale, { timeZone: configuration.timezone, hour: "2-digit", minute: "2-digit" });
  const fullClock = new Intl.DateTimeFormat(configuration.locale, { timeZone: configuration.timezone, dateStyle: "medium", timeStyle: "medium" });

  /** localize 翻译平台控件的直接文字，绝不进入消息正文、引用或参与者姓名。 */
  function localize() {
    scheduled = false;
    const controls = document.querySelectorAll("#gc-status,#gc-reply-cancel,#gc-add,#gc-send,#gc-text,#gc-file,#gc-attachment-menu button,.gc-history-control button,.gc-read-status");
    for (const control of controls) {
      for (const node of control.tagName === "TEXTAREA" ? [] : control.childNodes) {
        if (node.nodeType !== Node.TEXT_NODE) continue;
        const translated = translate(node.textContent || "");
        if (translated !== node.textContent) node.textContent = translated;
      }
      for (const attribute of ["placeholder", "aria-label", "title"]) {
        const original = control.getAttribute(attribute);
        if (original !== null) { const translated = translate(original); if (translated !== original) control.setAttribute(attribute, translated); }
      }
    }
    for (const element of document.querySelectorAll("#gc-messages .gc-message-meta time[datetime]")) {
      const date = new Date(element.dateTime);
      if (!Number.isFinite(date.getTime())) continue;
      const text = clock.format(date), title = fullClock.format(date);
      if (element.textContent !== text) element.textContent = text;
      if (element.title !== title) element.title = title;
    }
  }

  /** schedule 合并旧脚本状态更新引起的 DOM 变化，避免重复扫描聊天界面。 */
  function schedule() { if (!scheduled) { scheduled = true; requestAnimationFrame(localize); } }
  const observer = new MutationObserver(schedule);
  observer.observe(document.documentElement, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ["datetime", "placeholder", "aria-label", "title"] });
  document.addEventListener("DOMContentLoaded", schedule, { once: true });
  window.addEventListener("pagehide", () => observer.disconnect(), { once: true });
  schedule();
})();
