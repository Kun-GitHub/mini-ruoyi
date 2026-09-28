package handler

import (
	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/service"
)

type MonitorHandler struct {
	monitor *service.MonitorService
}

func NewMonitorHandler(monitor *service.MonitorService) *MonitorHandler {
	return &MonitorHandler{monitor: monitor}
}

// System 返回服务所在机器的运行状态（CPU / 内存 / 磁盘 / 进程）。
//
// 每个指标都带 available 标记：读不到就明说读不到。
// 1C1G 的机器上这个页面主要用来回答两个问题——
// 「快 OOM 了吗」和「磁盘写满了吗」。
func (h *MonitorHandler) System(c *gin.Context) {
	httpx.Success(c, h.monitor.Snapshot(c.Request.Context()))
}
