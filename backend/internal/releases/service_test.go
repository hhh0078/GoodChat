// 文件作用说明：验证强制更新的数字版本边界与下载链接安全规则。
package releases

import "testing"

// TestRequired 验证数字排序、相同版本与最低基准，防止安装后无限强制更新。
func TestRequired(t *testing.T) {
	for _, item := range []struct {
		latest, current string
		want            bool
	}{{"0.0.1", "0.0.0", false}, {"0.0.2", "0.0.1", true}, {"0.1.0", "0.1.0", false}, {"0.1.10", "0.1.9", true}, {"1.0.0", "2.0.0", false}} {
		got, err := Required(item.latest, item.current)
		if err != nil || got != item.want {
			t.Fatalf("%+v got=%v err=%v", item, got, err)
		}
	}
	for _, v := range []string{"1.2", "1.2.3-beta", "01.2.3", "65536.1.0"} {
		if _, err := VersionParts(v); err == nil {
			t.Fatal("接受非法版本", v)
		}
	}
}

// TestDownloadURL 验证下载不能引入本地协议或认证信息。
func TestDownloadURL(t *testing.T) {
	for _, v := range []string{"http://example.com/a", "file:///tmp/a", "https://user:pw@example.com/a", "javascript:alert(1)"} {
		if validURL(v) {
			t.Fatal("接受不安全下载链接", v)
		}
	}
	if !validURL("") || !validURL("https://example.com/a.zip") {
		t.Fatal("合法链接被拒绝")
	}
}
