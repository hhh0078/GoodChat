// 文件作用说明：装配GoodChat邮箱身份、租户客服、回调与统计服务，运行在单个应用容器中。
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"supportdesk/backend/internal/auth"
	"supportdesk/backend/internal/billing"
	"supportdesk/backend/internal/callbacks"
	"supportdesk/backend/internal/chat"
	"supportdesk/backend/internal/decoration"
	"supportdesk/backend/internal/platform"
	"supportdesk/backend/internal/reporting"
)

// main 验证非特权数据库账号后启动业务服务，退出时停止认领任务并等待请求完成。
func main() {
	cfg, err := platform.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("数据库配置不可用")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		log.Fatal("数据库暂时无法连接")
	}
	var elevated bool
	if err = pool.QueryRow(ctx, "SELECT r.rolsuper OR r.rolbypassrls OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p') AND c.relowner=r.oid) FROM pg_roles r WHERE r.rolname=current_user").Scan(&elevated); err != nil || elevated {
		log.Fatal("应用需要使用非特权且不是业务表所有者的数据库账号")
	}
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatal("Redis配置不可用")
	}
	cache := redis.NewClient(redisOptions)
	defer cache.Close()
	if err = cache.Ping(ctx).Err(); err != nil {
		log.Fatal("现有Redis暂时无法连接")
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			_ = cache.Set(ctx, "goodchat:application:heartbeat", time.Now().UTC().Format(time.RFC3339), 45*time.Second).Err()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	identity, err := auth.New(pool, cfg)
	if err != nil {
		log.Fatal(err)
	}
	messaging := chat.New(pool, identity, cfg)
	messaging.SetLeadNotifier(identity.SendNotification)
	plans := billing.New(pool, identity, cfg)
	messaging.SetFeatureChecker(plans.CheckFeature)
	identity.SetSeatChecker(plans.CheckSeats)
	identity.SetTenantInitializer(func(ctx context.Context, tx pgx.Tx, tenantID, userID string) error {
		if err := plans.EnsureTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID); err != nil {
			return err
		}
		var memberUser any
		if userID != "" {
			memberUser = userID
		}
		_, err := tx.Exec(ctx, "SELECT ensure_chat_defaults($1::uuid,$2::uuid)", tenantID, memberUser)
		return err
	})
	hooks := callbacks.New(pool, identity, cfg)
	hooks.SetFeatureChecker(plans.CheckFeature)
	designs, err := decoration.New(pool, identity, cfg)
	if err != nil {
		log.Fatal(err)
	}
	designs.SetFeatureChecker(plans.CheckFeature)
	messaging.SetEntryDecoration(designs.EntryDecoration)
	messaging.SetVisitorEnteredHook(hooks.Enqueue)
	mux := http.NewServeMux()
	identity.Register(mux)
	messaging.Register(mux)
	hooks.Register(mux)
	plans.Register(mux)
	designs.Register(mux)
	reports := reporting.New(pool, identity)
	reports.SetFeatureChecker(plans.CheckFeature)
	reports.Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, done := context.WithTimeout(r.Context(), 2*time.Second)
		defer done()
		if pool.Ping(pingCtx) != nil {
			platform.JSON(w, 503, map[string]any{"status": "degraded", "database": false})
			return
		}
		if cache.Ping(pingCtx).Err() != nil {
			platform.JSON(w, 503, map[string]any{"status": "degraded", "database": true, "redis": false})
			return
		}
		platform.JSON(w, 200, map[string]any{"status": "ok", "database": true, "redis": true, "version": "0.1.0"})
	})
	identity.Start(ctx)
	messaging.Start(ctx)
	messaging.StartLeadNotifications(ctx)
	hooks.Start(ctx)
	designs.Start(ctx)
	codeServer := &http.Server{Addr: ":18081", Handler: designs.CodeHandler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		if err := codeServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("装修渲染服务异常：%v", err)
			cancel()
		}
	}()
	server := &http.Server{Addr: cfg.Address, Handler: observeHTTP(middleware(cfg, rateLimits(cache, mux)), slog.New(slog.NewJSONHandler(os.Stdout, nil))), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	shutdownDone := shutdownHTTP(ctx, server, codeServer)
	log.Printf("GoodChat API已启动，监听%s", cfg.Address)
	err = server.ListenAndServe()
	cancel()
	<-shutdownDone
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// middleware 设置请求编号和基础响应安全头，拒绝浏览器跨站写入。
func middleware(cfg platform.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identifier := make([]byte, 12)
		_, _ = rand.Read(identifier)
		r.Header.Set("X-Request-ID", hex.EncodeToString(identifier))
		w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		remote, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(remote); ip != nil && ip.IsLoopback() && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-GoodChat-Gateway-Token")), []byte(cfg.GatewaySecret)) == 1 {
			if client := net.ParseIP(r.Header.Get("X-GoodChat-Client-IP")); client != nil {
				r.RemoteAddr = net.JoinHostPort(client.String(), "0")
			}
		}
		if origin := r.Header.Get("Origin"); origin != "" && r.Method != http.MethodGet && r.Method != http.MethodHead {
			allowed, _ := url.Parse(cfg.PublicURL)
			incoming, _ := url.Parse(origin)
			if allowed == nil || incoming == nil || incoming.Scheme != allowed.Scheme || !strings.EqualFold(incoming.Host, allowed.Host) {
				platform.ErrorJSON(w, r, platform.Fail(403, "origin_denied", "请从GoodChat页面完成此操作"))
				return
			}
		}
		defer func() {
			if recover() != nil {
				platform.ErrorJSON(w, r, platform.Fail(500, "internal_error", "服务暂时没有处理成功，请重试"))
				log.Printf("request_id=%s 请求处理异常", r.Header.Get("X-Request-ID"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
