package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/service"
)

// ProfileHandler 处理「我自己的东西」。
//
// 这几个接口刻意**不要求任何权限码**（路由上挂的是 self 而不是 protect）：
// 任何一个能登录的账号都必须能改自己的密码——否则初始密码是管理员设的，
// 本人想改还得再去找管理员，而没有权限的账号干脆一点自助能力都没有。
type ProfileHandler struct {
	users *service.UserService
}

func NewProfileHandler(users *service.UserService) *ProfileHandler {
	return &ProfileHandler{users: users}
}

type updateProfileRequest struct {
	Nickname string `json:"nickname" binding:"max=128"`
	Mobile   string `json:"mobile" binding:"max=20"`
	Email    string `json:"email" binding:"omitempty,email,max=64"`
}

func (h *ProfileHandler) Update(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
		return
	}

	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}

	if err := h.users.UpdateOwnProfile(c.Request.Context(), userID, req.Nickname, req.Mobile, req.Email); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": userID})
}

type changePasswordRequest struct {
	// max=72 是 bcrypt 的上限，见 auth.MaxPasswordBytes
	OldPassword string `json:"old_password" binding:"required,max=72"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=72"`
}

func (h *ProfileHandler) ChangePassword(c *gin.Context) {
	session, ok := middleware.Session(c)
	if !ok {
		httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}

	// 把当前会话的 hash 传下去：改完密码要保留它，只踢其他会话
	err := h.users.ChangeOwnPassword(c.Request.Context(),
		session.UserID, session.TokenHash, req.OldPassword, req.NewPassword)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"id": session.UserID})
}
