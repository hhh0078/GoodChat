// 文件作用说明：提供最小可用的官方聊天模板、外部AI编辑规范和版本化交付提示词。
package decoration

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
)

const starterVersion = "1.0.0"

// starterDocuments 将导入向导与外部AI实际读取的提示词保持为同一来源。
//
//go:embed starter/*.md
var starterDocuments embed.FS

// starterPackage 在固定核心之外附带可编辑页面、版本清单和纯文本说明，不改变历史模板。
func starterPackage() (*staticPackage, error) {
	markup := `<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>在线咨询</title><link rel="stylesheet" href="static/style.css"></head><body><main class="gc-template"><header class="gc-brandbar"><strong>在线咨询</strong><button type="button" data-gc-action="leave-message">留言</button><button type="button" data-gc-action="evaluation">评价</button></header>`
	chat := templateChatMarkup()
	// 基础版只保留官方状态挂载点，不增加第二套标题或说明弹框。
	start := strings.Index(chat, `<div class="gc-chat-heading"`)
	if start < 0 {
		return nil, invalidDesign("基础聊天标题挂载点缺失")
	}
	boundary := strings.Index(chat[start:], "</div>")
	if boundary < 0 {
		return nil, invalidDesign("基础聊天标题结构不完整")
	}
	end := boundary + start + len("</div>")
	chat = chat[:start] + `<span id="gc-status" hidden role="status"></span>` + chat[end:]
	markup += chat + `</main>` + templateScriptsMarkup(false) + `</body></html>`
	css := industryBaseCSS + `
/* 基础模板：紧凑单栏，消息滚动、输入区固定，保留官方消息类型。 */
:root{--gc-primary:#2159df;--gc-ink:#17243b;--gc-muted:#718097;--gc-border:#e3e9f1;--gc-background:#f6f8fb;--gc-peer:#f0f3f7;--gc-composer:#fff}
.gc-template{max-width:880px;background:#fff}.gc-brandbar{display:flex;align-items:center;gap:8px;min-height:56px;padding:8px 20px;border-bottom:1px solid var(--gc-border)}.gc-brandbar strong{margin-right:auto;font-size:16px}.gc-brandbar button{min-height:36px;background:none;border:0;color:var(--gc-primary);font-size:13px}#gc-messages{padding:20px 18px}.gc-message{font-size:15px}.gc-avatar{width:32px;height:32px}.gc-message-row{padding-left:42px;margin-bottom:20px}.gc-own{padding-left:0;padding-right:42px}.gc-composer-area{padding:10px 14px}.gc-system{padding:0}#gc-file[hidden]{display:none!important}
@media(prefers-reduced-motion:reduce){*{animation:none!important;transition:none!important}}
`
	pkg, err := buildTemplatePackage("index.html", []TemplateFile{{Path: "index.html", Content: markup}, {Path: "static/style.css", Content: css}}, defaultTemplateScripts())
	if err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(templateArchiveMetadata{SchemaVersion: 1, Name: "GoodChat 基础聊天模板", Version: starterVersion, Entrypoint: "index.html", SDKVersion: defaultTemplateScripts().Version})
	docs := map[string][]byte{"template.json": metadata}
	for _, name := range []string{"SKILL.md", "README.md", "PROMPT.md"} {
		raw, readErr := starterDocuments.ReadFile("starter/" + name)
		if readErr != nil {
			return nil, readErr
		}
		docs[name] = raw
	}
	for name, raw := range docs {
		file, checkErr := checkStaticFile(name, raw)
		if checkErr != nil {
			return nil, checkErr
		}
		pkg.Files = append(pkg.Files, file)
	}
	return finishPackage(pkg.Entrypoint, pkg.Files)
}

// starterPrompt 返回下载包中的同一提示词，避免页面说明与AI实际规范漂移。
func (s *Service) starterPrompt(w http.ResponseWriter, r *http.Request) {
	raw, err := starterDocuments.ReadFile("starter/PROMPT.md")
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	respond(w, r, map[string]any{"prompt": string(raw), "version": starterVersion}, nil)
}
