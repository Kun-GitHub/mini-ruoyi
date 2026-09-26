package middleware

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
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

// RateLimit 全局限流，低配服务器防止突发流量把进程打崩。
// rps: 每秒放行请求数；burst: 允许的瞬时突发量。
func RateLimit(rps float64, burst int) gin.HandlerFunc {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	return func(c *gin.Context) {
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code": 1, "msg": "too many requests",
			})
			return
		}
		c.Next()
	}
}
