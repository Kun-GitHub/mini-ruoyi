package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/reqid"
)

// Recovery 是 gin.Recovery() 的替代品。
//
// gin 自带的 Recovery 会返回一个**空 body 的 500**，这违反本项目的契约：
// 所有 API 响应都带 {code,msg,...} 信封。后果不只是前端拿不到错误信息——
// 它还会破坏客户端用来判断「这个响应是不是本服务发的」的依据。
// client.ts 正是靠「响应是不是信封格式」来区分「后端没起来」和「后端出错了」，
// 空 body 的 500 会被误判成前者。
//
// 堆栈必须打出来：Recovery 拦下的 panic 不会让进程退出，如果没有日志，
// 这个 500 就完全无从查起。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		// stack 作为独立字段而不是拼进消息：文本里塞多行会让后面的字段
		// 无法被 logfmt 解析，而堆栈恰恰是最该单独查看的部分。
		slog.Error("panic",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"panic", recovered,
			"request_id", reqid.From(c.Request.Context()),
			"stack", string(debug.Stack()),
		)
		httpx.Fail(c, http.StatusInternalServerError, httpx.KeyInternal)
		c.Abort()
	})
}
