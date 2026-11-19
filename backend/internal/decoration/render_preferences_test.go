// 文件作用说明：覆盖行业模板跳转附带语言时区后的真实资源读取，仍拒绝非法或额外参数。
package decoration

import (
	"net/http/httptest"
	"strings"
	"testing"

	"supportdesk/backend/internal/platform"
)

// TestRenderPreferences 验证静态资源只接受单值且有效的公开偏好。
func TestRenderPreferences(t *testing.T) {
	for _, test := range []struct {
		query string
		valid bool
	}{
		{"", true}, {"lang=zh-CN&timezone=Asia%2FShanghai", true}, {"lang=en", true},
		{"lang=invalid", false}, {"timezone=invalid", false}, {"lang=en&lang=zh-CN", false},
		{"token=other", false}, {"lang=%XX", false}, {"lang=en;other=value", false},
	} {
		r := httptest.NewRequest("GET", "/preview-assets/test?"+test.query, nil)
		if validRenderPreferences(r) != test.valid {
			t.Fatalf("偏好校验异常: %s", test.query)
		}
	}
}

// TestPostgresIndustryLocalizedPreviews 验证全部行业模板原样跟随入口重定向，不删除语言参数来绕过问题。
func TestPostgresIndustryLocalizedPreviews(t *testing.T) {
	f := newIntegrationFixture(t, platform.Config{})
	for _, item := range industryTemplateCatalog() {
		preview := f.mustRequest("POST", "/tenant/template-industries/"+item.Key+"/preview", f.member, nil)
		entry := f.packageEntryURL(preview["preview_url"].(string) + "&lang=zh-CN&timezone=Asia%2FShanghai")
		response := f.renderRequest(entry)
		if response.Code != 200 || !strings.Contains(response.Body.String(), "<html") {
			t.Fatalf("%s 预览失败: %d", item.Key, response.Code)
		}
		if f.renderRequest(entry+"&unexpected=1").Code != 404 {
			t.Fatal("不能放宽资源查询白名单")
		}
	}
}
