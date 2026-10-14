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
	result, err := h.svc.List(c.Request.Context(), roleFilter(c), page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

// roleFilter 解析角色列表与导出共用的筛选条件。
//
// 与列表共用一份的目的同用户导出：导出的文件必须与屏幕上的筛选一致，
// 否则用户会以为导出漏了数据。
func roleFilter(c *gin.Context) repository.RoleFilter {
	return repository.RoleFilter{
		Code:   c.Query("code"),
		Name:   c.Query("name"),
		Status: c.Query("status"),
	}
}

// roleExportHeader 是导出角色的列，与 sys_roles 的列名一致。
// 不导出授权（菜单/权限码）：那是另一份关系表，塞进同一行既会截断
// 又没法解析，需要时该单独给一个授权导出。
var roleExportHeader = []string{
	"id", "created_at", "updated_at", "status", "code", "name", "remark",
}

// Export 把角色按当前筛选条件导出成 CSV。
func (h *RoleHandler) Export(c *gin.Context) {
	filter := roleFilter(c)
	streamCSV(c, csvFilename("roles", time.Now()), roleExportHeader,
		func(ctx context.Context, page, size int) (service.Page[domain.Role], error) {
			return h.svc.List(ctx, filter, page, size)
		},
		func(r domain.Role) []string {
			return []string{
				strconv.FormatInt(r.ID, 10), csvTime(r.CreatedAt), csvTime(r.UpdatedAt),
				r.Status, r.Code, r.Name, r.Remark,
			}
		})
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
