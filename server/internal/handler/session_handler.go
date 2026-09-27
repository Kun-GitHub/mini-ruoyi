package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/middleware"
)

type SessionHandler struct {
	sessions *auth.SessionService
}

func NewSessionHandler(sessions *auth.SessionService) *SessionHandler {
	return &SessionHandler{sessions: sessions}
}

// List 返回在线会话（未过期的），最近活跃的在前。
func (h *SessionHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c)

	result, err := h.sessions.List(c.Request.Context(), page, pageSize)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, result)
}

// Kill 踢掉一条会话。
//
// 标识用的是 token_hash——它是会话表的主键，也是浏览器里那个明文 token 的
// SHA-256。暴露哈希不构成风险：它反推不出 token，也不能拿去认证。
func (h *SessionHandler) Kill(c *gin.Context) {
	current, ok := middleware.Session(c)
	if !ok {
		httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
		return
	}

	if err := h.sessions.Kick(c.Request.Context(), c.Param("hash"), current.TokenHash); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"ok": true})
}

// KillUserSessions 踢掉某个用户的全部会话（若依的「强退」）。
//
// 用户列表上也会用到它，所以路径挂在 /users/:id 下而不是 /sessions 下。
func (h *SessionHandler) KillUserSessions(c *gin.Context) {
	id, err := pathID(c)
	if err != nil {
		return
	}

	current, ok := middleware.Session(c)
	if !ok {
		httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
		return
	}
	// 对自己强制下线只会让人莫名被登出，且他本来就有退出按钮
	if id == current.UserID {
		httpx.FailFromError(c, domain.ErrCannotKickSelf)
		return
	}

	if err := h.sessions.RevokeUser(c.Request.Context(), id); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, gin.H{"user_id": id})
}
