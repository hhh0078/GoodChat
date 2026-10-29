/** 文件作用说明：为静态模板提供预先登记的文字咨询入口和本地弹框，业务动作仍由固定SDK执行。 */
(function () {
  "use strict";
  const chat = window.GoodChat, error = document.getElementById("gc-error");
  if (!chat) return;
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
