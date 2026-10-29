/** 文件作用说明：为静态模板提供预先登记的文字咨询、留言、评价入口和本地弹框，业务动作仍由固定SDK执行。 */
(function () {
  "use strict";
  const chat = window.GoodChat, error = document.getElementById("gc-error");
  if (!chat) return;
  for (const button of document.querySelectorAll("[data-gc-action]")) {
    const action = button.getAttribute("data-gc-action");
    if (action !== "leave-message" && action !== "evaluation") continue;
    if (action === "evaluation") {
      button.hidden = true;
      chat.subscribe(state => { button.hidden = state?.conversation?.status !== "closed"; });
    }
    /** 只通过官方核心方法打开当前咨询的留言或评价界面，模板不能自行拼装业务请求。 */
    async function openVisitorAction() {
      if (button.disabled) return;
      button.disabled = true;
      try { await chat.ready(); if (action === "evaluation") await chat.openEvaluation(); else await chat.openLeaveMessage(); }
      catch (problem) { if (error) error.textContent = problem?.message || "操作界面暂时无法打开，请稍后重试"; }
      finally { button.disabled = false; }
    }
    button.addEventListener("click", openVisitorAction);
  }
  for (const button of document.querySelectorAll("[data-gc-send]")) {
    /** 把服务入口转换为普通咨询文字，不允许模板指定接口、身份或任意业务动作。 */
    async function sendQuestion() {
      const text = button.getAttribute("data-gc-send") || "";
      if (button.disabled || !text.trim() || text.length > 10000) return;
      button.disabled = true;
      try { await chat.ready(); await chat.sendText(text, crypto.randomUUID()); }
      catch (problem) { if (error) error.textContent = problem?.message || "咨询暂时无法发送，请稍后重试"; }
      finally { button.disabled = false; }
    }
    button.addEventListener("click", sendQuestion);
  }
})();
