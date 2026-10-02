package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

type UserHandler struct {
	svc *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// 校验规则的取值必须与数据库的 CHECK 约束一致（见 docs/schema.md）。
// 标签里的规则只能写字面量——Go 的 struct tag 不接受常量拼接，
// 所以改了 CHECK 约束就要同时搜一遍 oneof= 来对齐。
type createUserRequest struct {
	Username string `json:"username" binding:"required,min=2,max=128"`
	// max=72 是 bcrypt 的上限，见 auth.MaxPasswordBytes
	Password string `json:"password" binding:"required,min=8,max=72"`
	Nickname string `json:"nickname" binding:"max=128"`
	Mobile   string `json:"mobile" binding:"max=20"`
	Email    string `json:"email" binding:"omitempty,email,max=64"`
	Status   string `json:"status" binding:"omitempty,oneof=active inactive"`
}

func (h *UserHandler) Create(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	u, err := h.svc.Create(c.Request.Context(), service.CreateUserInput{
		Username: req.Username,
		Password: req.Password,
		Nickname: req.Nickname,
		Mobile:   req.Mobile,
		Email:    req.Email,
		Status:   req.Status,
	})
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, u)
}

func (h *UserHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c)
	roleID, err := optionalID(c, "role_id")
	if err != nil {
		return
	}
	result, err := h.svc.List(c.Request.Context(), repository.UserFilter{
		Username: c.Query("username"),
		Nickname: c.Query("nickname"),
		Mobile:   c.Query("mobile"),
		Status:   c.Query("status"),
		RoleID:   roleID,
	}, page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

func (h *UserHandler) Get(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	detail, err := h.svc.GetDetail(c.Request.Context(), id)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, detail)
}

// Username 有意不在可改字段里：用户名是身份标识，改了会让审计记录失去意义，
// 所以数据库层面它也叫「不可变」。
type updateUserRequest struct {
	Nickname string `json:"nickname" binding:"max=128"`
	Mobile   string `json:"mobile" binding:"max=20"`
	Email    string `json:"email" binding:"omitempty,email,max=64"`
	Status   string `json:"status" binding:"required,oneof=active inactive"`
}

func (h *UserHandler) Update(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.Update(c.Request.Context(), domain.User{
		ID:       id,
		Status:   req.Status,
		Nickname: req.Nickname,
		Mobile:   req.Mobile,
		Email:    req.Email,
	}); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

func (h *UserHandler) Delete(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	// 当前登录用户从上下文取，不由客户端传入——
	// 否则只要伪造一个 id 就能绕过「不能删自己」的守卫
	currentUserID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id, currentUserID); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

type setRolesRequest struct {
	RoleIDs []int64 `json:"role_ids" binding:"required"`
}

func (h *UserHandler) SetRoles(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req setRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.SetRoles(c.Request.Context(), id, req.RoleIDs); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id, "role_ids": req.RoleIDs})
}

type resetPasswordRequest struct {
	Password string `json:"password" binding:"required,min=8,max=72"`
}

func (h *UserHandler) ResetPassword(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), id, req.Password); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": id})
}

// ---- 本包内共用的解析辅助 ----

// pathID 解析路径参数 id。失败时已经写好 400 响应，调用方直接 return 即可。
func pathID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidID)
		return 0, err
	}
	return id, nil
}

func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	return page, pageSize
}

// optionalID 解析一个可选的查询参数 ID。空串返回 0，表示不筛。
//
// 非法时报 400 而不是当作「不筛」——静默忽略的话筛选看起来生效了，
// 实际拿到的是全部数据，用户会对着没筛过的结果找问题。
// 失败时已经写好 400 响应，调用方直接 return 即可。
func optionalID(c *gin.Context, name string) (int64, error) {
	raw := c.Query(name)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidID)
		return 0, fmt.Errorf("非法的 %s=%q", name, raw)
	}
	return id, nil
}

// dateRange 解析日志筛选的 start / end 两个日期参数（YYYY-MM-DD）。
//
// 按**服务器本地时区**解释，与 cron 保持一致——「今天」是运维看到的那个今天。
// 代价是服务器与浏览器不在同一时区时会有一天以内的偏移，界面上显示的也是本地时间，
// 所以自托管场景下两者一致。
//
// 返回的 End 是**开区间上界**：界面上选的是「日期」而不是「时刻」，
// 选 10-01 到 10-01 应当包含那一整天，所以这里 +1 天。
//
// 格式错时已经写好 400 响应，调用方直接 return 即可。静默忽略是不行的——
// 用户会以为筛选生效了，对着一份没筛过的数据找问题。
func dateRange(c *gin.Context) (time.Time, time.Time, error) {
	start, err := parseDate(c.Query("start"))
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidDate)
		return time.Time{}, time.Time{}, err
	}
	end, err := parseDate(c.Query("end"))
	if err != nil {
		httpx.Fail(c, http.StatusBadRequest, httpx.KeyInvalidDate)
		return time.Time{}, time.Time{}, err
	}
	if !end.IsZero() {
		end = end.AddDate(0, 0, 1)
	}
	return start, end, nil
}

// parseDate 把 YYYY-MM-DD 解析成服务器本地时区的零点；空串返回零值。
func parseDate(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	d, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("解析日期 %q: %w", raw, err)
	}
	return d, nil
}
