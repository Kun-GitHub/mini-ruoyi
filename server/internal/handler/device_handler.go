package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"gin-sqlite-example/internal/repository"
	"gin-sqlite-example/internal/service"
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
		Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	d, err := h.svc.Create(c.Request.Context(), service.CreateDeviceInput{
		Name: req.Name, Location: req.Location, Enabled: req.Enabled,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, "create failed")
		return
	}
	Success(c, d)
}

func (h *DeviceHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	list, err := h.svc.List(c.Request.Context(), page, pageSize)
	if err != nil {
		Fail(c, http.StatusInternalServerError, "list failed")
		return
	}
	Success(c, list)
}

func (h *DeviceHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid id")
		return
	}
	d, err := h.svc.Get(c.Request.Context(), id)
	if errors.Is(err, repository.ErrNotFound) {
		Fail(c, http.StatusNotFound, "device not found")
		return
	}
	if err != nil {
		Fail(c, http.StatusInternalServerError, "get failed")
		return
	}
	Success(c, d)
}

type updateDeviceRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *DeviceHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.SetEnabled(c.Request.Context(), id, req.Enabled); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			Fail(c, http.StatusNotFound, "device not found")
			return
		}
		Fail(c, http.StatusInternalServerError, "update failed")
		return
	}
	Success(c, gin.H{"id": id, "enabled": req.Enabled})
}

func (h *DeviceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			Fail(c, http.StatusNotFound, "device not found")
			return
		}
		Fail(c, http.StatusInternalServerError, "delete failed")
		return
	}
	c.Status(http.StatusNoContent)
}
