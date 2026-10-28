/** 文件作用说明：在装修沙箱本地把发送文字移动到真实消息节点，不调用桥接能力或访问父窗口。 */
const runningSendMotions = new WeakMap();
/** animateSend 只使用当前文档的输入框、消息区和稳定消息编号执行本地动画，after可等待真实发送确认。 */
function animateSend({ input, messages, client_message_id, text, after }) {
  if (!input?.getBoundingClientRect || !messages?.querySelector || !text || typeof requestAnimationFrame !== "function" || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
  if (!/^[\da-f-]{36}$/i.test(client_message_id || "")) return;
  runningSendMotions.get(messages)?.();
  const from = input.getBoundingClientRect(), scope = crypto.randomUUID();
  let attempts = 0, overlay = null, style = null, cancelled = false;
  /** finish 移除动画副本并恢复当前同编号消息，不改变消息保存或已读状态。 */
  function finish() { cancelled=true; overlay?.remove(); style?.remove(); if (messages.dataset.sendScope === scope) delete messages.dataset.sendScope; if(runningSendMotions.get(messages)===finish)runningSendMotions.delete(messages); }
  runningSendMotions.set(messages,finish);
  /** locate 等实际消息DOM出现后测量，布局之外的估算坐标不参与飞行。 */
  function locate() {
    if (cancelled || !messages.isConnected) return;
    const row = messages.querySelector(`[data-client-message-id="${client_message_id}"]`);
    if (!row) { if (++attempts < 60) requestAnimationFrame(locate); return; }
    messages.scrollTop = messages.scrollHeight;
    const target = row.querySelector(".gc-message") || row;
    const to = target.getBoundingClientRect(), appearance = getComputedStyle(target);
    messages.dataset.sendScope = scope;
    style = document.createElement("style"); style.textContent = `[data-send-scope="${scope}"] [data-client-message-id="${client_message_id}"]{opacity:0}`; document.head.append(style);
    overlay = document.createElement("div"); overlay.textContent = text; overlay.setAttribute("aria-hidden", "true");
    Object.assign(overlay.style, { position:"fixed",zIndex:"1000",pointerEvents:"none",left:`${to.left}px`,top:`${to.top}px`,width:`${to.width}px`,maxHeight:`${Math.min(to.height,160)}px`,overflow:"hidden",boxSizing:"border-box",whiteSpace:"pre-wrap",overflowWrap:"anywhere",borderRadius:appearance.borderRadius,padding:appearance.padding,background:appearance.backgroundColor,color:appearance.color,font:appearance.font });
    document.body.append(overlay);
    const motion = overlay.animate([{transform:`translate(${from.left+12-to.left}px,${from.top+8-to.top}px) scale(.96)`,opacity:.95},{transform:"translate(0,0) scale(1)",opacity:1}],{duration:260,easing:"cubic-bezier(.23,1,.32,1)",fill:"forwards"});
    motion.finished.then(finish,finish);
  }
  Promise.resolve(after).then(()=>requestAnimationFrame(locate),finish);
}
