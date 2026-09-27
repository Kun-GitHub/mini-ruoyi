package handler

import (
	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/service"
)

type MenuHandler struct {
	svc *service.MenuService
}

func NewMenuHandler(svc *service.MenuService) *MenuHandler {
	return &MenuHandler{svc: svc}
}

// 校验规则的取值必须与数据库的 CHECK 约束一致（见 docs/schema.md）。
// 标签里的规则只能写字面量——Go 的 struct tag 不接受常量拼接，
// 所以改了 CHECK 约束就要同时搜一遍 oneof= 来对齐。
type createMenuRequest struct {
	// 允许为空表示根节点
	ParentID  *int64 `json:"parent_id"`
	Sort      int    `json:"sort"`
	MenuType  string `json:"menu_type" binding:"required,oneof=directory menu"`
	TitleKey  string `json:"title_key" binding:"required,max=128,startswith=menu."`
	Path      string `json:"path" binding:"max=255"`
	Component string `json:"component" binding:"max=255"`
	Icon      string `json:"icon" binding:"max=64"`
	Status    string `json:"status" binding:"omitempty,oneof=active inactive"`
}

func (h *MenuHandler) Create(c *gin.Context) {
	var req createMenuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	menu, err := h.svc.Create(c.Request.Context(), domain.Menu{
		Status:    req.Status,
		ParentID:  req.ParentID,
		Sort:      req.Sort,
		MenuType:  req.MenuType,
		TitleKey:  req.TitleKey,
		Path:      req.Path,
		Component: req.Component,
		Icon:      req.Icon,
	})
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, menu)
}

// Tree 返回完整菜单树（含未启用项），给菜单管理界面用。
// 前端导航用的是 /auth/me 里按角色过滤过的那份。
func (h *MenuHandler) Tree(c *gin.Context) {
	tree, err := h.svc.Tree(c.Request.Context())
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"tree": tree})
}

func (h *MenuHandler) Get(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	menu, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, menu)
}

type updateMenuRequest struct {
	ParentID  *int64 `json:"parent_id"`
	Sort      int    `json:"sort"`
	MenuType  string `json:"menu_type" binding:"required,oneof=directory menu"`
	TitleKey  string `json:"title_key" binding:"required,max=128,startswith=menu."`
	Path      string `json:"path" binding:"max=255"`
	Component string `json:"component" binding:"max=255"`
	Icon      string `json:"icon" binding:"max=64"`
	Status    string `json:"status" binding:"required,oneof=active inactive"`
}

func (h *MenuHandler) Update(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req updateMenuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.Update(c.Request.Context(), domain.Menu{
		ID:        id,
		Status:    req.Status,
		ParentID:  req.ParentID,
		Sort:      req.Sort,
		MenuType:  req.MenuType,
		TitleKey:  req.TitleKey,
		Path:      req.Path,
		Component: req.Component,
		Icon:      req.Icon,
	}); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

// Delete 支持 ?cascade=true。不带时若有子菜单或被角色引用则返回 409 + 影响面。
func (h *MenuHandler) Delete(c *gin.Context) {
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
