// 文件作用说明：用不同本地来源验证真实SDK拒绝错误握手，并通过专用MessageChannel安全返回头像PNG Blob。
package decoration

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestBrowserSDKProtocol 显式开启时检查真实SDK握手和头像Blob克隆，普通Go测试不会启动或控制浏览器。
func TestBrowserSDKProtocol(t *testing.T) {
	if os.Getenv("GOODCHAT_BROWSER_SDK_TEST") != "1" {
		t.Skip("需要显式开启并由浏览器打开测试地址")
	}
	const release = "20000000-0000-4000-8000-000000000001"
	const nonce = "browser-sdk-nonce-123456"
	const messageID = "30000000-0000-4000-8000-000000000001"
	var avatar bytes.Buffer
	if err := png.Encode(&avatar, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	var parentURL string
	done := make(chan bool, 1)
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setRenderPolicy(w, nonce, parentURL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>SDK沙箱</title><script nonce="%s">%s</script><script nonce="%s">window.GoodChat.getState().then(async state=>{const result=await window.GoodChat.getAvatar(%q);const file=result.file;const bytes=file instanceof Blob?new Uint8Array(await file.arrayBuffer()):new Uint8Array();const valid=file instanceof Blob&&file.type==='image/png'&&file.size===%d&&[137,80,78,71,13,10,26,10].every((value,index)=>bytes[index]===value);return window.GoodChat.request('browserProof',{marker:state.marker,avatar_valid:valid,avatar_type:file?.type,avatar_size:file?.size})}).catch(()=>window.GoodChat.request('browserProof',{marker:'failed',avatar_valid:false}));</script>`, nonce, sdkScript(parentURL, release, false), nonce, messageID, avatar.Len())
	}))
	defer child.Close()
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/done" {
			select {
			case done <- r.URL.Query().Get("ok") == "true":
			default:
			}
			_, _ = fmt.Fprint(w, "SDK浏览器验证完成")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>SDK可信父页</title><iframe id="child" src="%s" sandbox="allow-scripts allow-same-origin"></iframe><script>const frame=document.getElementById('child');const png=Uint8Array.from(atob(%q),value=>value.charCodeAt(0));let badCalls=0,avatarCalls=0,started=false;window.addEventListener('message',event=>{if(started||event.source!==frame.contentWindow||event.origin!==%q||event.data?.type!=='goodchat:init')return;started=true;const hello=event.data;function reply(overrides,bad){const connection=new MessageChannel();connection.port1.onmessage=event=>{const request=event.data;if(bad)badCalls++;if(request.method==='getState')connection.port1.postMessage({id:request.id,result:{marker:bad?'wrong':'authorized'}});else if(request.method==='getAvatar'){if(request.payload?.message_id!==%q){connection.port1.postMessage({id:request.id,error:{message:'头像必须绑定当前消息'}});return}if(!bad)avatarCalls++;connection.port1.postMessage({id:request.id,result:{file:new Blob([png],{type:'image/png'})}})}else if(request.method==='browserProof')location.href='/done?ok='+(!bad&&badCalls===0&&avatarCalls===1&&request.payload.marker==='authorized'&&request.payload.avatar_valid===true&&request.payload.avatar_type==='image/png'&&request.payload.avatar_size===png.byteLength)};connection.port1.start();frame.contentWindow.postMessage({type:'goodchat:port',version:1,nonce:hello.nonce,release_id:hello.release_id,...overrides},%q,[connection.port2])}reply({nonce:'invalid-once-only-nonce'},true);reply({version:2},true);reply({release_id:'other-release'},true);setTimeout(()=>reply({},false),50)});</script>`, child.URL, base64.StdEncoding.EncodeToString(avatar.Bytes()), child.URL, messageID, child.URL)
	}))
	defer parent.Close()
	parentURL = parent.URL
	t.Log("浏览器SDK验证地址：", parent.URL)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("SDK错误握手未被拒绝，或头像PNG Blob未经过合法专用通道完整返回")
		}
	case <-time.After(45 * time.Second):
		t.Fatal("未收到浏览器SDK验证结果")
	}
}
