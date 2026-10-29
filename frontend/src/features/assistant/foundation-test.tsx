/** 文件作用说明：在无凭据、禁网络的独立沙箱执行候选官方脚本，检查预览聊天核心方法及控件动作。 */
"use client";
import { useEffect, useRef, useState } from "react";
import { Alert, Button } from "antd";

const controls = `<header><strong>官方聊天脚本 · 模拟检查</strong><p>此页面只使用内存消息。</p><button type="button" data-gc-action="leave-message">留言按钮检查</button></header><div id="gc-status" role="status"></div><div id="gc-messages" role="log"></div><p id="gc-typing"></p><div id="gc-reply-bar" hidden><span id="gc-reply-text"></span><button id="gc-reply-cancel" type="button">取消引用</button></div><div id="gc-compose"><button id="gc-add" type="button">＋</button><textarea id="gc-text" aria-label="模拟消息"></textarea><button id="gc-send" type="button">发送</button><div id="gc-attachment-menu" hidden><button data-kind="image">图片</button><button data-kind="file">文件</button><button data-kind="video">视频</button></div><input id="gc-file" type="file" hidden></div><p id="gc-error" role="alert"></p>`;
const previewCSS = `*{box-sizing:border-box}body{padding:16px;margin:0;font:14px/1.7 system-ui;color:#20314b;background:#f5f8fd}header p{margin:0 0 12px;color:#6a7890;font-size:12px}#gc-messages{height:240px;overflow:auto}.gc-message-row{margin:12px 0}.gc-message{padding:10px;border-radius:8px;background:white}.gc-own{text-align:right}.gc-own .gc-message{background:#e4edff}.gc-avatar{width:26px;height:26px;border-radius:50%;vertical-align:middle;margin-right:6px}.gc-message img{max-width:200px;max-height:140px}.gc-message-meta{font-size:11px;color:#6a7890}.gc-message-options{display:flex;gap:8px}button{padding:6px 10px;border:1px solid #cbd9ec;border-radius:6px;background:white;color:#1e52a5;cursor:pointer}textarea{width:65%;padding:8px;font:inherit;border:1px solid #cbd9ec;border-radius:6px;vertical-align:middle}#gc-compose{position:relative;display:flex;align-items:center;gap:8px;margin-top:12px}#gc-attachment-menu{position:absolute;bottom:100%;background:white;padding:8px}[hidden]{display:none!important}#gc-error{color:#a0332d}`;
const smoke = `
(async function(){
  const results=[];
  /** 检查当前断言并记录真实通过的功能。 */
  function check(value,label){if(!value)throw new Error(label+'未通过');results.push(label);}
  /** 等待异步界面动作完成，不改变真实服务端状态。 */
  function pause(){return new Promise(resolve=>setTimeout(resolve,180));}
  /** 仅向创建此沙箱的父页面报告本次检查结果。 */
  function report(ok,message){parent.postMessage({type:'goodchat:foundation-test',nonce:window.__FoundationTestNonce,ok,message,checks:results},'*');}
  try {
    const api=window.GoodChat;
    check(!!api,'SDK加载');
    for(const name of ['ready','getState','sendText','subscribe','upload','download','loadOlder','typing','read','withdrawMessage','openLeaveMessage','openEvaluation'])check(typeof api[name]==='function',name+'接口');
    await api.ready();
    await api.openLeaveMessage();
    let dialog=document.getElementById('gc-preview-leave-message');
    check(!!dialog?.open,'留言核心弹框');dialog.querySelector('button').click();
    check(!document.getElementById('gc-preview-leave-message'),'留言弹框关闭');
    document.querySelector('[data-gc-action="leave-message"]').click();await pause();
    dialog=document.getElementById('gc-preview-leave-message');check(!!dialog?.open,'模板留言按钮');dialog.querySelector('button').click();
    await api.openEvaluation();dialog=document.getElementById('gc-preview-evaluation');
    check(!!dialog?.open,'评价核心弹框');dialog.querySelector('button').click();
    let before=await api.getState();
    check(before.preview===true&&Array.isArray(before.messages)&&before.messages.length>0,'模拟初始化');
    check(Object.isFrozen(api),'核心接口冻结');
    let notices=0;const unsubscribe=api.subscribe(()=>notices++);
    const id=crypto.randomUUID();
    await api.sendText('SDK消息检查',id);await api.sendText('SDK消息检查',id);
    let next=await api.getState();
    check(next.messages.filter(item=>item.client_message_id===id).length===1,'幂等发送');
    check(notices>=2,'消息订阅');
    const input=document.getElementById('gc-text'),send=document.getElementById('gc-send');
    await pause();input.value='发送按钮检查';input.dispatchEvent(new Event('input',{bubbles:true}));send.click();await pause();
    next=await api.getState();check(next.messages.some(item=>item.content==='发送按钮检查'),'发送按钮');
    check(document.getElementById('gc-messages').textContent.includes('发送按钮检查'),'消息渲染');
    const option=Array.from(document.querySelectorAll('#gc-messages button')).find(button=>button.textContent==='了解服务');
    check(!!option,'快捷选项控件');option.click();await pause();
    check((await api.getState()).messages.some(item=>item.content==='我想了解服务'),'快捷选项发送');
    await api.upload(new Blob(['attachment-proof'],{type:'text/plain'}),'模拟附件.txt');
    next=await api.getState();const attachment=next.messages.at(-1).attachment;
    check(!!attachment&&attachment.file_name==='模拟附件.txt','附件发送');
    const file=await api.download(attachment.id);check(file.file instanceof Blob&&await file.file.text()==='attachment-proof','附件读取');
    await api.loadOlder();check((await api.getState()).has_older===false,'历史消息');
    const own=(await api.getState()).messages.find(item=>item.client_message_id===id);
    await api.withdrawMessage(own.id);check(!!(await api.getState()).messages.find(item=>item.id===own.id).withdrawn_at,'本人撤回');
    await api.typing(false);await api.read('1');unsubscribe();
    report(true,'模拟初始化、订阅、发送、控件、选项、附件、历史、撤回及留言评价检查通过');
  }catch(problem){report(false,problem?.message||'脚本执行失败');}
})();`;

/** foundationTestDocument 将候选脚本放进不具备同源身份的文档，CSP 禁止网络与表单。 */
export function foundationTestDocument(sdk: string, chatUI: string, nonce: string): string {
  if ([sdk, chatUI].some((value) => /<\/script/i.test(value)))
    throw new Error("候选脚本不能包含结束 script 标签");
  const boot = `Object.defineProperty(window,'__GoodChatBoot',{value:Object.freeze({preview:true,origin:'',release:'foundation-check'}),writable:false,configurable:false});window.__FoundationTestNonce=${JSON.stringify(nonce)};`;
  return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none';script-src 'unsafe-inline';style-src 'unsafe-inline';img-src data: blob:;media-src blob:;connect-src 'none';form-action 'none';base-uri 'none';object-src 'none';frame-src 'none';worker-src 'none'"><style>${previewCSS}</style></head><body>${controls}<script>${boot}</script><script>${sdk}</script><script>${chatUI}</script><script>${smoke}</script></body></html>`;
}

/** FoundationTest 验证当前候选内容，字段再次修改时上层会撤销保存资格。 */
export function FoundationTest({
  sdk,
  chatUI,
  onVerified,
}: {
  sdk: string;
  chatUI: string;
  onVerified: (scripts: { sdk: string; chat_ui: string }) => void;
}) {
  const [run, setRun] = useState<{
    nonce: string;
    document: string;
    sdk: string;
    chatUI: string;
  } | null>(null);
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null);
  const [running, setRunning] = useState(false);
  const frame = useRef<HTMLIFrameElement | null>(null);
  const stale = !!run && (run.sdk !== sdk || run.chatUI !== chatUI);
  useEffect(() => {
    if (!run) return;
    let finished = false;
    /** receive 只接受本次隔离框架及随机编号的检查结果，其他窗口消息无效。 */
    function receive(event: MessageEvent) {
      const value = event.data;
      if (
        finished ||
        event.source !== frame.current?.contentWindow ||
        value?.type !== "goodchat:foundation-test" ||
        value.nonce !== run!.nonce
      )
        return;
      finished = true;
      clearTimeout(timer);
      setRunning(false);
      setResult({
        ok: value.ok === true,
        message: String(value.message || "检查没有返回结果").slice(0, 500),
      });
      if (value.ok === true) onVerified({ sdk: run!.sdk, chat_ui: run!.chatUI });
    }
    const timer = setTimeout(() => {
      finished = true;
      setRunning(false);
      setResult({ ok: false, message: "模拟检查超时，请检查 SDK 初始化及脚本错误。" });
    }, 10000);
    window.addEventListener("message", receive);
    return () => {
      finished = true;
      clearTimeout(timer);
      window.removeEventListener("message", receive);
    };
  }, [run, onVerified]);

  /** start 为当前脚本创建新的隔离检查，不向服务器或模型发送任何资料。 */
  function start() {
    setResult(null);
    try {
      const nonce = crypto.randomUUID();
      setRun({ nonce, document: foundationTestDocument(sdk, chatUI, nonce), sdk, chatUI });
      setRunning(true);
    } catch (problem) {
      setResult({
        ok: false,
        message: problem instanceof Error ? problem.message : "无法创建检查页面",
      });
    }
  }

  return (
    <div className="assistant-foundation-test">
      <Button onClick={start} loading={running} disabled={!sdk || !chatUI}>
        运行候选脚本模拟检查
      </Button>
      <p className="form-hint">
        检查当前编辑内容的模拟收发与控件，实际 WebSocket 和真实访客授权仍需发布验收。
      </p>
      {result && (
        <Alert
          type={stale ? "warning" : result.ok ? "success" : "error"}
          showIcon
          title={stale ? "脚本已修改，请重新运行模拟检查。" : result.message}
        />
      )}
      {run && (
        <iframe
          ref={frame}
          key={run.nonce}
          title="官方脚本候选模拟检查"
          srcDoc={run.document}
          sandbox="allow-scripts"
          referrerPolicy="no-referrer"
          style={{
            width: "100%",
            height: 390,
            border: "1px solid #dce5f1",
            borderRadius: 10,
            marginTop: 16,
          }}
        />
      )}
    </div>
  );
}
