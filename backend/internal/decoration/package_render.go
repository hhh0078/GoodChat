// 文件作用说明：在每个发布独立来源中按不可变白名单提供资源，短期预览令牌随相对CSS和ES模块路径继承。
package decoration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// renderSnapshot 只包含公开或短期授权的静态设计元数据，不含任何客户或管理员凭证。
type renderSnapshot struct {
	RenderMode string          `json:"render_mode"`
	Design     json.RawMessage `json:"design"`
	Entrypoint string          `json:"package_entrypoint"`
	Manifest   []packageFile   `json:"package_manifest"`
}

// packagePrefix 将预览令牌固定到资源目录，嵌套相对import和url不依赖第三方Cookie或查询参数传递。
func packagePrefix(id, token string) string {
	if token != "" {
		return "/preview-assets/" + id + "/" + url.PathEscape(token) + "/"
	}
	return "/assets/" + id + "/"
}

// staticSnapshot 通过受限数据库函数读取指定发布或有效短期预览。
func (s *Service) staticSnapshot(r *http.Request, id, token string) (renderSnapshot, error) {
	var raw json.RawMessage
	var snapshot renderSnapshot
	err := s.pool.QueryRow(r.Context(), "SELECT decoration_render($1::uuid,$2)", id, digest([]byte(token))).Scan(&raw)
	if err != nil {
		return snapshot, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return snapshot, invalidDesign("静态版本不存在或预览已过期")
	}
	err = json.Unmarshal(raw, &snapshot)
	return snapshot, err
}

// staticHostMatches 每个发布只能在自身来源服务，禁止在另一版本来源读取或执行文件。
func (s *Service) staticHostMatches(r *http.Request, id string) bool {
	expected, _ := url.Parse(s.renderURL(id, ""))
	return strings.EqualFold(r.Host, expected.Host)
}

// renderPackageAsset 只接受绑定版本的规范路径，所有HTML继续使用同一沙箱与SDK来源握手。
func (s *Service) renderPackageAsset(w http.ResponseWriter, r *http.Request) {
	var id, token, name string
	preview := strings.HasPrefix(r.URL.Path, "/preview-assets/")
	if preview {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/preview-assets/"), "/", 3)
		if len(parts) != 3 {
			http.NotFound(w, r)
			return
		}
		id, token, name = parts[0], parts[1], parts[2]
		if len(token) < 20 || len(token) > 128 || strings.ContainsAny(token, "%\\?#") {
			http.NotFound(w, r)
			return
		}
	} else {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/assets/"), "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		id, name = parts[0], parts[1]
	}
	if idValue(id) != nil || packagePath(name) != nil || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	if !s.staticHostMatches(r, id) {
		http.Error(w, "静态资源来源不匹配", http.StatusMisdirectedRequest)
		return
	}
	snapshot, err := s.staticSnapshot(r, id, token)
	if err != nil || snapshot.Entrypoint == "" {
		http.NotFound(w, r)
		return
	}
	var mime, hash string
	var raw []byte
	if err := s.pool.QueryRow(r.Context(), "SELECT content_type,content,sha256 FROM decoration_package_asset($1::uuid,$2,$3)", id, digest([]byte(token)), name).Scan(&mime, &raw, &hash); err != nil {
		http.NotFound(w, r)
		return
	}
	nonce, err := newID()
	if err != nil {
		http.Error(w, "静态资源暂不可用", 500)
		return
	}
	public, _ := url.Parse(s.cfg.PublicURL)
	parent := public.Scheme + "://" + public.Host
	setPackageRenderPolicy(w, nonce, parent)
	files := map[string]packageFile{}
	for _, file := range snapshot.Manifest {
		files[file.Path] = file
	}
	prefix := packagePrefix(id, token)
	if strings.HasPrefix(mime, "text/html") {
		var code CodeDesign
		if json.Unmarshal(snapshot.Design, &code) != nil {
			http.Error(w, "静态设计暂不可用", 422)
			return
		}
		if name == snapshot.Entrypoint && code.HTML != "" {
			raw = []byte(code.HTML)
		}
		if name != snapshot.Entrypoint {
			code.CSS = ""
			code.Javascript = ""
		}
		raw, err = packageHTML(raw, name, prefix, nonce, sdkScript(parent, id, preview), files, code)
	} else if strings.HasPrefix(mime, "text/css") {
		var css string
		css, err = packageCSS(string(raw), name, prefix, files)
		raw = []byte(css)
	}
	if err != nil {
		http.Error(w, "静态资源校验失败，请返回基础咨询入口", 422)
		return
	}
	w.Header().Set("Content-Type", mime)
	// 短期预览始终no-store；发布资源也保持no-store，防止已退出页面的浏览器误用另一预览内容。
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

// setPackageRenderPolicy 只扩展到当前发布白名单静态来源，不增加外网、API、子框架、表单或下载权限。
func setPackageRenderPolicy(w http.ResponseWriter, nonce, parent string) {
	csp := "default-src 'none'; script-src 'self' 'nonce-" + nonce + "'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; font-src 'self' data:; connect-src 'none'; frame-src 'none'; worker-src 'none'; object-src 'none'; manifest-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors " + parent + "; sandbox allow-scripts allow-same-origin"
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Add("Content-Security-Policy", "script-src 'self' 'unsafe-inline'")
	// 任意脚本仍可导航自身窗口，隔离不承诺获授权展示的聊天正文绝对不可外传。
}
