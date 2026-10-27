// 文件作用说明：验证现有Redis中的频率计数原子性和GoodChat独立短期键，不清空Redis或触碰其他项目。
package main

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	"supportdesk/backend/internal/platform"
)

// TestAtomicRedisRateBucket 验证24个并发请求只能通过12个，测试键自动到期。
func TestAtomicRedisRateBucket(t *testing.T) {
	if os.Getenv("SUPPORT_REDIS_INTEGRATION") != "1" {
		t.Skip("设置SUPPORT_REDIS_INTEGRATION=1使用现有Redis执行原子频率测试")
	}
	address := os.Getenv("SUPPORT_TEST_REDIS_ADDR")
	if address == "" {
		address = "127.0.0.1:56379"
	}
	cache := redis.NewClient(&redis.Options{Addr: address})
	defer cache.Close()
	identity, err := platform.Token()
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan bool, 24)
	failures := make(chan error, 24)
	for i := 0; i < 24; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			ok, err := rateBucket(context.Background(), cache, "integration", identity, 12)
			results <- ok
			failures <- err
		}()
	}
	group.Wait()
	close(results)
	close(failures)
	passed := 0
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for ok := range results {
		if ok {
			passed++
		}
	}
	if passed != 12 {
		t.Fatalf("24并发限额12应恰好通过12次，实际%d", passed)
	}
}
