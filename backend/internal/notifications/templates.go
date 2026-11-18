// Package notifications 提供产品邮件模板、九语言与个人时区调度，所有按钮目标由服务端控制。
package notifications

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"regexp"
	"strings"
	"time"

	"supportdesk/backend/internal/localization"
)

// Definition 定义受控业务场景、默认文案与固定站内入口。
type Definition struct {
	Kind, Subject, Body, Button, Path string
	Urgent                            bool
}

// Mail 保存已经安全渲染的邮件，认证邮件不得持久化其正文。
type Mail struct {
	Subject, Body, HTML string
	UnsubscribeURL      string
	Version             int
}

// Definitions 是可管理模板白名单，金额和期限从业务数据生成。
var Definitions = []Definition{
	{"admin_invitation", "GoodChat 管理员开通邀请", "平台已为您开通「{tenant}」。请验证受邀邮箱并设置密码，邀请7天有效。此邮件不包含初始密码；首次接受初始管理员邀请会成为本租户所有者。", "接受邀请", "/accept-invitation", true},
	{"lead", "GoodChat 新的待处理留言", "收到新的待处理留言，编号：{reference}。邮件不包含客户联系方式、聊天正文或附件。", "查看留言", "/console/automation", false},
	{"code_register", "GoodChat 注册验证码", "您的注册验证码为：{code}\n验证码5分钟内有效，仅限本次用途。重新申请后旧码失效。请勿转告他人。", "", "", true},
	{"code_login", "GoodChat 登录验证码", "您的登录验证码为：{code}\n验证码5分钟内有效，仅限本次用途。重新申请后旧码失效。请勿转告他人。", "", "", true},
	{"code_reset_password", "GoodChat 重置密码验证码", "您的重置密码验证码为：{code}\n验证码5分钟内有效，仅限本次用途。重新申请后旧码失效。请勿转告他人。", "", "", true},
	{"code_invitation", "GoodChat 接受邀请验证码", "您的接受邀请验证码为：{code}\n验证码5分钟内有效，仅限本次用途。重新申请后旧码失效。请勿转告他人。", "", "", true},
	{"code_ownership", "GoodChat 所有者移交验证码", "您的所有者移交验证码为：{code}\n验证码5分钟内有效，仅限本次用途。重新申请后旧码失效。请勿转告他人。", "", "", true},
	{"invitation", "GoodChat 团队邀请", "您受邀加入「{tenant}」。请验证受邀邮箱后加入。邀请7天有效，不包含初始密码。", "接受邀请", "/accept-invitation", true},
	{"welcome", "欢迎使用 GoodChat", "您的工作空间已准备就绪。可以接入网站、建立知识库并配置机器人。系统及2个基础坐席免费，AI调用按实际Token计费。", "开始配置", "/console/ai", false},
	{"password_changed", "GoodChat 密码已修改", "您的账号密码已修改，已有登录会话已撤销。如非本人操作，请立即重置密码。", "检查账号", "/reset-password", true},
	{"permissions_changed", "GoodChat 团队权限变更", "您在「{tenant}」的团队权限已变更。请登录核对，如有疑问请联系企业管理员。", "查看账号", "/console/settings", true},
	{"balance_low", "GoodChat AI余额提醒", "「{tenant}」AI余额较低，当前余额：{balance}。AI调用按实际Token计费，请及时补充余额。", "查看AI账单", "/console/ai", false},
	{"balance_critical", "GoodChat AI余额即将不足", "「{tenant}」AI余额即将不足，当前余额：{balance}。请及时补充余额，避免影响自动回复。", "查看AI账单", "/console/ai", true},
	{"ai_blocked", "GoodChat AI回复受阻", "「{tenant}」有AI任务因余额或每日预算不足而无法执行。请检查余额、机器人预算并安排人工接待。", "检查机器人", "/console/ai", true},
	{"topup", "GoodChat AI充值已入账", "「{tenant}」已入账：{amount}。交易参考：{reference}。请登录核对账单。", "查看AI账单", "/console/ai", false},
	{"subscription_active", "GoodChat 套餐已开通", "「{tenant}」套餐已开通，有效期至：{expires_at}。具体权益以控制台为准。", "查看套餐", "/console/billing", false},
	{"subscription_expiring", "GoodChat 套餐到期提醒", "「{tenant}」付费套餐将于 {expires_at} 到期。续费后将停止本次到期提醒。", "查看套餐", "/console/billing", false},
	{"subscription_expired", "GoodChat 套餐已到期", "「{tenant}」付费套餐已于 {expires_at} 到期。请查看当前有效权益；历史资料不会因此删除。", "查看套餐", "/console/billing", false},
	{"handoff_waiting", "GoodChat 有待处理的转人工咨询", "「{tenant}」有转人工咨询尚未收到人工回复。咨询编号：{reference}。请及时安排接待。", "打开工作台", "/workspace", false},
	{"ai_errors", "GoodChat AI服务异常提醒", "「{tenant}」近期AI任务持续异常。请检查机器人和模型服务，并安排人工接待。邮件不包含客户聊天内容。", "检查AI服务", "/console/ai", true},
	{"digest", "GoodChat 每周服务提醒", "「{tenant}」最近7天服务摘要：咨询 {sessions} 次，AI 用量 {tokens} Token。统计范围与您的权限一致。", "查看工作空间", "/console", false},
	{"onboarding", "GoodChat 接入配置提醒", "「{tenant}」尚未完成网站接入。可以创建客服链接，准备知识库并测试第一条咨询。", "继续配置", "/console/links", false},
	{"product_update", "GoodChat 产品更新", "GoodChat 已发布产品更新：{reference}。请登录查看相关功能。", "查看工作空间", "/console", false},
	{"test", "GoodChat 邮件发送测试", "这是一封由平台管理员主动发送的测试邮件。语言：{name}；时区：{reference}。", "打开GoodChat", "/console", true},
}
var variable = regexp.MustCompile(`\{([a-z_]+)\}`)
var allowed = map[string]bool{"tenant": true, "name": true, "code": true, "expires_at": true, "amount": true, "balance": true, "reference": true, "sessions": true, "tokens": true, "occurred_at": true}

// Find 仅返回已注册场景，拒绝任意发信模板代码。
func Find(kind string) (Definition, bool) {
	for _, d := range Definitions {
		if d.Kind == kind {
			return d, true
		}
	}
	return Definition{}, false
}

// Validate 检查模板长度、邮件头与允许变量，阻止模板修改业务链接及HTML执行。
func Validate(subject, body, button string) error {
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(body) == "" || len([]rune(subject)) > 120 || len([]rune(body)) > 6000 || len([]rune(button)) > 100 || strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("邮件模板内容无效")
	}
	for _, s := range []string{subject, body, button} {
		for _, m := range variable.FindAllStringSubmatch(s, -1) {
			if !allowed[m[1]] {
				return fmt.Errorf("邮件模板变量无效")
			}
		}
		if strings.ContainsAny(variable.ReplaceAllString(s, ""), "{}") {
			return fmt.Errorf("邮件模板变量无效")
		}
	}
	return nil
}

// Render 使用普通文本变量和HTML自动转义渲染品牌邮件，按钮地址仅允许本站受控路径。
func Render(origin, locale, zone string, d Definition, params map[string]string, subject, body, button string, version int) (Mail, error) {
	if err := ValidateFor(d, subject, body, button); err != nil {
		return Mail{}, err
	}
	if params["occurred_at"] != "" {
		body += "\n\n" + localization.Format(locale, "操作时间：{occurred_at}")
	}

	substitute := func(s string) string {
		return variable.ReplaceAllStringFunc(s, func(k string) string { return params[k[1:len(k)-1]] })
	}
	subject, body, button = substitute(subject), substitute(body), substitute(button)
	if strings.ContainsAny(subject, "\r\n") {
		return Mail{}, fmt.Errorf("邮件主题无效")
	}
	if !localization.ValidTimezone(zone) {
		zone = "UTC"
	}
	if localization.Normalize(locale) == "" {
		locale = "en"
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return Mail{}, fmt.Errorf("邮件站点地址无效")
	}
	u.Path = d.Path
	u.RawQuery = ""
	u.Fragment = ""
	q := u.Query()
	q.Set("lang", locale)
	q.Set("timezone", zone)
	if d.Path == "/platform" {
		q.Set("tab", "customer-ai")
	}
	if d.Kind == "invitation" || d.Kind == "admin_invitation" {
		if params["token"] == "" {
			return Mail{}, fmt.Errorf("邀请凭证缺失")
		}
		q.Set("token", params["token"])
	}
	u.RawQuery = q.Encode()
	link := ""
	if d.Path != "" {
		link = u.String()
		body += "\n\n" + button + "：" + link
	}
	footer := localization.Format(locale, "如果不是您本人操作，请忽略本邮件或联系企业管理员。")
	if params["_unsubscribe_url"] != "" {
		body += "\n\n" + localization.Format(locale, "取消此类邮件订阅") + "：" + params["_unsubscribe_url"]
	}
	data := map[string]string{"Unsubscribe": params["_unsubscribe_url"], "UnsubscribeLabel": localization.Format(locale, "取消此类邮件订阅"), "Subject": subject, "Body": body, "Button": button, "Link": link, "Footer": footer, "Lang": locale}
	var rich bytes.Buffer
	if err := frame.Execute(&rich, data); err != nil {
		return Mail{}, err
	}
	return Mail{Subject: subject, Body: body + "\n\n" + footer, HTML: rich.String(), Version: version, UnsubscribeURL: params["_unsubscribe_header"]}, nil
}

var frame = template.Must(template.New("mail").Parse(`<!doctype html><html lang="{{.Lang}}"><body style="margin:0;background:#f4f7fb;color:#182338;font-family:Arial,sans-serif"><table role="presentation" width="100%"><tr><td align="center" style="padding:28px 12px"><table role="presentation" width="100%" style="max-width:600px;background:#fff;border:1px solid #dde5ef;border-radius:12px"><tr><td style="padding:28px"><div style="font-size:24px;font-weight:bold;color:#1455ff">GoodChat</div><h1 style="font-size:21px;line-height:1.5">{{.Subject}}</h1><div style="white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.8">{{.Body}}</div>{{if .Link}}<p style="margin:28px 0"><a href="{{.Link}}" style="display:inline-block;padding:13px 22px;background:#1455ff;color:#fff;text-decoration:none;border-radius:6px">{{.Button}}</a></p>{{end}}<hr style="border:0;border-top:1px solid #e3e8f0"><p style="font-size:12px;color:#526176;line-height:1.7">{{.Footer}}</p>{{if .Unsubscribe}}<p><a href="{{.Unsubscribe}}">{{.UnsubscribeLabel}}</a></p>{{end}}</td></tr></table></td></tr></table></body></html>`))

// LocalTime 显示明确的收件人本地时间及IANA时区，不依赖服务器时区。
func LocalTime(at time.Time, zone string) string {
	l, e := time.LoadLocation(zone)
	if e != nil {
		l = time.UTC
		zone = "UTC"
	}
	return at.In(l).Format("2006-01-02 15:04 MST") + " (" + zone + ")"
}

// NextAllowed 按本地静默时间逐分钟寻找下次窗口，逐绝对时刻迭代以正确跨越夏令时。
func NextAllowed(at time.Time, zone string, start, end int) time.Time {
	l, e := time.LoadLocation(zone)
	if e != nil {
		l = time.UTC
	}
	for i := 0; i < 2880; i++ {
		h := at.In(l).Hour()
		quiet := start < end && h >= start && h < end || start > end && (h >= start || h < end)
		if !quiet {
			return at
		}
		at = at.Truncate(time.Minute).Add(time.Minute)
	}
	return at
}

// ValidateFor 限制每类邮件可用变量，金额、到期时间和验证码不能从模板中移除。
func ValidateFor(d Definition, subject, body, button string) error {
	if e := Validate(subject, body, button); e != nil {
		return e
	}
	permitted := map[string]bool{"name": true, "occurred_at": true}
	for _, m := range variable.FindAllStringSubmatch(d.Subject+d.Body+d.Button, -1) {
		permitted[m[1]] = true
	}
	for _, m := range variable.FindAllStringSubmatch(subject+body+button, -1) {
		if !permitted[m[1]] {
			return fmt.Errorf("该模板使用了不支持的变量")
		}
	}
	for _, name := range []string{"code", "amount", "balance", "expires_at"} {
		if strings.Contains(d.Body, "{"+name+"}") && !strings.Contains(subject+body, "{"+name+"}") {
			return fmt.Errorf("请保留邮件中的关键业务变量")
		}
	}
	return nil
}
