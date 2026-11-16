// 文件作用说明：验证AI每日提交限制配置，防止非法或零值变成无上限。
package platform

import "testing"

// TestRenderOriginDefaults 验证生产默认当前域名，本机Next开发仍保持原有渲染入口。
func TestRenderOriginDefaults(t *testing.T) {
	for _, production := range []bool{false, true} {
		t.Run(map[bool]string{false: "development", true: "production"}[production], func(t *testing.T) {
			t.Setenv("APP_ENV", map[bool]string{false: "development", true: "production"}[production])
			t.Setenv("DATABASE_URL", "postgres://localhost/unopened-test-database")
			t.Setenv("SUPPORT_DATA_DIR", t.TempDir())
			t.Setenv("SUPPORT_PUBLIC_URL", "https://goodchat.58it.cn")
			t.Setenv("SUPPORT_CODE_ORIGIN", "")
			t.Setenv("SUPPORT_AUTH_SECRET", "unit-test-config-secret-with-32-bytes")
			t.Setenv("SUPPORT_AI_DAILY_LIMIT", "20")
			cfg, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			want := "http://localhost:18081"
			if production {
				want = "https://goodchat.58it.cn"
			}
			if cfg.CodeOrigin != want {
				t.Fatalf("渲染默认地址错误: %s", cfg.CodeOrigin)
			}
		})
	}
}

// TestAIDailyLimitParsing 验证允许的非负整数和明确拒绝的非法配置。
func TestAIDailyLimitParsing(t *testing.T) {
	for _, value := range []string{"0", "1", "20", "100000"} {
		if _, err := aiDailyLimit(value); err != nil {
			t.Errorf("合法上限%s被拒绝", value)
		}
	}
	for _, value := range []string{"-1", "unlimited", "1.5", "100001", ""} {
		if _, err := aiDailyLimit(value); err == nil {
			t.Errorf("非法上限%s被接受", value)
		}
	}
	t.Setenv("SUPPORT_AI_DAILY_LIMIT", "")
	limit, err := aiDailyLimit(env("SUPPORT_AI_DAILY_LIMIT", "20"))
	if err != nil || limit != 20 {
		t.Fatal("未配置时每日提交次数应默认20")
	}
}
