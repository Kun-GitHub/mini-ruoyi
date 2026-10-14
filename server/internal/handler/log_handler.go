// 有问题欢迎联系 hot_kun@hotmail.com
package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

type LogHandler struct {
	logs *service.LogService
}

func NewLogHandler(logs *service.LogService) *LogHandler {
	return &LogHandler{logs: logs}
}

func (h *LogHandler) ListLoginLogs(c *gin.Context) {
	page, pageSize := pageParams(c)
	filter, err := loginLogFilter(c)
	if err != nil {
		return
	}

	result, err := h.logs.ListLoginLogs(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

// loginLogFilter 解析登录日志列表与导出共用的筛选条件。
//
// 与列表共用一份的目的同操作日志：导出的文件必须与屏幕上的筛选一致，
// 否则用户会以为导出漏了数据。失败时已写好 400 响应，调用方直接 return。
func loginLogFilter(c *gin.Context) (repository.LoginLogFilter, error) {
	start, end, err := dateRange(c)
	if err != nil {
		return repository.LoginLogFilter{}, err
	}
	return repository.LoginLogFilter{
		Username: c.Query("username"),
		Status:   c.Query("status"),
		Start:    start,
		End:      end,
	}, nil
}

func (h *LogHandler) ListOperLogs(c *gin.Context) {
	page, pageSize := pageParams(c)
	filter, err := operLogFilter(c)
	if err != nil {
		return
	}

	result, err := h.logs.ListOperLogs(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

// operLogExportHeader 是导出操作日志的列。
//
// 比列表页多一列 user_agent：界面上放不下，但追查「这条请求是人点的
// 还是脚本打的」全靠它，而导出的文件正是给人去比对、去筛查的。
var operLogExportHeader = []string{
	"created_at", "username", "method", "path", "status", "result", "duration_ms", "ip", "user_agent",
}

// ExportOperLogs 把操作日志按当前筛选条件导出成 CSV。
func (h *LogHandler) ExportOperLogs(c *gin.Context) {
	filter, err := operLogFilter(c)
	if err != nil {
		return
	}
	logs := h.logs
	streamCSV(c, csvFilename("oper-logs", time.Now()), operLogExportHeader,
		func(ctx context.Context, page, size int) (service.Page[domain.OperLog], error) {
			return logs.ListOperLogs(ctx, filter, page, size)
		},
		func(l domain.OperLog) []string {
			return []string{
				csvTime(l.CreatedAt), l.Username, l.Method, l.Path,
				strconv.Itoa(l.Status), l.Result, strconv.Itoa(l.DurationMS), l.IP, l.UserAgent,
			}
		})
}

// operLogFilter 解析操作日志列表与导出共用的筛选条件。
//
// 与列表共用一份的目的同用户导出：导出的文件必须与屏幕上的筛选一致。
// 失败时已经写好 400 响应，调用方直接 return 即可。
func operLogFilter(c *gin.Context) (repository.OperLogFilter, error) {
	start, end, err := dateRange(c)
	if err != nil {
		return repository.OperLogFilter{}, err
	}
	return repository.OperLogFilter{
		Username: c.Query("username"),
		Method:   c.Query("method"),
		Path:     c.Query("path"),
		Start:    start,
		End:      end,
	}, nil
}

// loginLogExportHeader 是导出登录日志的列。
//
// reason 是 i18n 键（如 error.badCredentials），导出的就是键本身：
// 导出的文件要能被程序再解析，翻成中文反而丢了结构。列名与 sys_login_logs
// 的列名一致。
var loginLogExportHeader = []string{
	"created_at", "username", "status", "reason", "ip", "user_agent",
}

// ExportLoginLogs 把登录日志按当前筛选条件导出成 CSV。
func (h *LogHandler) ExportLoginLogs(c *gin.Context) {
	filter, err := loginLogFilter(c)
	if err != nil {
		return
	}
	logs := h.logs
	streamCSV(c, csvFilename("login-logs", time.Now()), loginLogExportHeader,
		func(ctx context.Context, page, size int) (service.Page[domain.LoginLog], error) {
			return logs.ListLoginLogs(ctx, filter, page, size)
		},
		func(l domain.LoginLog) []string {
			return []string{
				csvTime(l.CreatedAt), l.Username, l.Status, l.Reason, l.IP, l.UserAgent,
			}
		})
}
