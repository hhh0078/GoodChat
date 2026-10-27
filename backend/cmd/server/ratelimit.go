// 文件作用说明：使用现有Redis的GoodChat独立前缀限制公开接入、消息和附件频率，不修改其他项目键。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"supportdesk/backend/internal/platform"
)

const rateScript = `local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],ARGV[1]) end;return n`

// rateBucket 原子增加短期计数；scope只允许服务端预定义值，identity仅存不可逆摘要。
func rateBucket(ctx context.Context, cache *redis.Client, scope, identity string, limit int) (bool, error) {
	sum := sha256.Sum256([]byte(identity))
	key := "goodchat:rate:" + scope + ":" + strconv.FormatInt(time.Now().Unix()/60, 10) + ":" + hex.EncodeToString(sum[:16])
	count, err := cache.Eval(ctx, rateScript, []string{key}, 120).Int64()
	return count <= int64(limit), err
}

// rateLimits 在可信来源中间件之后控制写入频率，登录凭证只用于摘要分桶，不进入日志或Redis明文。
func rateLimits(cache *redis.Client, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		scope, limit := "", 0
		switch {
		case r.Method == "POST" && path == "/api/v1/visitor/session":
			scope, limit = "visit", 30
		case r.Method == "GET" && strings.HasPrefix(path, "/api/v1/entries/"):
			scope, limit = "entry", 120
		case r.Method == "POST" && path == "/api/v1/attachments":
			scope, limit = "upload", 20
		case r.Method == "POST" && strings.HasSuffix(path, "/messages"):
			scope, limit = "message", 120
		case r.Method == "POST" && strings.HasSuffix(path, "/typing"):
			scope, limit = "typing", 240
		case r.Method == "POST" && path == "/api/v1/realtime/ticket":
			scope, limit = "ticket", 60
		}
		if limit == 0 {
			next.ServeHTTP(w, r)
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		identity := ip
		if scope != "visit" && scope != "entry" {
			if token := r.Header.Get("Authorization"); token != "" {
				identity = token
			} else if cookie, err := r.Cookie("support_session"); err == nil {
				identity = cookie.Value
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		allowed, err := rateBucket(ctx, cache, scope, identity, limit)
		if err == nil && allowed && identity != ip {
			allowed, err = rateBucket(ctx, cache, scope+"_ip", ip, 1200)
		}
		if err != nil {
			platform.ErrorJSON(w, r, platform.Fail(503, "rate_service_unavailable", "服务暂时繁忙，请稍后重试"))
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", "60")
			platform.ErrorJSON(w, r, platform.Fail(429, "rate_limited", "操作过于频繁，请稍后重试"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
