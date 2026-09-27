package handler

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/service"
)

// LoginLogRecorder 由 service.LogService 实现。
// 在这里声明接口而不是直接依赖 service：handler 只描述自己需要的能力。
type LoginLogRecorder interface {
	RecordLogin(domain.LoginLog)
}

// CookieWriter 由 middleware.Auth 实现。
//
// 在这里声明接口而不是 import middleware：handler 不需要知道中间件的存在，
// 而中间件本来就不该被业务层依赖。
type CookieWriter interface {
	SetCookie(c *gin.Context, token string, maxAgeSeconds int)
	ClearCookie(c *gin.Context)
}

type AuthHandler struct {
	sessions *auth.SessionService
	authz    *service.AuthzService
	menus    *service.MenuService
	cookie   CookieWriter
	logs     LoginLogRecorder
}

func NewAuthHandler(
	sessions *auth.SessionService,
	authz *service.AuthzService,
	menus *service.MenuService,
	cookie CookieWriter,
	logs LoginLogRecorder,
) *AuthHandler {
	return &AuthHandler{sessions: sessions, authz: authz, menus: menus, cookie: cookie, logs: logs}
}

// sessionPayload 是登录与 /me 共用的响应体。
//
// 登录后直接返回它，前端不用再补一次 /me 请求。
type sessionPayload struct {
	User      domain.User        `json:"user"`
	IsAdmin   bool               `json:"is_admin"`
	Perms     []string           `json:"perms"`
	CSRFToken string             `json:"csrf_token"`
	ExpiresAt time.Time          `json:"expires_at"`
	Menus     []*domain.MenuNode `json:"menus"`
}

// Password 用 max=72 而不是更长：bcrypt 对超过 72 字节的输入直接报错，
// 不在这里挡住的话用户会拿到 500 而不是「密码太长」。
type loginRequest struct {
	Username string `json:"username" binding:"required,max=128"`
	Password string `json:"password" binding:"required,max=72"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.FailBindError(c, err)
		return
	}

	result, err := h.sessions.Login(c.Request.Context(),
		req.Username, req.Password, c.ClientIP(), c.Request.UserAgent())

	// 成功与失败都记。只记成功的话，「有人在暴力破解」就完全看不出来。
	h.recordLogin(c, req.Username, err)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}

	identity, err := h.authz.IdentityOf(c.Request.Context(), result.User)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	payload, err := h.buildPayload(c.Request.Context(), identity, result.Session)
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}

	h.cookie.SetCookie(c, result.Token, int(auth.SessionTTL.Seconds()))
	httpx.Success(c, payload)
}

// recordLogin 记录一次登录尝试。
//
// 失败的 reason 用错误键（如 error.badCredentials），与响应体保持一致，
// 这样日志和界面上的提示能对上。
func (h *AuthHandler) recordLogin(c *gin.Context, username string, err error) {
	if h.logs == nil {
		return
	}

	entry := domain.LoginLog{
		Username:  username,
		Status:    domain.LoginSuccess,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	if err != nil {
		entry.Status = domain.LoginFailed
		entry.Reason = httpx.ErrorKeyOf(err)
	}
	h.logs.RecordLogin(entry)
}

// Me 返回当前登录用户的资料、权限、菜单与 CSRF 令牌。
//
// 前端刷新页面后靠它恢复状态：会话在 cookie 里，但 CSRF 令牌只在内存里。
func (h *AuthHandler) Me(c *gin.Context) {
	identity, ok := middleware.IdentityOf(c)
	if !ok {
		httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
		return
	}
	csrf, _ := middleware.CSRFToken(c)
	expiresAt, _ := middleware.SessionExpiresAt(c)

	payload, err := h.buildPayload(c.Request.Context(), identity, domain.Session{
		CSRFToken: csrf,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		httpx.FailFromError(c, err)
		return
	}
	httpx.Success(c, payload)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	token, _ := middleware.SessionToken(c)
	if err := h.sessions.Logout(c.Request.Context(), token); err != nil {
		httpx.FailFromError(c, err)
		return
	}
	h.cookie.ClearCookie(c)
	httpx.Success(c, gin.H{"ok": true})
}

func (h *AuthHandler) buildPayload(ctx context.Context, identity service.Identity, session domain.Session) (sessionPayload, error) {
	perms := make([]string, 0, len(identity.Perms))
	for code := range identity.Perms {
		perms = append(perms, code)
	}
	sort.Strings(perms) // 顺序稳定，便于前端 diff 与测试断言

	menus, err := h.menus.TreeForIdentity(ctx, identity)
	if err != nil {
		return sessionPayload{}, err
	}

	return sessionPayload{
		User:      identity.User,
		IsAdmin:   identity.IsAdmin,
		Perms:     perms,
		CSRFToken: session.CSRFToken,
		ExpiresAt: session.ExpiresAt,
		Menus:     menus,
	}, nil
}
