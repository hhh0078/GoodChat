// 文件作用说明：运行显式限定本机的实时压测夹具和负载命令，不打印数据库、Cookie、票据或访客凭证。
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type options struct {
	stages                                     string
	rate                                       int
	mode, config, fixture, output, base, label string
	agents, visitors                           int
}

// main 限制生成器资源并输出固定错误类型，异常不包含连接串或身份凭证。
func main() {
	runtime.GOMAXPROCS(2)
	debug.SetMemoryLimit(384 << 20)
	o := options{}
	flag.StringVar(&o.mode, "mode", "fixture", "fixture或cleanup或run")
	flag.StringVar(&o.config, "config", ".local/dev-database.json", "本机私有数据库配置")
	flag.StringVar(&o.fixture, "fixture", ".local/evidence/realtime-resilience/load/fixture.json", "私有夹具")
	flag.StringVar(&o.output, "output", ".local/evidence/realtime-resilience/load", "脱敏证据目录")
	flag.StringVar(&o.base, "base", "http://127.0.0.1:13000", "本机实际入口")
	flag.StringVar(&o.label, "label", "压测", "独立演示租户标签")
	flag.IntVar(&o.agents, "agents", 5, "客服数")
	flag.IntVar(&o.visitors, "visitors", 400, "预置排队访客数；生命周期用0")
	flag.StringVar(&o.stages, "stages", "50", "显式阶梯WS总数，默认只跑50")
	flag.IntVar(&o.rate, "rate", 10, "消息请求平均每秒上限，最大10")
	flag.Parse()
	interrupted, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(interrupted, 30*time.Minute)
	defer cancel()
	var err error
	switch o.mode {
	case "fixture":
		err = createFixture(ctx, o)
	case "run":
		err = runLoad(ctx, o)
	case "cleanup":
		err = cleanupFixture(ctx, o)
	default:
		err = errors.New("unsupported_mode")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "realtime_load_error:", safeError(err))
		os.Exit(1)
	}
}

// uuid 创建随机稳定操作编号，不使用进程时间猜测业务身份。
func uuid() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("random_unavailable")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// secret 产生仅保存在0600夹具中的独立高熵身份凭证。
func secret(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("random_unavailable")
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

// digestHex 按业务访客鉴权相同方式保存SHA256十六进制摘要。
func digestHex(value string) string {
	s := sha256.Sum256([]byte(value))
	return hex.EncodeToString(s[:])
}

// safeError 仅保留SQLSTATE或固定类型，禁止数据库驱动错误回显DSN和请求参数。
func safeError(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return "sqlstate_" + pg.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	return fmt.Sprintf("%T", err)
}

// writeJSON 将夹具和证据写到已明确指定目录，不覆盖现有文件夹以外资源。
func writeJSON(path string, value any, private bool) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	mode := os.FileMode(0644)
	if private {
		mode = 0600
	}
	return os.WriteFile(path, append(b, '\n'), mode)
}
