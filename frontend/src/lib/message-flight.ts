/** 文件作用说明：将发送中的文字从输入框移动到真实消息气泡，动画与服务端确认状态相互独立。 */

/** animateMessageFlight 等待真实目标布局后执行轻量飞行动画，返回清理函数供切换会话时取消。 */
export function animateMessageFlight(
  root: HTMLElement,
  input: HTMLElement,
  clientId: string,
  content: string,
): () => void {
  if (!content || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return () => {};
  const from = input.getBoundingClientRect();
  const scope = crypto.randomUUID();
  let frame = 0,
    attempts = 0,
    cancelled = false;
  let overlay: HTMLDivElement | null = null,
    style: HTMLStyleElement | null = null;
  let animation: Animation | undefined;
  /** cleanup 无论成功结束或会话卸载都移除浮层并恢复真实消息的展示。 */
  function cleanup() {
    cancelled = true;
    cancelAnimationFrame(frame);
    animation?.cancel();
    overlay?.remove();
    style?.remove();
    if (root.dataset.flightScope === scope) delete root.dataset.flightScope;
  }
  /** locate 在当前会话中定位同一客户端消息，不用估算坐标或把网络发送成功提前展示。 */
  function locate() {
    if (cancelled || !root.isConnected) return;
    const target = root.querySelector<HTMLElement>(
      `[data-client-message-id="${CSS.escape(clientId)}"] .message-bubble`,
    );
    if (!target || !target.getClientRects().length) {
      if (++attempts < 30) frame = requestAnimationFrame(locate);
      return;
    }
    const scroller = root.querySelector<HTMLElement>(".chat-messages");
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
    const to = target.getBoundingClientRect(),
      appearance = getComputedStyle(target);
    root.dataset.flightScope = scope;
    style = document.createElement("style");
    style.textContent = `[data-flight-scope="${scope}"] [data-client-message-id="${CSS.escape(clientId)}"] .message-bubble{opacity:0}`;
    document.head.append(style);
    overlay = document.createElement("div");
    overlay.className = "message-send-flight";
    overlay.setAttribute("aria-hidden", "true");
    overlay.textContent = content;
    Object.assign(overlay.style, {
      left: `${to.left}px`,
      top: `${to.top}px`,
      width: `${to.width}px`,
      maxHeight: `${Math.min(to.height, 160)}px`,
      background: appearance.backgroundColor,
      color: appearance.color,
      borderRadius: appearance.borderRadius,
      font: appearance.font,
      padding: appearance.padding,
    });
    document.body.append(overlay);
    animation = overlay.animate(
      [
        {
          transform: `translate(${from.left + 12 - to.left}px, ${from.top + 8 - to.top}px) scale(.96)`,
          opacity: 0.95,
        },
        { transform: "translate(0, 0) scale(1)", opacity: 1 },
      ],
      { duration: 260, easing: "cubic-bezier(.23,1,.32,1)", fill: "forwards" },
    );
    animation.finished.then(cleanup, cleanup);
  }
  frame = requestAnimationFrame(locate);
  return cleanup;
}
