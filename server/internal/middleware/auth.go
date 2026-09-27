package middleware

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/perm"
	"mini-ruoyi/internal/service"
)

const (
	// CookieName 是会话 cookie 名。HttpOnly 让 JS 读不到它，
	// 因此 XSS 也偷不走会话——这正是选 session cookie 而不是 localStorage JWT 的原因之一。
	CookieName = "mr_session"

	ctxIdentity = "auth.identity"
	ctxSession  = "auth.session"
	ctxToken    = "auth.token"
)

// Auth 负责从 cookie 解析会话，并把 Identity 放进请求上下文。
type Auth struct {
	sessions *auth.SessionService
	authz    *service.AuthzService
	// secureCookie 为 true 时给 cookie 加 Secure 标记。
	// 本服务自身不监听 TLS，所以默认 false；若前面有 TLS 终止层，必须打开。
	secureCookie bool
}

func NewAuth(sessions *auth.SessionService, authz *service.AuthzService, secureCookie bool) *Auth {
	return &Auth{sessions: sessions, authz: authz, secureCookie: secureCookie}
}

// Require 挂在使用方分组上：未登录直接 401，不进入业务 handler。
func (m *Auth) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(CookieName)
		if err != nil || token == "" {
			m.reject(c, domain.ErrUnauthorized)
			return
		}

		user, session, err := m.sessions.Resolve(c.Request.Context(), token)
		if err != nil {
			m.reject(c, err)
			return
		}

		identity, err := m.authz.IdentityOf(c.Request.Context(), user)
		if err != nil {
			httpx.FailFromError(c, err)
			c.Abort()
			return
		}

		c.Set(ctxIdentity, identity)
		c.Set(ctxSession, session)
		c.Set(ctxToken, token)
		c.Next()
	}
}

// reject 统一处理会话层面的失败。会话无效时顺手清掉 cookie，
// 否则浏览器会一直带着一个永远失败的 cookie 反复 401。
func (m *Auth) reject(c *gin.Context, err error) {
	if isSessionInvalid(err) {
		m.ClearCookie(c)
	}
	httpx.FailFromError(c, err)
	c.Abort()
}

// CSRF 校验非安全方法上的 X-CSRF-Token 头。
//
// 只在需要登录的路由组上生效：登录接口本身没有会话，无法校验，
// 而「登录 CSRF」的后果只是把用户登进攻击者的账号，对管理后台影响有限。
func (m *Auth) CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		session, ok := Session(c)
		if !ok {
			httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
			c.Abort()
			return
		}
		expected := session.CSRFToken
		got := c.GetHeader("X-CSRF-Token")

		// 常量时间比较，避免通过响应耗时逐字节猜测令牌
		if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(got)) != 1 {
			httpx.Fail(c, http.StatusForbidden, httpx.KeyCSRFInvalid)
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequirePerm 校验权限码。内置管理员直接放行（见 docs/schema.md §8.2）。
func RequirePerm(code perm.Code) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := IdentityOf(c)
		if !ok {
			// 走到这里说明路由少挂了 Auth 中间件，属于编程错误
			httpx.Fail(c, http.StatusUnauthorized, httpx.KeyUnauthorized)
			c.Abort()
			return
		}
		if !identity.Can(string(code)) {
			httpx.Fail(c, http.StatusForbidden, httpx.KeyForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}

// SetCookie 下发会话 cookie。
//
// Path=/ 而不是限定 /api：前端是 SPA，深链接刷新也要能带上会话。
// SameSite=Lax 挡住跨站 POST，再配上 CSRF 头形成双保险。
func (m *Auth) SetCookie(c *gin.Context, token string, maxAgeSeconds int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, token, maxAgeSeconds, "/", "", m.secureCookie, true)
}

func (m *Auth) ClearCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CookieName, "", -1, "/", "", m.secureCookie, true)
}

// Session 取出当前请求的会话。
func Session(c *gin.Context) (domain.Session, bool) {
	v, ok := c.Get(ctxSession)
	if !ok {
		return domain.Session{}, false
	}
	session, ok := v.(domain.Session)
	return session, ok
}

// SessionToken 取出 cookie 里的明文 token，登出时需要它来定位会话行。
func SessionToken(c *gin.Context) (string, bool) {
	v, ok := c.Get(ctxToken)
	if !ok {
		return "", false
	}
	token, ok := v.(string)
	return token, ok
}

// CSRFToken 取出当前会话的 CSRF 令牌。
func CSRFToken(c *gin.Context) (string, bool) {
	session, ok := Session(c)
	if !ok {
		return "", false
	}
	return session.CSRFToken, true
}

// SessionExpiresAt 取出会话过期时间。
func SessionExpiresAt(c *gin.Context) (time.Time, bool) {
	session, ok := Session(c)
	if !ok {
		return time.Time{}, false
	}
	return session.ExpiresAt, true
}

// IdentityOf 取出当前请求的身份。
func IdentityOf(c *gin.Context) (service.Identity, bool) {
	v, ok := c.Get(ctxIdentity)
	if !ok {
		return service.Identity{}, false
	}
	identity, ok := v.(service.Identity)
	return identity, ok
}

// CurrentUser 返回当前登录用户。需要用户名字段（比如上传时记录上传者）时用它。
func CurrentUser(c *gin.Context) (domain.User, bool) {
	identity, ok := IdentityOf(c)
	if !ok {
		return domain.User{}, false
	}
	return identity.User, true
}

// CurrentUserID 返回当前登录用户 id；未登录时返回 (0, false)。
func CurrentUserID(c *gin.Context) (int64, bool) {
	identity, ok := IdentityOf(c)
	if !ok {
		return 0, false
	}
	return identity.User.ID, true
}

func isSessionInvalid(err error) bool {
	return errors.Is(err, domain.ErrUnauthorized) || errors.Is(err, domain.ErrAccountDisabled)
}
