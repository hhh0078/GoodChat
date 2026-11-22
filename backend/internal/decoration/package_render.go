// 文件作用说明：在每个发布独立来源中按不可变白名单提供资源，短期预览令牌随相对CSS和ES模块路径继承。
package decoration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"supportdesk/backend/internal/localization"
	"supportdesk/backend/internal/platform"
)

// renderSnapshot 只包含公开或短期授权的静态设计元数据，不含任何客户或管理员凭证。
type renderSnapshot struct {
	RenderMode  string          `json:"render_mode"`
	Design      json.RawMessage `json:"design"`
	Entrypoint  string          `json:"package_entrypoint"`
	Manifest    []packageFile   `json:"package_manifest"`
	Ephemeral   bool            `json:"ephemeral_package"`
	EditorNonce string          `json:"editor_nonce"`
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
	expected, _ := url.Parse(s.renderURLFor(r.Context(), id, ""))
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
	if idValue(id) != nil || packagePath(name) != nil || !validRenderPreferences(r) {
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
	public, _ := url.Parse(platform.PublicURL(r.Context(), s.cfg.PublicURL))
	parent := public.Scheme + "://" + public.Host
	code, _ := url.Parse(s.cfg.CodeOrigin)
	setPackageRenderPolicy(w, nonce, parent, code.Scheme+"://"+r.Host+packagePrefix(id, token))
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
		sdk := sdkScript(parent, id, preview)
		if templateHasContract(files) {
			if code.HTML != "" || code.CSS != "" || code.Javascript != "" {
				http.Error(w, "固定SDK模板请修订静态文件，不能覆盖入口代码", 422)
				return
			}
			sdk = templateBootScript(parent, id, preview)
		}
		if preview && snapshot.Ephemeral && snapshot.EditorNonce != "" && name == snapshot.Entrypoint {
			sdk += "\n" + editorPreviewBoot(parent, id, snapshot.EditorNonce)
			raw, err = markEditorDocument(raw)
			if err != nil {
				http.Error(w, "编辑文档校验失败", http.StatusUnprocessableEntity)
				return
			}
		}
		sdk = visitorLocaleBoot(r) + sdk
		raw = localizeChatControls(raw, localization.RequestLocale(r))
		raw, err = packageHTML(raw, name, prefix, nonce, sdk, files, code)
		if err == nil {
			raw = compactWidgetDocument(r, raw)
		}
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
func setPackageRenderPolicy(w http.ResponseWriter, nonce, parent, static string) {
	csp := "default-src 'none'; script-src " + static + " 'nonce-" + nonce + "'; style-src " + static + " 'unsafe-inline'; img-src " + static + " data: blob:; media-src " + static + " blob:; font-src " + static + " data:; connect-src 'none'; frame-src 'none'; worker-src 'none'; object-src 'none'; manifest-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors " + parent + "; sandbox allow-scripts"
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Add("Content-Security-Policy", "script-src "+static+" 'unsafe-inline'")
	// 不透明来源加载模块与字体需要匿名CORS，静态处理器不提供认证接口且不允许携带凭据读取。
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// 任意脚本仍可导航自身窗口，隔离不承诺获授权展示的聊天正文绝对不可外传。
}
