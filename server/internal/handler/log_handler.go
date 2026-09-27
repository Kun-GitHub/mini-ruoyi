package handler

import (
	"github.com/gin-gonic/gin"

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

	result, err := h.logs.ListLoginLogs(c.Request.Context(), repository.LoginLogFilter{
		Username: c.Query("username"),
		Status:   c.Query("status"),
	}, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

func (h *LogHandler) ListOperLogs(c *gin.Context) {
	page, pageSize := pageParams(c)

	result, err := h.logs.ListOperLogs(c.Request.Context(), repository.OperLogFilter{
		Username: c.Query("username"),
		Method:   c.Query("method"),
		Path:     c.Query("path"),
	}, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}
