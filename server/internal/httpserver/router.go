package httpserver

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/handler"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/middleware"
)

const (
	rateLimitRPS   = 20
	rateLimitBurst = 40
	rateLimitTTL   = 3 * time.Minute
	maxBodyBytes   = 1 << 20 // 1MiB
)

// NewRouter 组装路由。前端产物目录不可用时直接返回错误，让进程启动即失败，
// 而不是等用户访问了才发现路径配错。
func NewRouter(h *handler.DeviceHandler, db *sql.DB, webDir string) (*gin.Engine, error) {
	// 校验错误的字段名用 json tag 输出，需在注册路由前生效
	httpx.RegisterJSONFieldNames()

	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestLogger())

	// 本服务直接对外监听（无 nginx 反代），不能信任任何 X-Forwarded-For，
	// 否则限流用的 ClientIP() 可以被伪造。
	if err := r.SetTrustedProxies(nil); err != nil {
		return nil, err
	}

	r.Use(
		middleware.BodyLimit(maxBodyBytes),
		middleware.RateLimit(rateLimitRPS, rateLimitBurst, rateLimitTTL),
		middleware.ImmutableAssets(AssetsPrefix),
	)

	r.GET("/healthz", func(c *gin.Context) {
		if err := db.PingContext(c.Request.Context()); err != nil {
			httpx.Fail(c, http.StatusServiceUnavailable, httpx.KeyServiceUnavailable)
			return
		}
		httpx.Success(c, gin.H{"status": "up"})
	})

	v1 := r.Group("/api/v1")
	{
		devices := v1.Group("/devices")
		devices.POST("", h.Create)
		devices.GET("", h.List)
		devices.GET("/:id", h.Get)
		devices.PATCH("/:id", h.Update)
		devices.DELETE("/:id", h.Delete)
	}

	if err := MountStatic(r, webDir); err != nil {
		return nil, err
	}
	return r, nil
}
