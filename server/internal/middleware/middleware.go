package middleware

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"mini-ruoyi/internal/handler"
)

// RequestLogger 简单耗时日志。生产建议换 zap/slog，这里保持零依赖。
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("%s %s %d %s",
			c.Request.Method, c.Request.URL.Path,
			c.Writer.Status(), time.Since(start))
	}
}

// RateLimit 按客户端 IP 限流，低配服务器防止突发流量把进程打崩。
//
// 按 IP 而非全进程：全进程令牌桶意味着单个客户端刷满配额后，其他所有用户
// 都会一起收到 429，反而成了放大攻击的手段。
//
// 注意：依赖 c.ClientIP()，因此路由必须调用 SetTrustedProxies(nil)，
// 否则客户端可伪造 X-Forwarded-For 绕过限流。
func RateLimit(rps float64, burst int, idleTTL time.Duration) gin.HandlerFunc {
	type client struct {
		limiter *rate.Limiter
		seen    time.Time
	}

	var (
		mu      sync.Mutex
		clients = map[string]*client{}
		lastGC  time.Time
	)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		mu.Lock()
		// 惰性 GC：不清理的话，被扫描时 map 会随访问过的 IP 无上限增长，
		// 这在 1C1G 上比限流本身更危险。
		if now.Sub(lastGC) > idleTTL {
			for k, v := range clients {
				if now.Sub(v.seen) > idleTTL {
					delete(clients, k)
				}
			}
			lastGC = now
		}
		cl, ok := clients[ip]
		if !ok {
			cl = &client{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
			clients[ip] = cl
		}
		cl.seen = now
		mu.Unlock()

		if !cl.limiter.Allow() {
			handler.Fail(c, http.StatusTooManyRequests, "too many requests")
			c.Abort()
			return
		}
		c.Next()
	}
}

// BodyLimit 限制请求体大小。没有它，一个超大 body 就能把 1G 内存的机器打爆。
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

// ImmutableAssets 给带内容哈希的静态资源加长缓存头。
//
// 前端每次构建后文件名哈希都会变，所以内容与 URL 一一对应，可以放心 immutable。
// index.html 不能这样缓存，否则浏览器会一直引用旧的 asset 哈希，
// 导致前端重新构建后刷新拿不到最新版本。
func ImmutableAssets(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, prefix) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		}
		c.Next()
	}
}
