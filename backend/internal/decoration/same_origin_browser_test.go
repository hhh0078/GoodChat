// 文件作用说明：用真实浏览器验证同域装修的响应级沙箱、静态模块与受控SDK桥接，不访问业务数据库。
package decoration

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestBrowserSameOriginSandbox 显式开启后验证主站Cookie、存储和DOM隔离及同域模块、样式、图片和消息通道。
func TestBrowserSameOriginSandbox(t *testing.T) {
	if os.Getenv("GOODCHAT_BROWSER_SAME_ORIGIN") != "1" {
		t.Skip("需要显式开启并由真实浏览器访问日志中的地址")
	}
	const id = "20000000-0000-4000-8000-000000000001"
	const prefix = "/assets/" + id + "/"
	var forbidden atomic.Int32
	done := make(chan map[string]bool, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/done" {
			var proof map[string]bool
			_ = json.NewDecoder(r.Body).Decode(&proof)
			select {
			case done <- proof:
			default:
			}
			return
		}
		if r.URL.Path == "/outside.js" || strings.HasPrefix(r.URL.Path, "/api/") {
			forbidden.Add(1)
			http.Error(w, "禁止请求", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, prefix) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			switch strings.TrimPrefix(r.URL.Path, prefix) {
			case "module.mjs":
				w.Header().Set("Content-Type", "application/javascript")
				_, _ = fmt.Fprint(w, `import {value} from './helper.mjs';window.moduleProof=value;`)
				return
			case "helper.mjs":
				w.Header().Set("Content-Type", "application/javascript")
				_, _ = fmt.Fprint(w, `export const value=42;`)
				return
			case "style.css":
				w.Header().Set("Content-Type", "text/css")
				_, _ = fmt.Fprint(w, `body{--sandbox-loaded:yes}`)
				return
			case "logo.png":
				w.Header().Set("Content-Type", "image/png")
				b, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a9WQAAAAASUVORK5CYII=")
				_, _ = w.Write(b)
				return
			}
			setPackageRenderPolicy(w, "sandbox-browser-proof", server.URL, server.URL+prefix)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><link rel="stylesheet" crossorigin="anonymous" href="style.css"><img id="logo" crossorigin="anonymous" src="logo.png"><script nonce="sandbox-browser-proof">%s</script><script type="module" crossorigin="anonymous" src="module.mjs"></script><script nonce="sandbox-browser-proof">
const blocked=fn=>{try{fn();return false}catch{return true}};
const proof={opaque:window.origin==='null',cookie:blocked(()=>document.cookie),storage:blocked(()=>localStorage.getItem('parent-private')),parent:blocked(()=>parent.document.body)};
fetch('/api/private').then(()=>proof.network=false,()=>proof.network=true);
const script=document.createElement('script');script.nonce=document.currentScript.nonce;script.src='/outside.js';document.head.append(script);
setTimeout(async()=>{proof.module=window.moduleProof===42;proof.style=getComputedStyle(document.body).getPropertyValue('--sandbox-loaded').trim()==='yes';proof.image=document.getElementById('logo').naturalWidth===1;const state=await GoodChat.getState();proof.bridge=state.marker==='authorized';await GoodChat.sendText('沙箱验收');await GoodChat.request('browserProof',proof)},700);
</script>`, sdkScript(server.URL, id, false))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "parent-private", Value: "not-for-frame", Path: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><title>同域装修隔离验收</title><iframe id="child" sandbox="allow-scripts" src="%sindex.html"></iframe><script>
localStorage.setItem('parent-private','not-for-frame');const frame=document.getElementById('child');let connected=false,sent=false;
window.addEventListener('message',event=>{const hello=event.data;if(connected||event.source!==frame.contentWindow||event.origin!=='null'||hello?.type!=='goodchat:init'||hello.release_id!==%q)return;connected=true;const connection=new MessageChannel();connection.port1.onmessage=event=>{const call=event.data;let result={};if(call.method==='getState')result={marker:'authorized'};else if(call.method==='sendText'){sent=call.payload.text==='沙箱验收'}else if(call.method==='browserProof'){fetch('/done',{method:'POST',body:JSON.stringify({...call.payload,sent})}).then(()=>document.title='同域装修隔离验收通过')};connection.port1.postMessage({id:call.id,result})};connection.port1.start();frame.contentWindow.postMessage({type:'goodchat:port',version:1,nonce:hello.nonce,release_id:hello.release_id},'*',[connection.port2])});</script>`, prefix, id)
	}))
	defer server.Close()
	t.Log("同域沙箱浏览器验证地址：", server.URL)
	select {
	case proof := <-done:
		for _, key := range []string{"opaque", "cookie", "storage", "parent", "network", "module", "style", "image", "bridge", "sent"} {
			if !proof[key] {
				t.Errorf("浏览器未通过%s: %v", key, proof)
			}
		}
		if forbidden.Load() != 0 {
			t.Fatal("沙箱仍发出了被禁止的主站或脚本请求")
		}
	case <-time.After(90 * time.Second):
		t.Fatal("没有收到真实浏览器验证结果")
	}
}
