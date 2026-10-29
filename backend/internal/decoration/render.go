// 文件作用说明：仅在独立版本子域提供静态装修设计，设置拒绝外网和平台凭证的限制性浏览器策略。
package decoration

import (
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// CodeHandler 提供隔离静态站点处理器，不注册认证、客户、消息或附件接口。
func (s *Service) CodeHandler() http.Handler { return http.HandlerFunc(s.render) }

// render 在精确版本子域定位发布或短期预览，并输出与后台完全分离的静态HTML。
func (s *Service) render(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), display-capture=()")
	if r.Method != "GET" {
		http.Error(w, "仅支持静态页面读取", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/preview-assets/") {
		s.renderPackageAsset(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/render/")
	if !strings.HasPrefix(r.URL.Path, "/render/") || idValue(id) != nil {
		http.NotFound(w, r)
		return
	}
	expected, _ := url.Parse(s.renderURL(id, ""))
	if !strings.EqualFold(r.Host, expected.Host) {
		http.Error(w, "请通过独立版本来源打开装修页面", http.StatusMisdirectedRequest)
		return
	}
	if len(r.URL.Query()["preview"]) > 1 || len(r.URL.Query().Get("preview")) > 128 {
		http.Error(w, "预览凭证格式不正确", http.StatusBadRequest)
		return
	}
	token := r.URL.Query().Get("preview")
	snapshot, err := s.staticSnapshot(r, id, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	design, err := validateDesign(snapshot.RenderMode, snapshot.Design)
	if err != nil {
		http.Error(w, "页面校验失败，请使用基础咨询入口", http.StatusUnprocessableEntity)
		return
	}
	if snapshot.Entrypoint != "" {
		http.Redirect(w, r, packagePrefix(id, token)+escapePackagePath(snapshot.Entrypoint), http.StatusFound)
		return
	}
	nonce, err := newID()
	if err != nil {
		http.Error(w, "页面暂不可用", http.StatusInternalServerError)
		return
	}
	public, _ := url.Parse(s.cfg.PublicURL)
	parentOrigin := public.Scheme + "://" + public.Host
	setRenderPolicy(w, nonce, parentOrigin)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	code := CodeDesign{}
	if snapshot.RenderMode == "schema" {
		var schema Schema
		_ = json.Unmarshal(design, &schema)
		code = schemaCode(schema, token != "")
	} else {
		_ = json.Unmarshal(design, &code)
	}
	sdk := sdkScript(parentOrigin, id, token != "")
	_, _ = fmt.Fprintf(w, "<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1,viewport-fit=cover\"><title>GoodChat 在线咨询</title><style>%s</style></head><body>%s<script nonce=\"%s\">%s</script><script nonce=\"%s\">%s</script></body></html>", code.CSS, code.HTML, nonce, sdk, nonce, code.Javascript)
}

// setRenderPolicy 叠加两条CSP，使脚本同时满足随机nonce与仅内联要求，阻止脚本复用nonce加载外链。
func setRenderPolicy(w http.ResponseWriter, nonce, parentOrigin string) {
	csp := "default-src 'none'; script-src 'nonce-" + nonce + "'; style-src 'unsafe-inline'; img-src data: blob:; media-src blob:; font-src 'none'; connect-src 'none'; frame-src 'none'; worker-src 'none'; object-src 'none'; manifest-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors " + parentOrigin + "; sandbox allow-scripts allow-same-origin"
	w.Header().Set("Content-Security-Policy", csp)
	// W3C要求多个策略同时执行；第二条没有nonce，因此即使租户读取nonce也只能执行内联脚本。
	w.Header().Add("Content-Security-Policy", "script-src 'unsafe-inline'")
	// 任意脚本仍能导航自身窗口；CSP不能承诺当前授权消息永不外传。
}

// schemaCode 将受控页面结构转换为静态独立文档，所有用户文案按文本转义。
func schemaCode(schema Schema, preview bool) CodeDesign {
	var out strings.Builder
	out.WriteString("<main class=\"gc-page\"><header>")
	controlledLogo := strings.HasPrefix(schema.Brand.LogoURL, "data:image/png;base64,") && validateLogoData(schema.Brand.LogoURL) == nil
	if controlledLogo {
		out.WriteString("<img class=\"gc-brand-logo\" width=\"64\" height=\"64\" style=\"object-fit:contain\" alt=\"品牌Logo\" src=\"" + html.EscapeString(schema.Brand.LogoURL) + "\">")
	}
	out.WriteString("<h1>" + html.EscapeString(schema.Brand.Title) + "</h1><p>" + html.EscapeString(schema.Brand.Subtitle) + "</p></header>")
	if preview {
		out.WriteString("<p class=\"gc-preview\">布局预览 · 此页面不连接真实访客会话</p>")
		if schema.Brand.LogoURL != "" && !controlledLogo {
			out.WriteString("<p class=\"gc-preview\">品牌Logo尚未导入受控素材；隔离页面暂不加载外链Logo。</p>")
		}
	}
	for _, block := range schema.Blocks {
		out.WriteString("<section class=\"gc-block gc-" + html.EscapeString(block.Type) + "\"><h2>" + html.EscapeString(block.Title) + "</h2>")
		if block.Content != "" {
			out.WriteString("<p>" + html.EscapeString(block.Content) + "</p>")
		}
		for _, item := range block.Items {
			title := item["question"]
			if title == "" {
				title = item["title"]
			}
			content := item["answer"]
			if content == "" {
				content = item["content"]
			}
			if content == "" {
				content = item["value"]
			}
			out.WriteString("<article><strong>" + html.EscapeString(title) + "</strong><p>" + html.EscapeString(content) + "</p></article>")
		}
		if block.Type == "chat" {
			out.WriteString(`<div id="gc-status" role="status">正在连接咨询服务…</div><div id="gc-messages" role="log" aria-label="聊天消息"></div><p id="gc-typing" role="status" aria-live="polite"></p><div id="gc-reply-bar" hidden><span id="gc-reply-text"></span><button id="gc-reply-cancel" type="button">取消引用</button></div><div id="gc-compose"><div class="gc-capsule"><button id="gc-add" type="button" aria-label="添加附件" aria-expanded="false">＋</button><textarea id="gc-text" maxlength="10000" rows="1" placeholder="输入消息…" aria-label="消息内容"></textarea><button id="gc-send" type="button" aria-label="发送消息">➤</button></div><div id="gc-attachment-menu" hidden><button type="button" data-kind="image">图片</button><button type="button" data-kind="file">文件</button><button type="button" data-kind="video">视频</button></div><input id="gc-file" type="file" aria-label="选择附件" hidden></div><p id="gc-error" role="alert"></p>`)
		}
		out.WriteString("</section>")
	}
	out.WriteString("</main>")
	css := fmt.Sprintf(`*{box-sizing:border-box}body{margin:0;background:%s;color:%s;font-family:system-ui,-apple-system,sans-serif;line-height:1.6}.gc-page{max-width:840px;margin:auto;padding:24px 16px calc(24px + env(safe-area-inset-bottom))}header{padding:16px 4px}h1{font-size:28px;margin:0}h2{font-size:19px;margin:0 0 12px}p{white-space:pre-wrap;overflow-wrap:anywhere}.gc-block{border:1px solid #7a8b7d44;border-radius:%dpx;margin-bottom:16px;padding:20px;background:#ffffff0a}article{padding:12px 0;border-top:1px solid #7a8b7d33}#gc-status{font-size:13px;opacity:.7;margin-bottom:12px}#gc-messages{min-height:160px;max-height:440px;overflow:auto;overscroll-behavior:contain}.gc-message{padding:10px 12px;margin:10px 0;border-radius:12px;background:#7a8b7d18;white-space:pre-wrap;overflow-wrap:anywhere}.gc-message img{max-width:100%%;max-height:360px}.gc-message audio{max-width:100%%}textarea{display:block;width:100%%;min-height:76px;resize:vertical;border:1px solid #7a8b7d77;border-radius:10px;padding:12px;font:inherit;background:transparent;color:inherit}.gc-actions{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-top:10px}button{font:inherit;cursor:pointer;border-radius:10px;padding:10px 18px}button{border:0;background:%s;color:white}button:disabled{opacity:.5;cursor:wait}#gc-error{color:#aa382c;font-size:13px}.gc-preview{padding:10px;border:1px dashed #7a8b7d77;border-radius:10px}@media(max-width:480px){.gc-page{padding:12px 10px calc(12px + env(safe-area-inset-bottom))}.gc-block{padding:14px}h1{font-size:24px}#gc-messages{max-height:48dvh}}`, schema.Theme.Background, schema.Theme.TextColor, schema.Theme.Radius, schema.Theme.PrimaryColor)
	chatCSS := fmt.Sprintf(`:root{--gc-primary:%s}.gc-message-row{display:flex;flex-direction:column;align-items:flex-start;margin:14px 0}.gc-message-row.gc-own{align-items:flex-end}.gc-message-meta{display:flex;gap:10px;align-items:center;font-size:12px;opacity:.72;margin:0 4px 4px}.gc-message-meta time{font-size:11px}.gc-message-row .gc-message{max-width:86%%;margin:0}.gc-own .gc-message{background:color-mix(in srgb,var(--gc-primary) 20%%,transparent);border:1px solid color-mix(in srgb,var(--gc-primary) 45%%,transparent)}.gc-system{align-items:center;margin:12px 0}.gc-system .gc-message{font-size:12px;opacity:.7;background:transparent;text-align:center}.gc-system .gc-message-meta{justify-content:center}.gc-read-status{font-size:11px;opacity:.65;margin:3px 4px 0}.gc-reply{font-size:12px;border-left:3px solid #7a8b7d77;padding:4px 8px;margin:0 0 8px;opacity:.8;white-space:pre-wrap;overflow-wrap:anywhere}.gc-message-body{white-space:pre-wrap;overflow-wrap:anywhere}#gc-typing{min-height:20px;font-size:12px;opacity:.72;margin:4px 0 8px}#gc-messages:empty:after{content:'可以在下方发送消息，客服会尽快为您服务。';display:block;font-size:13px;opacity:.65;text-align:center;padding:36px 10px}@media(max-width:480px){.gc-message-row .gc-message{max-width:92%%}}`, schema.Theme.PrimaryColor)
	chatCSS += `.gc-message-actions{display:flex;gap:6px;align-items:center;margin-top:3px;font-size:12px}.gc-message-actions button,#gc-reply-cancel{font:inherit;font-size:12px;background:transparent;color:inherit;padding:2px 6px;border:1px solid transparent;border-radius:4px;opacity:.8}.gc-message-actions button:hover,#gc-reply-cancel:hover{border-color:#7a8b7d77}#gc-reply-bar:not([hidden]){display:flex;justify-content:space-between;gap:12px;align-items:center;padding:8px 10px;margin-bottom:8px;border-left:3px solid #7a8b7d77;background:#7a8b7d16;font-size:12px}#gc-reply-text{overflow-wrap:anywhere}`
	chatCSS += `.gc-avatar{width:32px;height:32px;border-radius:50%;object-fit:cover;flex-shrink:0}`
	chatCSS += `.gc-file-control{display:flex;flex-direction:column;gap:4px;min-width:0;max-width:70%;font-size:12px}#gc-file{max-width:100%;font:inherit;color:inherit}#gc-file::file-selector-button{cursor:pointer;font:inherit;background:transparent;color:inherit;border:1px solid #7a8b7d77;border-radius:8px;padding:8px 10px;margin-right:6px}.gc-file-save{font:inherit;font-size:13px;background:transparent;color:inherit;border:1px solid #7a8b7d77;padding:7px 10px;max-width:100%;overflow-wrap:anywhere}.gc-file-notice,.gc-attachment-error{display:block;font-size:12px;margin-top:5px}.gc-attachment-error{color:#aa382c}.gc-attachment-preview{margin-top:8px}`
	chatCSS += `.gc-message-actions[hidden],#gc-attachment-menu[hidden]{display:none}#gc-compose{position:relative}.gc-capsule{display:flex;align-items:flex-end;gap:7px;padding:7px;border:1px solid #7a8b7d55;border-radius:28px;background:#fff;color:#1d2939}#gc-text{flex:1;min-width:0;min-height:0;max-height:120px;margin:0;padding:8px 2px;border:0;border-radius:0;resize:none;font-size:14px;line-height:24px;outline:none}#gc-add,#gc-send{flex-shrink:0;width:38px;height:38px;border-radius:50%;padding:0;font-size:20px}#gc-add{background:transparent;color:inherit;font-size:26px}#gc-attachment-menu{position:absolute;z-index:10;left:0;bottom:calc(100% + 8px);display:flex;gap:6px;padding:8px;background:#fff;border:1px solid #7a8b7d44;border-radius:12px;box-shadow:0 8px 24px #172b4d18}#gc-attachment-menu button{font-size:13px;padding:6px 12px}.gc-message:focus-visible{outline:2px solid var(--gc-primary);outline-offset:3px}.gc-attachment-preview video{max-width:100%;max-height:300px;border-radius:10px}`
	chatCSS += `#gc-messages{overflow-anchor:none}.gc-history-control{display:grid;justify-items:center;gap:4px;padding:4px 0 12px}.gc-history-control[hidden]{display:none}.gc-history-control button{min-height:36px;padding:5px 12px;border:1px solid #7a8b7d55;background:transparent;color:inherit;font-size:12px}.gc-history-error{font-size:12px;color:#aa382c;white-space:pre-wrap;overflow-wrap:anywhere}.gc-message:has(.gc-message-options){width:360px}.gc-message-options{display:flex;flex-direction:column;gap:8px;width:100%;max-width:320px;margin-top:10px}.gc-message-options .gc-welcome-option{width:100%;max-width:320px;min-height:44px;padding:10px 12px;white-space:normal;overflow-wrap:anywhere;text-align:left;font-size:14px;line-height:1.5}`
	return CodeDesign{HTML: out.String(), CSS: css + "\n" + chatCSS + "\n" + schema.CustomCSS, Javascript: schemaChatScript}
}

// hostIsIP 检查代码源是否为无法分配独立版本子域的IP地址。
func hostIsIP(host string) bool { return net.ParseIP(host) != nil }
