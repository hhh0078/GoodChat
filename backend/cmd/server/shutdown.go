// 文件作用说明：等待HTTP服务真正完成宽限关闭，避免监听器停止后main提前退出而截断在途请求。
package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// shutdownHTTP 在信号到达后并发停止入口与渲染服务，并返回必须由main等待的完成通道。
func shutdownHTTP(ctx context.Context, servers ...*http.Server) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		grace, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var group sync.WaitGroup
		for _, server := range servers {
			group.Add(1)
			go func(server *http.Server) { defer group.Done(); _ = server.Shutdown(grace) }(server)
		}
		group.Wait()
	}()
	return done
}
