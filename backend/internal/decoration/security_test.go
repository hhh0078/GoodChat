// 文件作用说明：验证供应商元数据脱敏、AI任务撤权和浏览器实际执行的CSP交集边界。
package decoration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestBrowserCSPIntersection 通过显式开启的真实浏览器验证nonce不能授权外部脚本；普通测试不启动浏览器。
func TestBrowserCSPIntersection(t *testing.T) {
	if os.Getenv("GOODCHAT_BROWSER_CSP_TEST") != "1" {
		t.Skip("需要显式开启并由浏览器打开测试地址")
	}
	var external atomic.Int32
	done := make(chan map[string]string, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/external.js" {
			external.Add(1)
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = fmt.Fprint(w, "window.externalRan=true")
			return
		}
		if r.URL.Path == "/done" {
			values := map[string]string{}
			for _, key := range []string{"sdk", "code", "unapproved", "external"} {
				values[key] = r.URL.Query().Get(key)
			}
			select {
			case done <- values:
			default:
			}
			_, _ = fmt.Fprint(w, "浏览器安全验证完成")
			return
		}
		setRenderPolicy(w, "browser-test-nonce-123456", server.URL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>GoodChat CSP本地测试</title><script>window.unapprovedRan=true</script><script nonce="browser-test-nonce-123456">window.sdkRan=true</script><script nonce="browser-test-nonce-123456">window.codeRan=true;const script=document.createElement('script');script.nonce=document.currentScript.nonce;script.src='%s/external.js';document.head.append(script);setTimeout(()=>{location.href='/done?sdk='+Boolean(window.sdkRan)+'&code='+Boolean(window.codeRan)+'&unapproved='+Boolean(window.unapprovedRan)+'&external='+Boolean(window.externalRan)},500)</script>`, server.URL)
	}))
	defer server.Close()
	t.Log("浏览器CSP验证地址：", server.URL)
	select {
	case flags := <-done:
		if external.Load() != 0 || flags["sdk"] != "true" || flags["code"] != "true" || flags["unapproved"] != "false" || flags["external"] != "false" {
			t.Fatalf("CSP实际执行不符合预期：%v，外部请求%d", flags, external.Load())
		}
	case <-time.After(45 * time.Second):
		t.Fatal("未收到浏览器验证，需手动打开日志中的本地测试地址")
	}
}
