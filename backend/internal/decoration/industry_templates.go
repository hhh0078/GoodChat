// 文件作用说明：提供七套人工编写且版式独立的行业访客模板，统一复用受保护的聊天SDK。
package decoration

import (
	"embed"
	"fmt"
	"path"
	"strings"
)

// IndustryTemplate 只暴露官方素材目录，不引用任何租户的私有资源包编号。
type IndustryTemplate struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Industry    string `json:"industry"`
	Description string `json:"description"`
}

// industryBaseCSS 只共享消息、附件、输入与小视口的可用性样式，行业外壳各自独立。
//
//go:embed industry_base.css
var industryBaseCSS string

// industrySources 保存人工编写的行业页面、获确认设计稿对应照片及本地图标。
//
//go:embed industry_sources/*
var industrySources embed.FS

// industryTemplateCatalog 返回七套独立构图的公共设计，复制后成为租户自己的不可变资源包。
func industryTemplateCatalog() []IndustryTemplate {
	return []IndustryTemplate{
		{"finance", "金融 · 蓝金专属顾问", "金融", "参考顾问会客场景，浅蓝人物照片、金色服务标签与通高聊天。"},
		{"commerce", "电商 · 好物到家", "电商", "橙色品牌、真实生活摄影和开放式售后咨询。"},
		{"education", "教育 · 书页学习顾问", "教育", "暖白书桌摄影、墨绿文字与学习问题边注。"},
		{"medical", "医疗 · 清白预约服务台", "医疗", "清透白青、预约流程和科室指引，聊天占据主要空间。"},
		{"property", "房产 · 岩砂空间会客厅", "房产", "暖砂留白、真实空间摄影与编辑式分屏会客。"},
		{"business", "企业 · 石墨项目服务台", "企业服务", "黑白编辑排版、混凝土空间摄影与开放式咨询。"},
		{"gaming", "电竞 · 黑曜竞技服务站", "电竞", "黑曜底色、青柠点缀与真实电竞设备摄影。"},
	}
}

// templateChatMarkup 保留唯一固定聊天节点，所有消息和动作由chat-ui.js实现。
func templateChatMarkup() string {
	return `<section class="gc-conversation" data-gc-edit-id="chat-panel" aria-label="在线咨询"><div class="gc-chat-heading" data-gc-edit-id="chat-heading"><strong data-gc-edit-id="chat-title">在线咨询</strong><span id="gc-status" data-gc-edit-id="chat-status" role="status">正在连接咨询服务…</span><button type="button" data-dialog-open="gc-service-notice" class="gc-service-button">服务说明</button></div><div id="gc-messages" data-gc-edit-id="chat-messages" role="log" aria-label="聊天消息" aria-live="polite"></div><div class="gc-composer-area" data-gc-edit-id="composer-area"><p id="gc-typing" role="status" aria-live="polite"></p><div id="gc-reply-bar" hidden><span id="gc-reply-text"></span><button id="gc-reply-cancel" type="button">取消引用</button></div><div id="gc-compose" data-gc-edit-id="composer"><div class="gc-capsule" data-gc-edit-id="composer-capsule"><button id="gc-add" type="button" data-gc-edit-id="attachment-button" aria-label="添加附件" aria-expanded="false">＋</button><textarea id="gc-text" data-gc-edit-id="message-input" maxlength="10000" rows="1" placeholder="输入您想咨询的内容" aria-label="消息内容"></textarea><button id="gc-send" data-gc-edit-id="send-button" type="button" aria-label="发送消息">发送</button></div><div id="gc-attachment-menu" hidden><button type="button" data-kind="image">图片</button><button type="button" data-kind="file">文件</button><button type="button" data-kind="video">视频</button></div><input id="gc-file" type="file" aria-label="选择附件" hidden></div><p id="gc-error" role="alert"></p></div></section>`
}

// industryChatMarkup 为新版摄影模板添加本地库图标与表情，旧版导出和迁移继续使用原始节点。
func industryChatMarkup() string {
	markup := strings.Replace(templateChatMarkup(), ">＋</button>", `><img src="static/assets/icon-picture.png" alt="" width="25" height="25"></button>`, 1)
	markup = strings.Replace(markup, `</textarea><button id="gc-send"`, `</textarea><button type="button" class="gc-emoji-button" data-gc-ui="emoji-toggle" aria-label="选择表情" aria-expanded="false"><img src="static/assets/icon-smile.png" alt="" width="25" height="25"></button><button id="gc-send"`, 1)
	return strings.Replace(markup, `<input id="gc-file"`, `<div class="gc-emoji-menu" hidden><button type="button" data-gc-ui="emoji-insert" data-emoji="😊" aria-label="微笑">😊</button><button type="button" data-gc-ui="emoji-insert" data-emoji="👍" aria-label="赞">👍</button><button type="button" data-gc-ui="emoji-insert" data-emoji="🙏" aria-label="感谢">🙏</button></div><input id="gc-file"`, 1)
}

// templateScriptsMarkup 按顺序引用平台固定脚本，不携带租户身份和核心脚本字节。
func templateScriptsMarkup(custom bool) string {
	value := `<script src="static/goodchat/sdk.js"></script><script src="static/goodchat/chat-ui.js"></script>`
	if custom {
		value += `<script src="static/custom.js"></script>`
	}
	return value
}

// industryTemplatePackage 读取独立人工设计，注入唯一官方聊天挂载点并严格验证完整静态包。
func industryTemplatePackage(key string, scripts TemplateScripts) (*staticPackage, error) {
	found := false
	for _, item := range industryTemplateCatalog() {
		if item.Key == key {
			found = true
			break
		}
	}
	if !found {
		return nil, invalidDesign("未找到该行业的官方访客模板")
	}
	markup, err := industrySources.ReadFile("industry_sources/" + key + ".html")
	if err != nil {
		return nil, fmt.Errorf("读取行业HTML：%w", err)
	}
	css, err := industrySources.ReadFile("industry_sources/" + key + ".css")
	if err != nil {
		return nil, fmt.Errorf("读取行业CSS：%w", err)
	}
	assets := []packageFile{}
	entries, err := industrySources.ReadDir("industry_sources/assets")
	if err != nil {
		return nil, fmt.Errorf("读取模板素材目录：%w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasPrefix(name, "icon-") && !strings.HasPrefix(name, "avatar-") && name != key+"-hero.webp" && name != key+"-avatar.webp" && name != key+"-logo.png") {
			continue
		}
		raw, readErr := industrySources.ReadFile(path.Join("industry_sources/assets", name))
		if readErr != nil {
			return nil, fmt.Errorf("读取模板图片：%w", readErr)
		}
		assets = append(assets, packageFile{Path: "static/assets/" + name, Content: raw})
	}
	dialog := `<dialog id="gc-service-notice" class="gc-dialog" data-gc-edit-id="service-dialog"><h2 data-gc-edit-id="service-dialog-title">服务说明</h2><p data-gc-edit-id="service-dialog-copy">您可以发送文字、图片和文件咨询。服务入口只发送普通咨询文字，具体事项由客服与您确认。请勿发送支付密码、验证码等敏感信息。</p><button type="button" data-dialog-close>我知道了</button></dialog>`
	chat := industryChatMarkup()
	if key == "finance" {
		chat = strings.Replace(chat, `data-gc-edit-id="chat-title">在线咨询`, `data-gc-edit-id="chat-title">专属顾问`, 1)
	} else if key == "education" {
		chat = strings.Replace(chat, `data-gc-edit-id="chat-title">在线咨询`, `data-gc-edit-id="chat-title">学习咨询`, 1)
		chat = strings.Replace(chat, `placeholder="输入您想咨询的内容"`, `placeholder="写下你想聊的问题"`, 1)
	}
	content := strings.NewReplacer("{{CHAT}}", chat, "{{SCRIPTS}}", templateScriptsMarkup(true), "{{SERVICE_DIALOG}}", dialog).Replace(string(markup))
	return buildTemplatePackageAssets("index.html", []TemplateFile{{Path: "index.html", Content: content}, {Path: "static/style.css", Content: industryBaseCSS + "\n" + string(css)}, {Path: "static/custom.js", Content: industryDialogScript}}, scripts, assets)
}

// industryDialogScript 只控制本地说明框和有时限的页面开场，不实现网络或聊天业务。
const industryDialogScript = `/** 文件作用说明：控制行业模板本地说明弹框及不阻塞连接的短开场，编辑模式直接跳过。 */
(function(){'use strict';
for(const button of document.querySelectorAll('[data-dialog-open]')){
/** 打开当前模板的本地说明框，由浏览器管理焦点。 */
function openDialog(){const dialog=document.getElementById(button.getAttribute('data-dialog-open'));if(dialog&&typeof dialog.showModal==='function')dialog.showModal()}
button.addEventListener('click',openDialog)}
for(const button of document.querySelectorAll('[data-dialog-close]')){
/** 关闭本地说明框并恢复触发按钮焦点。 */
function closeDialog(){button.closest('dialog')?.close()}
button.addEventListener('click',closeDialog)}
const composer=document.querySelector('.gc-composer-area');
for(const note of document.querySelectorAll('[data-gc-composer-note]')){if(composer)composer.append(note)}
const emojiToggle=document.querySelector('[data-gc-ui="emoji-toggle"]'),emojiMenu=document.querySelector('.gc-emoji-menu');
/** 切换本地表情面板，不发送消息或调用业务接口。 */
function toggleEmoji(){if(!emojiMenu)return;emojiMenu.hidden=!emojiMenu.hidden;emojiToggle?.setAttribute('aria-expanded',String(!emojiMenu.hidden))}
emojiToggle?.addEventListener('click',toggleEmoji);
for(const button of document.querySelectorAll('[data-gc-ui="emoji-insert"]')){
/** 将表情写入输入框，沿用官方核心处理输入和发送。 */
function insertEmoji(){const input=document.getElementById('gc-text');if(!input||input.disabled)return;const value=button.getAttribute('data-emoji')||'';if(input.value.length+value.length>input.maxLength)return;input.setRangeText(value,input.selectionStart,input.selectionEnd,'end');input.dispatchEvent(new Event('input',{bubbles:true}));input.focus();if(emojiMenu)emojiMenu.hidden=true;emojiToggle?.setAttribute('aria-expanded','false')}
button.addEventListener('click',insertEmoji)}
const intro=document.querySelector('[data-gc-intro]');
if(!intro)return;
/** 关闭页面开场；不等待或伪造客服连接状态。 */
function finishIntro(){intro.hidden=true}
if(document.body.dataset.gcEditor==='true'||new URLSearchParams(window.location.search).get('editor')==='1'||window.matchMedia('(prefers-reduced-motion:reduce)').matches){finishIntro();return}
const meter=intro.querySelector('progress'),label=intro.querySelector('[data-intro-label]');
if(meter)meter.value=65;
/** 本地页面资源就绪后完成进度，最多停留一千八百毫秒。 */
function pageReady(){if(meter)meter.value=100;if(label)label.textContent='页面已就绪';setTimeout(finishIntro,160)}
if(document.readyState==='complete')pageReady();else window.addEventListener('load',pageReady,{once:true});
setTimeout(finishIntro,1800);
})();`
