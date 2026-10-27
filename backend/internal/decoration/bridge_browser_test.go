// 文件作用说明：使用不同本地来源的父页和沙箱子页，验证真实浏览器中的SDK握手拒绝错误nonce、版本和发布号。
package decoration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestBrowserSDKProtocol 显式开启时检查真实SDK与专用MessageChannel，普通Go测试不会启动或控制浏览器。
func TestBrowserSDKProtocol(t *testing.T) {
	if os.Getenv("GOODCHAT_BROWSER_SDK_TEST") != "1" {
		t.Skip("需要显式开启并由浏览器打开测试地址")
	}
	const release = "20000000-0000-4000-8000-000000000001"
	const nonce = "browser-sdk-nonce-123456"
	var parentURL string
	done := make(chan bool, 1)
	child := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setRenderPolicy(w, nonce, parentURL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>SDK沙箱</title><script nonce="%s">%s</script><script nonce="%s">window.GoodChat.getState().then(state=>window.GoodChat.request('browserProof',{marker:state.marker}));</script>`, nonce, sdkScript(parentURL, release, false), nonce)
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
		_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>SDK可信父页</title><iframe id="child" src="%s" sandbox="allow-scripts allow-same-origin"></iframe><script>const frame=document.getElementById('child');let badCalls=0,started=false;window.addEventListener('message',event=>{if(started||event.source!==frame.contentWindow||event.origin!==%q||event.data?.type!=='goodchat:init')return;started=true;const hello=event.data;function reply(overrides,bad){const connection=new MessageChannel();connection.port1.onmessage=event=>{const request=event.data;if(bad)badCalls++;if(request.method==='getState')connection.port1.postMessage({id:request.id,result:{marker:bad?'wrong':'authorized'}});else if(request.method==='browserProof')location.href='/done?ok='+(!bad&&badCalls===0&&request.payload.marker==='authorized')};connection.port1.start();frame.contentWindow.postMessage({type:'goodchat:port',version:1,nonce:hello.nonce,release_id:hello.release_id,...overrides},%q,[connection.port2])}reply({nonce:'invalid-once-only-nonce'},true);reply({version:2},true);reply({release_id:'other-release'},true);setTimeout(()=>reply({},false),50)});</script>`, child.URL, child.URL, child.URL)
	}))
	defer parent.Close()
	parentURL = parent.URL
	t.Log("浏览器SDK验证地址：", parent.URL)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("SDK错误握手未被拒绝或未使用合法通道")
		}
	case <-time.After(45 * time.Second):
		t.Fatal("未收到浏览器SDK验证结果")
	}
}
