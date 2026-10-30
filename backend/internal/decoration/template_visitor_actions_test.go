// 文件作用说明：验证模板留言与评价只能走固定SDK、空参数桥接、纯本地预览及历史内置脚本兼容。
package decoration

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestTemplateVisitorActionDeclarations 拒绝任意动作和混合声明，允许模板用固定动作直接绑定留言与评价。
func TestTemplateVisitorActionDeclarations(t *testing.T) {
	for _, action := range []string{"leave-message", "evaluation"} {
		_, files, assets := templateWithActionButton(t, `<button type="button" data-gc-action="`+action+`">固定访客动作</button>`)
		if _, err := buildTemplatePackageAssets("index.html", files, defaultTemplateScripts(), assets); err != nil {
			t.Fatalf("固定访客动作%s不能打包：%v", action, err)
		}
	}
	legacy, err := industryTemplatePackage("finance", defaultTemplateScripts())
	if err != nil {
		t.Fatal(err)
	}
	legacyFiles := templateFileMap(legacy)
	oldSDK := legacyFiles[templateSDKPath]
	oldSDK.Content = []byte(strings.ReplaceAll(string(oldSDK.Content), "openLeaveMessage", "oldUnsupportedAction"))
	legacyFiles[templateSDKPath] = oldSDK
	if err := validateNewTemplateActions(legacyFiles["index.html"].Content, legacyFiles); err == nil {
		t.Fatal("不能把留言按钮绑定到不支持该方法的旧SDK")
	}
	for _, button := range []string{`<button type="button" data-gc-action="">空动作</button>`, `<button type="button" data-gc-action="submit-payment">越权动作</button>`, `<button type="button" data-gc-action="leave-message" data-gc-send="伪装发送">混合动作</button>`} {
		_, files, assets := templateWithActionButton(t, button)
		if _, err := buildTemplatePackageAssets("index.html", files, defaultTemplateScripts(), assets); err == nil {
			t.Fatal("非法动作或双重动作不能被新模板接受")
		}
	}
}

// TestSDKVisitorActionsUseEmptyPayload 执行合法握手后只发送两个已登记空参数RPC，调用方不能注入身份或自动提交数据。
func TestSDKVisitorActionsUseEmptyPayload(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("执行SDK回归需要本机Node运行时")
	}
	source, _ := json.Marshal(sdkScript("https://parent.example", "visitor-actions", false))
	harness := `import assert from 'node:assert/strict';import {runInNewContext} from 'node:vm';
const events=new Map(),calls=[],parent={postMessage(){}},window={addEventListener(n,f){events.set(n,f)},removeEventListener(n){events.delete(n)}};
const port={start(){},close(){},postMessage(call){calls.push(call);queueMicrotask(()=>this.onmessage({data:{id:call.id,result:{opened:true}}}));}};
runInNewContext(` + string(source) + `,{window,parent,Blob,crypto:{randomUUID:()=> 'visitor-actions-nonce'},setInterval:()=>1,clearInterval(){},setTimeout,clearTimeout});
events.get('message')({source:parent,origin:'https://parent.example',ports:[port],data:{type:'goodchat:port',version:1,nonce:'visitor-actions-nonce',release_id:'visitor-actions'}});
await window.GoodChat.openLeaveMessage({token:'must-not-forward',entry_id:'foreign',message:'not-a-submission'});
await window.GoodChat.openEvaluation({score:5,conversation_id:'foreign'});
assert.deepEqual(calls.map(c=>c.method),['openLeaveMessage','openEvaluation']);
for(const call of calls)assert.equal(Object.keys(call.payload).length,0);
assert.equal(Object.isFrozen(window.GoodChat),true);`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("访客动作SDK空参数边界失败：%v\n%s", err, output)
	}
}

// TestPreviewVisitorActionsAreLocalDialogs 执行实际预览SDK，确认留言与评价仅打开可关闭说明框且不联网、不追加真实消息。
func TestPreviewVisitorActionsAreLocalDialogs(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("执行SDK预览回归需要本机Node运行时")
	}
	scripts := defaultTemplateScripts()
	source, _ := json.Marshal(templateBootScript("https://parent.example", "visitor-actions", true) + scripts.SDK)
	harness := `import assert from 'node:assert/strict';import {runInNewContext} from 'node:vm';
const nodes=[];
/** PreviewNode 仅模拟原生对话框生命周期，不模拟业务接口。 */
class PreviewNode {
 /** constructor 建立仅供预览生命周期断言的DOM节点替身。 */
 constructor(tag){this.tag=tag;this.dataset={};this.handlers={};this.children=[];this.open=false;this.removed=false;nodes.push(this)}
 /** append 保留说明与关闭按钮的顺序。 */
 append(...children){this.children.push(...children)}
 /** addEventListener 记录本地界面事件，不创建网络回调。 */
 addEventListener(name,handler){this.handlers[name]=handler}
 /** showModal 模拟对话框开启状态。 */
 showModal(){this.open=true}
 /** close 模拟原生关闭事件。 */
 close(){this.open=false;this.handlers.close?.()}
 /** remove 标记节点被页面清理。 */
 remove(){this.removed=true}
 /** setAttribute 模拟无showModal环境的open属性。 */
 setAttribute(name){if(name==='open')this.open=true}
}
const body=new PreviewNode('body'),document={body,createElement:tag=>new PreviewNode(tag),getElementById:id=>nodes.find(n=>n.id===id&&!n.removed)},window={};
const forbidden=()=>{throw new Error('预览动作不能请求网络或父层')};
runInNewContext(` + string(source) + `,{window,document,Blob,Uint8Array,atob,crypto:{randomUUID:()=>crypto.randomUUID()},parent:{postMessage:forbidden},fetch:forbidden,WebSocket:forbidden,setInterval:forbidden});
for(const [method,id,title] of [['openLeaveMessage','gc-preview-leave-message','留言功能预览'],['openEvaluation','gc-preview-evaluation','会话评价预览']]){
 const result=await window.GoodChat[method]();assert.equal(result.preview,true);assert.equal(result.opened,true);
 const dialog=document.getElementById(id);assert.ok(dialog.open);assert.equal(dialog.children[0].textContent,title);
 await window.GoodChat[method]();assert.equal(nodes.filter(n=>n.id===id&&!n.removed).length,1);
 dialog.children.find(n=>n.tag==='button').handlers.click();assert.ok(dialog.removed);
}
assert.equal((await window.GoodChat.getState()).messages.length,3);`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("SDK本地访客动作预览失败：%v\n%s", err, output)
	}
}

// TestBuiltinTemplate101RemainsTrusted 仅接受发布时冻结的内置摘要，升级后仍可校验原包但不能篡改摘要冒充历史版本。
func TestBuiltinTemplate101RemainsTrusted(t *testing.T) {
	legacy := templateContract{SchemaVersion: 1, SDKVersion: "1.0.1", Files: map[string]string{
		templateSDKPath: "8532ecb1fe008727b471c9f6448c142ae0559de9fc57bc25ad99e1825f41b7cc",
		templateUIPath:  "546ccdf023366b9ecdfe9aaccbcfd0db73ce5577cf50e0fa0a223d9e5ab7235c",
	}}
	if err := verifyRegisteredTemplateContract(context.Background(), nil, legacy); err != nil {
		t.Fatal("内置历史摘要无需数据库也应继续有效", err)
	}
	legacy.Files[templateSDKPath] = digest([]byte("forged legacy"))
	if builtinTemplateContract(legacy) {
		t.Fatal("修改历史SDK后不得冒充内置旧版本")
	}
}

// TestFixedVisitorActionButtonsFollowStatus 执行实际动作脚本，评价仅在已结束状态显示，两个入口都只调用核心方法。
func TestFixedVisitorActionButtonsFollowStatus(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("执行固定按钮回归需要本机Node运行时")
	}
	source, _ := json.Marshal(templateActionScript)
	harness := `import assert from 'node:assert/strict';import {runInNewContext} from 'node:vm';
let notify,leaves=0,evaluations=0;
/** actionButton 记录真实固定脚本给声明式按钮绑定的处理器。 */
function actionButton(action){return {hidden:false,disabled:false,handlers:{},getAttribute:()=>action,addEventListener(name,handler){this.handlers[name]=handler}}}
const leave=actionButton('leave-message'),evaluation=actionButton('evaluation'),unknown=actionButton('arbitrary-action'),error={textContent:''};
const chat={ready:()=>Promise.resolve(),subscribe(handler){notify=handler;handler({conversation:{status:'active'}})},async openLeaveMessage(){leaves++},async openEvaluation(){evaluations++}};
const document={getElementById:()=>error,querySelectorAll:selector=>selector==='[data-gc-action]'?[leave,evaluation,unknown]:[]};
runInNewContext(` + string(source) + `,{window:{GoodChat:chat},document,fetch(){throw new Error('不能直连业务接口')}});
assert.equal(evaluation.hidden,true);assert.equal(leave.hidden,false);assert.equal(unknown.handlers.click,undefined);
await leave.handlers.click();assert.equal(leaves,1);assert.equal(leave.disabled,false);
notify({conversation:{status:'closed'}});assert.equal(evaluation.hidden,false);
await evaluation.handlers.click();assert.equal(evaluations,1);assert.equal(evaluation.disabled,false);
notify({conversation:{status:'active'}});assert.equal(evaluation.hidden,true);assert.equal(error.textContent,'');`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("固定访客动作按钮执行失败：%v\n%s", err, output)
	}
}

// TestFoundationRequiresVisitorActions 新保存的平台固定脚本必须同时提供留言、评价与声明式动作，读取历史合同不走本检查。
func TestFoundationRequiresVisitorActions(t *testing.T) {
	for _, marker := range []string{"openLeaveMessage", "openEvaluation"} {
		broken := defaultTemplateScripts()
		broken.SDK = strings.ReplaceAll(broken.SDK, marker, "removedVisitorAction")
		if err := validateTemplateScripts(broken); err == nil {
			t.Fatal("新SDK不能缺少核心访客动作", marker)
		}
		broken = defaultTemplateScripts()
		broken.ChatUI = strings.ReplaceAll(broken.ChatUI, marker, "removedVisitorAction")
		if err := validateTemplateScripts(broken); err == nil {
			t.Fatal("新聊天脚本不能缺少固定访客动作", marker)
		}
	}
}
