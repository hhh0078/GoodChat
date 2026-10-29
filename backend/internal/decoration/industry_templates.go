// 文件作用说明：提供六个行业的官方静态访客模板，以同一固定SDK接入聊天并物化为租户私有资源包。
package decoration

import (
	_ "embed"
	"fmt"
	"html"
	"strings"
)

// IndustryTemplate 只暴露官方素材目录，不引用任何租户的私有资源包编号。
type IndustryTemplate struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Industry    string `json:"industry"`
	Description string `json:"description"`
}

// industryTemplateStyle 保存各行业的品牌语气、版式与预先登记的普通咨询入口。
type industryTemplateStyle struct {
	IndustryTemplate
	Kicker, Title, Subtitle, Notice, Accent, Background, Badge, Decoration string
	Questions                                                              []string
}

// industryBaseCSS 为无需构建的行业模板提供响应布局和完整固定聊天节点样式。
//
//go:embed industry_base.css
var industryBaseCSS string

// industryTemplateStyles 返回具有行业辨识度的本地官方素材，内容不包含审批或资质承诺。
func industryTemplateStyles() []industryTemplateStyle {
	return []industryTemplateStyle{
		{IndustryTemplate{"finance", "金融咨询 · 稳健蓝金", "金融", "咨询顾问、反诈提醒和方案沟通，蓝金配色与清晰风险提示。"}, "FINANCIAL CONSULTING", "每一步，都有专业回应", "金融服务在线咨询 · 为您解答产品与办理流程", "反诈提醒：请通过官方渠道核实身份，切勿向陌生账户转账或提供验证码。", "#234e70", "#edf3f8", "专属顾问 · 信息保护", `<div class="finance-scene"><div class="finance-orbit"></div><div class="finance-seal"><small>PRIVATE SERVICE</small><span>专属服务</span><strong>安心咨询</strong><i>✓</i></div><span class="art-caption">每一份托付 · 认真回应</span></div>`, []string{"了解产品方案", "咨询办理流程", "联系服务顾问"}},
		{IndustryTemplate{"commerce", "电商售后 · 奶油橙", "电商", "围绕订单、物流和售后组织入口，让客户快速找到购物帮助。"}, "CUSTOMER CARE", "好物到家，服务也到家", "从下单到签收，我们都在这里", "温馨提示：查询订单时请提供订单编号，请勿发送支付密码或验证码。", "#a44722", "#fff6eb", "订单 · 配送 · 售后", `<div class="commerce-scene"><div class="commerce-parcel"><i class="parcel-tape"></i><span>GOOD<br>THINGS</span><small class="parcel-label">HANDLE WITH CARE<br>用心送达每一份期待</small></div><span class="parcel-tag">售后也有好服务 ↗</span></div>`, []string{"查询订单物流", "咨询退换货", "了解商品详情"}},
		{IndustryTemplate{"education", "教育咨询 · 书页绿", "教育", "课程选择、学习计划和试听咨询，柔和纸张与知识卡片布局。"}, "LEARNING STARTS HERE", "找到适合你的成长节奏", "课程咨询、学习规划与报名帮助", "咨询提示：可以先说明学习阶段与目标，我们会为您介绍可选课程和服务。", "#386249", "#f2f4e9", "了解目标 / 匹配课程 / 开始学习", `<div class="education-scene"><div class="education-page"><span>Aa</span><i></i><i></i><i></i></div><div class="education-books"><b>LEARN</b><b>EXPLORE</b><b>GROW</b></div><small>每一次提问，都是新的开始</small></div>`, []string{"了解课程安排", "咨询试听体验", "获取学习规划"}},
		{IndustryTemplate{"medical", "医疗服务 · 清透青", "医疗", "预约、科室和就诊流程导航，明确区分服务咨询与医疗诊断。"}, "CARE WITH YOU", "让每一次就诊，都更从容", "预约帮助 · 科室指引 · 就诊流程", "服务说明：在线客服提供预约与流程咨询，不能替代医生诊断；紧急情况请及时线下就医。", "#246b68", "#eef7f6", "预约指引 · 温暖服务", `<div class="medical-scene"><div class="medical-calendar"><small>CARE WITH YOU</small><strong>就诊服务</strong><div><i></i><i></i><i></i><i></i><i></i><i></i></div><span>让服务多一点温度</span></div><div class="medical-cross"><span>＋</span></div></div>`, []string{"了解预约流程", "咨询科室服务", "查看就诊准备"}},
		{IndustryTemplate{"property", "房产顾问 · 岩砂棕", "房产", "围绕区域、户型和看房安排提供咨询入口，呈现建筑空间感。"}, "FIND YOUR PLACE", "关于家的期待，慢慢聊", "项目咨询、户型介绍与看房服务", "咨询提示：展示信息仅供沟通参考，具体房源、价格与交易条件以实际确认资料为准。", "#795941", "#f5f0e9", "空间 / 生活 / 归属", `<div class="property-scene"><div class="property-outline"></div><div class="property-building"><i></i><i></i><i></i><i></i><i></i><i></i></div><span class="art-caption">A PLACE TO CALL HOME</span></div>`, []string{"了解在售项目", "咨询户型空间", "预约看房沟通"}},
		{IndustryTemplate{"business", "企业服务 · 石墨黑", "企业服务", "以项目目标、交付流程和合作沟通为主线，简洁的企业服务工作室风格。"}, "LET’S BUILD TOGETHER", "把业务问题，变成下一步", "解决方案咨询 · 项目需求 · 合作沟通", "服务提示：请简要描述业务场景与目标，客服将协助您对接相应服务。", "#37434b", "#f1f3f4", "需求梳理 → 方案沟通 → 服务交付", `<div class="business-scene"><span class="business-caption">FROM IDEA TO IMPACT</span><div class="business-grid"><b><small>01</small>IDEA</b><b><small>02</small>PLAN</b><b><small>03</small>BUILD</b><b><small>04</small>GROW</b></div></div>`, []string{"咨询解决方案", "沟通项目需求", "了解合作流程"}},
	}
}

// industryTemplateCatalog 返回可复制的公共目录，调用方按租户身份生成全新私有包。
func industryTemplateCatalog() []IndustryTemplate {
	items := []IndustryTemplate{}
	for _, item := range industryTemplateStyles() {
		items = append(items, item.IndustryTemplate)
	}
	return items
}

// templateChatMarkup 保留唯一固定聊天节点，所有消息和动作由chat-ui.js实现。
func templateChatMarkup() string {
	return `<section class="gc-conversation" aria-label="在线咨询"><div class="gc-chat-heading"><strong>在线咨询</strong><span id="gc-status" role="status">正在连接咨询服务…</span><button type="button" data-dialog-open="gc-service-notice" class="gc-service-button">服务说明</button></div><div id="gc-messages" role="log" aria-label="聊天消息" aria-live="polite"></div><div class="gc-composer-area"><p id="gc-typing" role="status" aria-live="polite"></p><div id="gc-reply-bar" hidden><span id="gc-reply-text"></span><button id="gc-reply-cancel" type="button">取消引用</button></div><div id="gc-compose"><div class="gc-capsule"><button id="gc-add" type="button" aria-label="添加附件" aria-expanded="false">＋</button><textarea id="gc-text" maxlength="10000" rows="1" placeholder="输入您想咨询的内容" aria-label="消息内容"></textarea><button id="gc-send" type="button" aria-label="发送消息">发送</button></div><div id="gc-attachment-menu" hidden><button type="button" data-kind="image">图片</button><button type="button" data-kind="file">文件</button><button type="button" data-kind="video">视频</button></div><input id="gc-file" type="file" aria-label="选择附件" hidden></div><p id="gc-error" role="alert"></p></div></section>`
}

// templateScriptsMarkup 按顺序引用平台固定脚本，不携带租户身份和核心脚本字节。
func templateScriptsMarkup(custom bool) string {
	value := `<script src="static/goodchat/sdk.js"></script><script src="static/goodchat/chat-ui.js"></script>`
	if custom {
		value += `<script src="static/custom.js"></script>`
	}
	return value
}

// industryTemplatePackage 将官方行业素材转换成经过严格校验的新静态包。
func industryTemplatePackage(key string, scripts TemplateScripts) (*staticPackage, error) {
	var chosen *industryTemplateStyle
	for _, item := range industryTemplateStyles() {
		if item.Key == key {
			value := item
			chosen = &value
			break
		}
	}
	if chosen == nil {
		return nil, invalidDesign("未找到该行业的官方访客模板")
	}
	var actions strings.Builder
	label := chosen.Industry
	if !strings.HasSuffix(label, "服务") {
		label += "服务"
	}
	for i, question := range chosen.Questions {
		fmt.Fprintf(&actions, `<button type="button" data-gc-send="%s"><small>0%d</small><span>%s</span><b aria-hidden="true">↗</b></button>`, html.EscapeString(question), i+1, html.EscapeString(question))
	}
	markup := fmt.Sprintf(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><title>%s</title><link rel="stylesheet" href="static/style.css"></head><body><main class="gc-template gc-industry-%s"><header class="gc-hero"><div class="gc-hero-copy"><p class="gc-eyebrow">%s</p><p class="gc-industry-label">%s</p><h1>%s</h1><p class="gc-subtitle">%s</p><span class="gc-hero-badge">%s</span></div><div class="gc-hero-art" aria-hidden="true">%s</div></header><nav class="gc-service-entries" aria-label="快捷咨询">%s</nav><p class="gc-service-notice">%s</p>%s<footer class="gc-footer">GoodChat 提供技术支持</footer></main><dialog id="gc-service-notice" class="gc-dialog"><h2>服务说明</h2><p>%s</p><p>您可以在此发送咨询消息和附件。服务入口只发送普通咨询文字，具体业务将由客服与您确认。</p><button type="button" data-dialog-close>我知道了</button></dialog>%s</body></html>`, html.EscapeString(chosen.Name), chosen.Key, chosen.Kicker, label, html.EscapeString(chosen.Title), html.EscapeString(chosen.Subtitle), html.EscapeString(chosen.Badge), chosen.Decoration, actions.String(), html.EscapeString(chosen.Notice), templateChatMarkup(), html.EscapeString(chosen.Notice), templateScriptsMarkup(true))
	css := fmt.Sprintf("/* 文件作用说明：定义%s静态访客模板及固定聊天控件的响应样式。 */\n:root{--gc-primary:%s;--gc-background:%s}\n", chosen.Name, chosen.Accent, chosen.Background) + industryBaseCSS
	return buildTemplatePackage("index.html", []TemplateFile{{Path: "index.html", Content: markup}, {Path: "static/style.css", Content: css}, {Path: "static/custom.js", Content: industryDialogScript}}, scripts)
}

// industryDialogScript 只控制当前HTML的本地说明弹框，不包含聊天或业务请求。
const industryDialogScript = `/** 文件作用说明：仅控制行业模板的本地服务说明弹框。 */
(function(){'use strict';for(const button of document.querySelectorAll('[data-dialog-open]')){/** 打开当前模板内的说明弹框。 */function openDialog(){const dialog=document.getElementById(button.getAttribute('data-dialog-open'));if(dialog&&typeof dialog.showModal==='function')dialog.showModal()}button.addEventListener('click',openDialog)}for(const button of document.querySelectorAll('[data-dialog-close]')){/** 关闭说明弹框并由浏览器恢复焦点。 */function closeDialog(){button.closest('dialog')?.close()}button.addEventListener('click',closeDialog)}})();`
