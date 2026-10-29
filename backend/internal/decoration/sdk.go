// 文件作用说明：提供精确来源、一次性握手和专用消息通道的装修SDK及受控结构化聊天界面。
package decoration

import (
	_ "embed"
	"encoding/json"
)

// sendMotionScript 仅在当前装修文档执行发送动效，预览也不需要平台身份或网络能力。
//
//go:embed send_motion.js
var sendMotionScript string

// sdkScript 生成只面向当前父来源与静态版本的聊天桥接SDK，不接触任何平台令牌。
// sendText接受可选消息幂等编号和引用编号，withdrawMessage仅接受当前消息编号，最终由可信父层与后端鉴权。
func sdkScript(parentOrigin, releaseID string, preview bool) string {
	config, _ := json.Marshal(map[string]any{"origin": parentOrigin, "release": releaseID, "preview": preview})
	return `(function(){'use strict';` + sendMotionScript + `const cfg=` + string(config) + `;let port=null,readyResolve,readyReject,serial=0,attempts=0;const pending=new Map(),listeners=new Set();const nonce=crypto.randomUUID();const ready=new Promise((resolve,reject)=>{readyResolve=resolve;readyReject=reject});ready.catch(()=>{});const fail=message=>Object.assign(new Error(message),{code:'bridge_unavailable'});/** 向精确父来源重试一次性握手。 */function init(){if(cfg.preview){clearInterval(timer);readyReject(fail('布局预览不连接真实会话'));return}if(parent===window){clearInterval(timer);readyReject(fail('请从授权咨询入口打开页面'));return}parent.postMessage({type:'goodchat:init',version:1,nonce,release_id:cfg.release},cfg.origin);if(++attempts>=40&&!port){clearInterval(timer);readyReject(fail('咨询连接超时，请刷新入口重试'))}}/** 验证父窗口及一次性随机数并接收专用通道。 */function handshake(event){const d=event.data;if(port||event.source!==parent||event.origin!==cfg.origin||!d||d.type!=='goodchat:port'||d.version!==1||d.nonce!==nonce||d.release_id!==cfg.release||event.ports.length!==1)return;port=event.ports[0];clearInterval(timer);window.removeEventListener('message',handshake);port.onmessage=event=>{const d=event.data;if(!d||typeof d!=='object')return;if(d.type==='state'){for(const fn of listeners){try{fn(d.data)}catch{}}return}const call=pending.get(d.id);if(!call)return;pending.delete(d.id);clearTimeout(call.timeout);if(d.error)call.reject(Object.assign(new Error(String(d.error.message||'操作未完成')),{code:d.error.code}));else call.resolve(d.result)};port.start();readyResolve()}async /** 发送受限聊天请求并跟踪结果未知状态。 */function request(method,payload){await ready;const id=nonce+':'+(++serial);return new Promise((resolve,reject)=>{const timeout=setTimeout(()=>{pending.delete(id);reject(Object.assign(new Error('操作结果暂未确认，请刷新查看后再试'),{code:'result_unknown'}))},30000);pending.set(id,{resolve,reject,timeout});port.postMessage({id,method,payload})})}/** upload 在子来源先校验并读取磁盘文件，再只把内存Blob经专用通道交给可信父层；原始MIME供父层区分webm音视频。 */async function upload(file,name){if(!(file instanceof Blob)||file.size===0||file.size>20*1024*1024)throw new TypeError('请选择不超过20MB的有效文件');const filename=name||file.name||'附件';if(typeof filename!=='string'||!filename.trim()||filename.length>240)throw new TypeError('附件名称不能为空或超过240个字符');const bytes=await file.arrayBuffer();if(bytes.byteLength!==file.size)throw new Error('附件内容已变化，请重新选择文件');const memory=new Blob([bytes],{type:file.type});return request('upload',{file:memory,name:filename})}const api={animateSend,ready:()=>ready,request,getState:()=>request('getState',{}),/** loadOlder 无参数加载当前授权会话的上一页，游标仅由可信父层持有。 */loadOlder:()=>request('loadOlder',{}),/** openLeaveMessage 仅请求可信父层打开当前咨询的留言表单，不接收身份或业务参数。 */openLeaveMessage:()=>request('openLeaveMessage',{}),/** openEvaluation 仅打开可信父层对已结束会话提供的评价界面，不自动提交评价。 */openEvaluation:()=>request('openEvaluation',{}),getAvatar:message_id=>request('getAvatar',{message_id}),sendText:(text,client_message_id,reply_to_id)=>request('sendText',{text,client_message_id:client_message_id||crypto.randomUUID(),...(reply_to_id?{reply_to_id}:{})}),withdrawMessage:message_id=>request('withdrawMessage',{message_id}),typing:active=>request('typing',{active:!!active}),upload,download:attachment_id=>request('download',{attachment_id}),saveAttachment:attachment_id=>request('saveAttachment',{attachment_id}),read:last_read_seq=>request('read',{last_read_seq:String(last_read_seq)}),subscribe:handler=>{if(typeof handler!=='function')throw new TypeError('订阅函数不正确');listeners.add(handler);api.getState().then(state=>handler(state)).catch(()=>{});return()=>listeners.delete(handler)}};Object.defineProperty(window,'GoodChat',{value:Object.freeze(api),writable:false,configurable:false});window.addEventListener('message',handshake);let timer=setInterval(init,250);init();window.addEventListener('pagehide',()=>{clearInterval(timer);if(port)port.close();for(const call of pending.values()){clearTimeout(call.timeout);call.reject(fail('页面连接已结束'))}pending.clear();listeners.clear()},{once:true})})();`
}

// schemaChatScript 使用有限SDK渲染可辨识聊天，附件凭证留在可信父层。
//
//go:embed schema_chat.js
var schemaChatScript string
