package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/service"
)

type DeviceHandler struct {
	svc *service.DeviceService
}

func NewDeviceHandler(svc *service.DeviceService) *DeviceHandler {
	return &DeviceHandler{svc: svc}
}

type createDeviceRequest struct {
	Name     string `json:"name" binding:"required,min=2,max=64"`
	Location string `json:"location" binding:"required"`
	Enabled  bool   `json:"enabled"`
}

func (h *DeviceHandler) Create(c *gin.Context) {
	var req createDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	d, err := h.svc.Create(c.Request.Context(), service.CreateDeviceInput{
		Name: req.Name, Location: req.Location, Enabled: req.Enabled,
	})
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, d)
}

func (h *DeviceHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	list, err := h.svc.List(c.Request.Context(), page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, list)
}

func (h *DeviceHandler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	d, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, d)
}

// Enabled 用指针是为了区分「没传这个字段」和「传了 false」：
// 用 bool 的话 PATCH 一个 {} 会把 enabled 静默改成 false。
type updateDeviceRequest struct {
	Enabled *bool `json:"enabled"`
}

func (h *DeviceHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req updateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if req.Enabled == nil {
		httpx.FailValidation(c, []httpx.FieldError{{Field: "enabled", Rule: "required"}})
		return
	}
	if err := h.svc.SetEnabled(c.Request.Context(), id, *req.Enabled); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id, "enabled": *req.Enabled})
}

func (h *DeviceHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	// 不返回 204：所有接口统一走响应信封，前端无需特判空 body
	httpx.Success(c, gin.H{"id": id})
}

// parseID 解析路径参数 id，失败时已写好 400 响应，调用方直接 return 即可。
func parseID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidID)
		return 0, err
	}
	return id, nil
}
