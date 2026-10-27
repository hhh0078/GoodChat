// 文件作用说明：从环境与任务内开发数据库配置读取Go服务设置，不内置其他项目密钥。
package platform

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LoadConfig 读取环境配置，复用现有Docker数据库与Redis。
func LoadConfig() (Config, error) {
	config := Config{Address: env("SUPPORT_ADDR", "127.0.0.1:18080"), DatabaseURL: os.Getenv("DATABASE_URL"), MigrationURL: os.Getenv("MIGRATION_DATABASE_URL"), PublicURL: env("SUPPORT_PUBLIC_URL", "http://127.0.0.1:13000"), DataDir: env("SUPPORT_DATA_DIR", "../.local"), Production: os.Getenv("APP_ENV") == "production", SMTPHost: os.Getenv("SUPPORT_SMTP_HOST"), SMTPPort: env("SUPPORT_SMTP_PORT", "587"), SMTPUser: os.Getenv("SUPPORT_SMTP_USERNAME"), SMTPPassword: os.Getenv("SUPPORT_SMTP_PASSWORD"), SMTPFrom: os.Getenv("SUPPORT_SMTP_FROM"), SMTPAllowPlain: os.Getenv("SUPPORT_SMTP_ALLOW_PLAIN") == "true", AIEndpoint: os.Getenv("SUPPORT_AI_ENDPOINT"), AIKey: os.Getenv("SUPPORT_AI_KEY"), AIModel: os.Getenv("SUPPORT_AI_MODEL"), CodeOrigin: env("SUPPORT_CODE_ORIGIN", "http://localhost:18081"), AuthSecret: os.Getenv("SUPPORT_AUTH_SECRET")}
	var err error
	config.AIDailyLimit, err = aiDailyLimit(env("SUPPORT_AI_DAILY_LIMIT", "20"))
	if err != nil {
		return config, err
	}
	if config.DatabaseURL == "" && !config.Production {
		body, err := os.ReadFile(filepath.Join(config.DataDir, "dev-database.json"))
		if err == nil {
			var local struct {
				DatabaseURL  string `json:"database_url"`
				MigrationURL string `json:"migration_url"`
			}
			if json.Unmarshal(body, &local) == nil {
				config.DatabaseURL = local.DatabaseURL
				config.MigrationURL = local.MigrationURL
			}
		}
	}
	if config.SMTPHost == "" {
		body, err := os.ReadFile(filepath.Join(config.DataDir, "smtp.json"))
		if err == nil {
			var smtp struct {
				Host     string `json:"host"`
				Port     string `json:"port"`
				Username string `json:"username"`
				Password string `json:"password"`
				From     string `json:"from"`
			}
			if json.Unmarshal(body, &smtp) == nil {
				config.SMTPHost = smtp.Host
				if smtp.Port != "" {
					config.SMTPPort = smtp.Port
				}
				config.SMTPUser = smtp.Username
				config.SMTPPassword = smtp.Password
				config.SMTPFrom = smtp.From
			}
		}
	}
	if config.DatabaseURL == "" {
		return config, errors.New("请配置DATABASE_URL或现有Docker数据库的本地私有连接")
	}
	config.CallbackAllowLocal = !config.Production && os.Getenv("SUPPORT_CALLBACK_ALLOW_LOCAL") == "true"
	config.RedisURL = env("SUPPORT_REDIS_URL", "redis://127.0.0.1:56379/0")
	if config.AuthSecret == "" && !config.Production {
		if err := os.MkdirAll(config.DataDir, 0700); err != nil {
			return config, err
		}
		secretPath := filepath.Join(config.DataDir, "auth-secret")
		body, err := os.ReadFile(secretPath)
		if os.IsNotExist(err) {
			secret, tokenErr := Token()
			if tokenErr != nil {
				return config, tokenErr
			}
			body = []byte(secret)
			if err = os.WriteFile(secretPath, body, 0600); err != nil {
				return config, err
			}
		} else if err != nil {
			return config, err
		}
		config.AuthSecret = string(body)
	}
	if config.Production && (len(config.AuthSecret) < 32 || !strings.HasPrefix(config.PublicURL, "https://")) {
		return config, errors.New("生产环境需要至少32字节SUPPORT_AUTH_SECRET和HTTPS官网地址")
	}
	config.GatewaySecret = env("SUPPORT_GATEWAY_SECRET", config.AuthSecret)
	return config, nil
}

// aiDailyLimit 校验每日提交上限，零明确禁用AI提交，不将非法值退化为无限额度。
func aiDailyLimit(value string) (int, error) {
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 || limit > 100000 {
		return 0, errors.New("SUPPORT_AI_DAILY_LIMIT需要为0至100000的整数，0表示禁用AI提交")
	}
	return limit, nil
}

// env 返回去除空白后的环境值，空值使用给定默认值。
func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
