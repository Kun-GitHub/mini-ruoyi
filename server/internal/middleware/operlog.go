package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
)

// OperLogRecorder 是 OperationLog 需要的落库能力，由 service.LogService 实现。
type OperLogRecorder interface {
	RecordOper(domain.OperLog)
}

// OperationLog 记录「谁在什么时候动了哪个资源」。
//
// **记录范围**：非 GET/HEAD/OPTIONS 的请求（写操作），外加任何以 401/403
// 结束的请求——后者是安全审计最关心的：「谁在反复试探自己没有权限的接口」。
//
// **不记录请求体**：登录、改密码、重置密码这些接口的 body 里是明文密码，
// 而日志表的访问控制比用户表弱得多，把它们写进去等于自己制造一个泄密点。
// method + path 已经能回答「谁动了哪个资源」，具体值可以查资源本身。
//
// 必须挂在鉴权中间件**之后**：要拿到已解析的身份，也要让 403 发生在本中间件的
// Next 之内（否则权限拒绝的请求不会被记录）。
func OperationLog(recorder OperLogRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if !shouldLog(c) {
			return
		}

		var userID int64
		var username string
		if identity, ok := IdentityOf(c); ok {
			userID = identity.User.ID
			username = identity.User.Username
		}

		recorder.RecordOper(domain.OperLog{
			UserID:     userID,
			Username:   username,
			Method:     c.Request.Method,
			Path:       c.Request.URL.Path,
			Status:     c.Writer.Status(),
			Result:     httpx.ResultKey(c),
			DurationMS: int(time.Since(start).Milliseconds()),
			IP:         c.ClientIP(),
			UserAgent:  c.Request.UserAgent(),
		})
	}
}

func shouldLog(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		// 读操作不记，除非被拒绝——那是试探行为，要留痕
		status := c.Writer.Status()
		return status == http.StatusUnauthorized || status == http.StatusForbidden
	default:
		return true
	}
}
