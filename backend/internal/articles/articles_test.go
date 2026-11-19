// 文件作用说明：验证Markdown安全、标题去重、调度时间与搜索凭据用途隔离等文章边界。
package articles

import (
	"strings"
	"supportdesk/backend/internal/platform"
	"testing"
	"time"
)

// TestMarkdownSafety 验证脚本和危险链接不会成为可执行HTML，表格和代码仍可阅读。
func TestMarkdownSafety(t *testing.T) {
	html, e := renderMarkdown("# 标题\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1))\n\n|A|B|\n|-|-|\n|1|2|\n\n```js\nalert(1)\n```")
	if e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"<script>", "href=\"javascript:", "<h1>"} {
		if strings.Contains(html, bad) {
			t.Fatal(html)
		}
	}
	for _, good := range []string{"<h2>", "<table>", "<code class=\"language-js\""} {
		if !strings.Contains(html, good) {
			t.Fatal(html)
		}
	}
}

// TestTitleAndSchedule 验证标题规范化及北京时间严格未来边界。
func TestTitleAndSchedule(t *testing.T) {
	if !similarTitle("ＡＩ 客服，怎么用？", "ai客服怎么用") || similarTitle("电商售后退换货流程", "学校招生咨询配置") {
		t.Fatal("标题比较失败")
	}
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	next, e := nextDaily(now, "18:00", "Asia/Shanghai")
	if e != nil || next.Sub(now) != 24*time.Hour {
		t.Fatal(next, e)
	}
	next, e = nextDaily(now.Add(-time.Second), "18:00", "Asia/Shanghai")
	if e != nil || next.Sub(now) != 0 {
		t.Fatal(next, e)
	}
	if _, e = nextDaily(now, "25:00", "Asia/Shanghai"); e == nil {
		t.Fatal("无效时刻")
	}
	if _, e = languages([]string{"zh", "zz"}); e == nil {
		t.Fatal("不应接受非规范语言")
	}
}

// TestSearchSecret 验证读取凭据不串用途、不接受篡改且只接受当前站点。
func TestSearchSecret(t *testing.T) {
	s := New(nil, nil, platform.Config{AuthSecret: strings.Repeat("s", 40), PublicURL: "https://goodchat.58it.cn"})
	cipher, e := s.secret("private-token", "baidu", false)
	if e != nil || strings.Contains(cipher, "private-token") {
		t.Fatal(e)
	}
	if value, e := s.secret(cipher, "baidu", true); e != nil || value != "private-token" {
		t.Fatal(e)
	}
	if _, e = s.secret(cipher, "google", true); e == nil {
		t.Fatal("用途不应互换")
	}
	for _, v := range []string{"https://evil.com/", "sc-domain:evil.com", "https://goodchat.58it.cn/?token=x"} {
		if _, e = s.googleProperty(v); e == nil {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"https://goodchat.58it.cn/", "sc-domain:58it.cn"} {
		if _, e = s.googleProperty(v); e != nil {
			t.Fatal(e)
		}
	}
}

// TestGeneratedProductFacts 阻止常见但未实现的行业功能被自动写成当前产品能力。
func TestGeneratedProductFacts(t *testing.T) {
	for _, body := range []string{"可接入微信公众号统一接待", "调整置信度阈值", "随后自动生成工单", "轻量免费模型处理", "只有当用户发送消息才产生费用", "Use a confidence threshold"} {
		if validateGeneratedFacts(Draft{Markdown: body}) == nil {
			t.Fatal(body)
		}
	}
	if e := validateGeneratedFacts(Draft{Title: "教育机构如何准备课程咨询知识库", Markdown: "在接入管理中创建链接，然后到分享接入的嵌入中取得代码。基础版含2个坐席，AI按实际用量计费。"}); e != nil {
		t.Fatal(e)
	}
}
