// 文件作用说明：通过服务端协议Ping/Pong探活并续租，不依赖浏览器主线程心跳定时器。
package chat

import (
	"context"
	"github.com/coder/websocket"
	"time"
)

// keepRealtimeAlive 在读取协程处理Pong的同时定时探活，成功后核验身份并续租，失败立即结束连接。
// interval 为探活周期，timeout 为单次探活和续租总时限；调用方必须同时执行conn.Read。
func keepRealtimeAlive(ctx context.Context, conn *websocket.Conn, interval, timeout time.Duration, renew func(context.Context) error, failed func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, cancel := context.WithTimeout(ctx, timeout)
			err := conn.Ping(probeCtx)
			if err == nil {
				err = renew(probeCtx)
			}
			if err == nil {
				err = conn.Write(probeCtx, websocket.MessageText, jsonValue(map[string]any{"type": "heartbeat", "data": map[string]any{"time": nowUTC()}}))
			}
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					failed(err)
				}
				return
			}
		}
	}
}
