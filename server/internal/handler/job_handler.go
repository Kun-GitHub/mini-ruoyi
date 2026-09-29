package handler

import (
	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/service"
)

type JobHandler struct {
	jobs *service.JobService
}

func NewJobHandler(jobs *service.JobService) *JobHandler {
	return &JobHandler{jobs: jobs}
}

// List 返回全部**已注册**任务的状态。
//
// 以代码注册表为准：数据库里可能残留已从代码删除的任务，
// 它们永远不会被执行，不该出现在界面上。
func (h *JobHandler) List(c *gin.Context) {
	list, err := h.jobs.List(c.Request.Context())
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"list": list, "total": len(list)})
}

// ListLogs 返回某个任务的执行历史（成功 / 失败 / 跳过都在内）。
//
// 未注册的任务 key 同样走 404：清单以代码注册表为准，
// 库里残留的已删任务不该在界面上留一个能点开的入口。
func (h *JobHandler) ListLogs(c *gin.Context) {
	key := c.Param("key")
	page, pageSize := pageParams(c)

	result, err := h.jobs.ListLogs(c.Request.Context(), key, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

type updateJobRequest struct {
	// cron 是标准 5 段表达式（分 时 日 月 周），不是若依那种 6 段。
	Cron   string `json:"cron" binding:"required,max=64"`
	Status string `json:"status" binding:"required,oneof=active inactive"`
	Remark string `json:"remark" binding:"max=255"`
}

func (h *JobHandler) Update(c *gin.Context) {
	key := c.Param("key")

	var req updateJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}

	if err := h.jobs.Update(c.Request.Context(), key, req.Cron, req.Status, req.Remark); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"job_key": key})
}

// RunNow 立即执行一次。
//
// 异步：任务的执行时间不可控，同步等待会让请求挂住最多 5 分钟。
// 返回值只表示「已触发」，结果要刷新列表看。
func (h *JobHandler) RunNow(c *gin.Context) {
	key := c.Param("key")

	if err := h.jobs.RunNow(c.Request.Context(), key); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"job_key": key, "triggered": true})
}
