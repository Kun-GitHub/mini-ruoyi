// 有问题欢迎联系 hot_kun@hotmail.com
package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/reqid"
)

// RequestID 给每个请求一个 ID，回写到响应头并放进请求上下文。
//
// 上游（nginx、网关、探针）传来的 ID 合法就沿用，否则重新生成——
// 不合法的值会被写进日志行，见 reqid.Valid 的日志注入说明。
//
// 放在 RequestLogger 之前：日志是这个 ID 最主要的消费方。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(reqid.Header)
		if !reqid.Valid(id) {
			id = reqid.New()
		}
		c.Writer.Header().Set(reqid.Header, id)
		c.Request = c.Request.WithContext(reqid.WithContext(c.Request.Context(), id))
		c.Next()
	}
}

// RequestLogger 用 slog 记一条结构化的访问日志。
//
// 结构化（键值对）而不是拼字符串：`grep "status=500"` 和
// `grep "request_id=..."` 都能直接用，而拼接出来的那行只能整行匹配。
// 状态码 ≥500 记为 ERROR，其余记为 INFO——4xx 大多是正常的业务结果
// （密码错、没权限、资源不存在），按 ERROR 记会让真问题淹没在里面。
//
// 耗时用 duration_ms 而不是人类可读的 `1.2s`：前者能直接比较和求和。
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"request_id", reqid.From(c.Request.Context()),
		}
		if c.Writer.Status() >= http.StatusInternalServerError {
			slog.Error("request", attrs...)
			return
		}
		slog.Info("request", attrs...)
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
			httpx.Fail(c, http.StatusTooManyRequests, httpx.KeyTooManyRequests)
			c.Abort()
			return
		}
		c.Next()
	}
}

// BodyLimit 限制请求体大小。没有它，一个超大 body 就能把 1G 内存的机器打爆。
//
// skipPrefixes 用于放行那些**自己控制上限**的路由：文件上传的体量远超普通
// JSON 请求，而且它需要给出「文件太大」而不是「请求太大」。
// 这里必须放行而不是「用一个更大的值再包一层」——
// MaxBytesReader 是可以嵌套的，内层那个小的会先报错。
func BodyLimit(maxBytes int64, skipPrefixes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(c.Request.URL.Path, prefix) {
				c.Next()
				return
			}
		}
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
