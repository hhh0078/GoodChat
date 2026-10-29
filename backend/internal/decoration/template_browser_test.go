// 文件作用说明：按需启动六行业静态模板的真实浏览器验收服务，使用生产渲染策略和SDK模拟分支。
package decoration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBrowserIndustryTemplates 显式运行时提供真实模板预览地址，等待浏览器完成桌面和手机交互验收。
func TestBrowserIndustryTemplates(t *testing.T) {
	if os.Getenv("GOODCHAT_TEMPLATE_BROWSER_TEST") != "1" {
		t.Skip("需要显式开启并由浏览器验收六行业静态模板")
	}
	packages := map[string]map[string]packageFile{}
	for _, industry := range industryTemplateCatalog() {
		pkg, err := industryTemplatePackage(industry.Key, defaultTemplateScripts())
		if err != nil {
			t.Fatal(err)
		}
		packages[industry.Key] = templateFileMap(pkg)
	}
	done := make(chan bool, 1)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/done" {
			select {
			case done <- r.URL.Query().Get("ok") == "true":
			default:
			}
			_, _ = fmt.Fprint(w, "六行业浏览器验收结束")
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>行业模板验收</title>`)
			for _, item := range industryTemplateCatalog() {
				_, _ = fmt.Fprintf(w, `<a href="/%s/index.html">%s</a><br>`, item.Key, item.Name)
			}
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		files, ok := packages[parts[0]]
		file, exists := files[parts[1]]
		if !ok || !exists {
			http.NotFound(w, r)
			return
		}
		setPackageRenderPolicy(w, "browser-template-test", origin)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", file.ContentType)
		raw := file.Content
		if strings.HasPrefix(file.ContentType, "text/html") {
			var err error
			raw, err = packageHTML(raw, file.Path, "/"+parts[0]+"/", "browser-template-test", templateBootScript(origin, parts[0], true), files)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	origin = server.URL
	t.Log("六行业静态模板浏览器验收地址：", server.URL)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("浏览器报告行业模板验收失败")
		}
	case <-time.After(180 * time.Second):
		t.Fatal("未收到浏览器行业模板验收结果")
	}
}
