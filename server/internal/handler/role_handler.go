package handler

import (
	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

type RoleHandler struct {
	svc *service.RoleService
}

func NewRoleHandler(svc *service.RoleService) *RoleHandler {
	return &RoleHandler{svc: svc}
}

// 校验规则的取值必须与数据库的 CHECK 约束一致（见 docs/schema.md）。
// 标签里的规则只能写字面量——Go 的 struct tag 不接受常量拼接，
// 所以改了 CHECK 约束就要同时搜一遍 oneof= 来对齐。
type createRoleRequest struct {
	Code   string `json:"code" binding:"required,min=2,max=64"`
	Name   string `json:"name" binding:"required,min=2,max=128"`
	Remark string `json:"remark" binding:"max=255"`
	Status string `json:"status" binding:"omitempty,oneof=active inactive"`
}

func (h *RoleHandler) Create(c *gin.Context) {
	var req createRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	role, err := h.svc.Create(c.Request.Context(), domain.Role{
		Code:   req.Code,
		Name:   req.Name,
		Remark: req.Remark,
		Status: req.Status,
	})
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, role)
}

func (h *RoleHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c)
	result, err := h.svc.List(c.Request.Context(), repository.RoleFilter{
		Code:   c.Query("code"),
		Name:   c.Query("name"),
		Status: c.Query("status"),
	}, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

func (h *RoleHandler) Get(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	role, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, role)
}

// updateRoleRequest 没有 code：角色编码是授权判断的依据，改了会让已有的
// 代码内引用（如内置 admin）失效，所以不可变。
type updateRoleRequest struct {
	Name   string `json:"name" binding:"required,min=2,max=128"`
	Remark string `json:"remark" binding:"max=255"`
	Status string `json:"status" binding:"required,oneof=active inactive"`
}

func (h *RoleHandler) Update(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.Update(c.Request.Context(), domain.Role{
		ID: id, Name: req.Name, Remark: req.Remark, Status: req.Status,
	}); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

// Delete 支持 ?cascade=true。
//
// 不带 cascade 时，若角色被用户持有则返回 409 + 影响面，
// 前端据此弹确认框，用户确认后带 cascade 重发。契约见 docs/architecture.md §3.4。
func (h *RoleHandler) Delete(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id, cascadeRequested(c)); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

func (h *RoleHandler) Grants(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	grants, err := h.svc.Grants(c.Request.Context(), id)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, grants)
}

type setGrantsRequest struct {
	MenuIDs   []int64  `json:"menu_ids"`
	PermCodes []string `json:"perm_codes"`
}

func (h *RoleHandler) SetGrants(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req setGrantsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.SetGrants(c.Request.Context(), id, service.Grants{
		MenuIDs: req.MenuIDs, PermCodes: req.PermCodes,
	}); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

// cascadeRequested 判断调用方是否已确认级联删除。
func cascadeRequested(c *gin.Context) bool {
	return c.Query("cascade") == "true"
}
