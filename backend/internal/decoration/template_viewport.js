/** 文件作用说明：为固定访客页面同步iframe内实际可用高度及软键盘状态，保持输入区可见且消息区独立滚动。 */
(function () {
  "use strict";
  const root = document.documentElement, body = document.body;
  const input = document.getElementById("gc-text");
  if (!body || !input) return;
  let expandedHeight = Math.max(1, window.innerHeight);

  /** 按当前视觉视口同步高度，输入时缩小视口只压缩装饰区，不裁掉输入控件。 */
  function updateViewport() {
    const visualHeight = window.visualViewport?.height || window.innerHeight;
    const height = Math.max(1, Math.round(Math.min(window.innerHeight, visualHeight)));
    const focused = document.activeElement === input;
    if (!focused || height > expandedHeight) expandedHeight = Math.max(height, window.innerHeight);
    root.style.setProperty("--gc-viewport-height", height + "px");
    body.classList.toggle("gc-input-focused", focused);
    body.classList.toggle("gc-keyboard-open", focused && expandedHeight - height > 100);
  }

  /** 焦点离开后等待浏览器更新活动元素，再恢复正常页面装饰。 */
  function updateAfterFocus() { requestAnimationFrame(updateViewport); }
  window.addEventListener("resize", updateViewport);
  window.visualViewport?.addEventListener("resize", updateViewport);
  window.visualViewport?.addEventListener("scroll", updateViewport);
  document.addEventListener("focusin", updateViewport);
  document.addEventListener("focusout", updateAfterFocus);
  document.addEventListener("visibilitychange", updateViewport);
  updateViewport();
})();
