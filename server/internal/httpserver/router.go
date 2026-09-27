package httpserver

import (
	"database/sql"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/handler"
	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/perm"
)

const (
	defaultRateLimitRPS   = 20
	defaultRateLimitBurst = 40

	rateLimitTTL = 3 * time.Minute
	maxBodyBytes = 1 << 20 // 1MiB
)

// Deps 是路由装配所需的依赖。
type Deps struct {
	DB      *sql.DB
	Auth    *handler.AuthHandler
	User    *handler.UserHandler
	Role    *handler.RoleHandler
	Menu    *handler.MenuHandler
	Perm    *handler.PermHandler
	Job     *handler.JobHandler
	File    *handler.FileHandler
	Session *handler.SessionHandler
	Log     *handler.LogHandler
	// OperLog 由 service.LogService 实现，用于记录写操作审计
	OperLog middleware.OperLogRecorder
	AuthMW  *middleware.Auth
	WebDir  string
	// TrustedProxies 为空表示不信任任何反向代理（默认）。
	// 部署在 nginx 后面时必须填上代理地址，否则按 IP 限流会退化成全局限流。
	TrustedProxies []string
	// RateLimitRPS / RateLimitBurst 为 0 时用默认值（20 rps / burst 40）。
	RateLimitRPS   float64
	RateLimitBurst int
}

// validate 保证依赖已全部注入。
//
// 少注入一个 handler 的表现是「某些接口 panic 后被 Recovery 兜成 500」，
// 排查起来会往业务逻辑上找，实际只是装配遗漏。所以在启动阶段直接拒绝。
func (d Deps) validate() error {
	v := reflect.ValueOf(d)
	for i := 0; i < v.NumField(); i++ {
		if f := v.Field(i); f.Kind() == reflect.Ptr && f.IsNil() {
			return fmt.Errorf("Deps.%s 未注入（装配遗漏，会导致对应接口 500）", v.Type().Field(i).Name)
		}
	}
	return nil
}

// NewRouter 组装路由，同时返回路由表供审计与测试断言。
//
// 依赖不完整或前端产物目录不可用时直接返回错误，让进程启动即失败，
// 而不是等用户访问了才发现。
func NewRouter(deps Deps) (*gin.Engine, *RouteTable, error) {
	if err := deps.validate(); err != nil {
		return nil, nil, err
	}

	// 校验错误的字段名用 json tag 输出，需在注册路由前生效
	httpx.RegisterJSONFieldNames()

	r := gin.New()
	r.Use(middleware.Recovery(), middleware.RequestLogger())

	// 默认为空 = 不信任任何 X-Forwarded-For。
	// 本服务可以直接对外监听，无条件采信该头会让客户端能伪装成任意 IP，
	// 按 IP 限流和登录 IP 记录都会失真。
	// 部署在反向代理后面时，用 APP_TRUSTED_PROXIES 声明代理地址。
	if err := r.SetTrustedProxies(deps.TrustedProxies); err != nil {
		return nil, nil, fmt.Errorf("APP_TRUSTED_PROXIES 配置无效（%v）: %w", deps.TrustedProxies, err)
	}

	r.Use(
		// /files 自己控制上限（上传要放行到 APP_UPLOAD_MAX_MB），
		// 这里把它从全局的 1 MiB 限制里排掉
		middleware.BodyLimit(maxBodyBytes, "/api/v1/files"),
		middleware.RateLimit(rateLimit(deps.RateLimitRPS, defaultRateLimitRPS), rateLimit(deps.RateLimitBurst, defaultRateLimitBurst), rateLimitTTL),
		middleware.ImmutableAssets(AssetsPrefix),
	)

	r.GET("/healthz", func(c *gin.Context) {
		if err := deps.DB.PingContext(c.Request.Context()); err != nil {
			httpx.Fail(c, http.StatusServiceUnavailable, httpx.KeyServiceUnavailable)
			return
		}
		httpx.Success(c, gin.H{"status": "up"})
	})

	v1 := r.Group("/api/v1")
	// 顺序即执行顺序：登录 → CSRF → 操作日志 → 权限校验 → 业务 handler。
	// 操作日志必须排在权限校验之前，这样 403（越权试探）也会被记下来。
	authed := v1.Group("",
		deps.AuthMW.Require(),
		deps.AuthMW.CSRF(),
		middleware.OperationLog(deps.OperLog),
	)
	reg := newRegistrar(v1, authed)

	// ---- 公开端点 ----
	// 只允许一个：登录接口不可能要求「已经登录」。
	// 新增公开端点会让 TestPublicEndpointsAreMinimal 失败，是刻意的减速带。
	reg.open(http.MethodPost, "/auth/login", "登录是获得会话的前提，不可能要求已登录", deps.Auth.Login)

	// ---- 仅需登录（操作自己的数据） ----
	reg.self(http.MethodGet, "/auth/me", deps.Auth.Me)
	reg.self(http.MethodPost, "/auth/logout", deps.Auth.Logout)

	// ---- 菜单 ----
	reg.protect(http.MethodGet, "/menus", perm.SystemMenuList, deps.Menu.Tree)
	reg.protect(http.MethodGet, "/menus/:id", perm.SystemMenuList, deps.Menu.Get)
	reg.protect(http.MethodPost, "/menus", perm.SystemMenuAdd, deps.Menu.Create)
	reg.protect(http.MethodPut, "/menus/:id", perm.SystemMenuEdit, deps.Menu.Update)
	reg.protect(http.MethodDelete, "/menus/:id", perm.SystemMenuDelete, deps.Menu.Delete)

	// ---- 角色 ----
	// 读授权用 list 权限、写授权用 edit 权限：能看授权和能改授权是两件事。
	reg.protect(http.MethodGet, "/roles", perm.SystemRoleList, deps.Role.List)
	reg.protect(http.MethodGet, "/roles/:id", perm.SystemRoleList, deps.Role.Get)
	reg.protect(http.MethodGet, "/roles/:id/grants", perm.SystemRoleList, deps.Role.Grants)
	reg.protect(http.MethodPost, "/roles", perm.SystemRoleAdd, deps.Role.Create)
	reg.protect(http.MethodPut, "/roles/:id", perm.SystemRoleEdit, deps.Role.Update)
	reg.protect(http.MethodPut, "/roles/:id/grants", perm.SystemRoleEdit, deps.Role.SetGrants)
	reg.protect(http.MethodDelete, "/roles/:id", perm.SystemRoleDelete, deps.Role.Delete)

	// 权限清单本身要单独授权，不能因为「能进后台」就看到全部权限点
	reg.protect(http.MethodGet, "/perms", perm.SystemPermList, deps.Perm.Catalogue)

	// ---- 用户 ----
	reg.protect(http.MethodGet, "/users", perm.SystemUserList, deps.User.List)
	reg.protect(http.MethodGet, "/users/:id", perm.SystemUserList, deps.User.Get)
	reg.protect(http.MethodPost, "/users", perm.SystemUserAdd, deps.User.Create)
	reg.protect(http.MethodPut, "/users/:id", perm.SystemUserEdit, deps.User.Update)
	reg.protect(http.MethodPut, "/users/:id/roles", perm.SystemUserEdit, deps.User.SetRoles)
	// 重设密码单独授权：它能让持有者冒充任意用户，比「改资料」危险得多
	reg.protect(http.MethodPut, "/users/:id/password", perm.SystemUserResetPwd, deps.User.ResetPassword)
	reg.protect(http.MethodDelete, "/users/:id", perm.SystemUserDelete, deps.User.Delete)

	// ---- 系统工具 ----
	// 任务清单以**代码注册表**为准（数据库里可能残留已删除任务），所以没有新增/删除接口。
	reg.protect(http.MethodGet, "/jobs", perm.ToolJobList, deps.Job.List)
	reg.protect(http.MethodPut, "/jobs/:key", perm.ToolJobEdit, deps.Job.Update)
	// 手动触发与改 cron 分开授权：能改调度时间不等于能立刻把任务跑起来
	reg.protect(http.MethodPost, "/jobs/:key/run", perm.ToolJobRun, deps.Job.RunNow)

	reg.protect(http.MethodGet, "/files", perm.ToolFileList, deps.File.List)
	// 下载也走权限校验：上传的文件**不能静态挂载**，
	// 那会绕过鉴权，任何人拼一个 URL 就能拿到别人上传的东西
	reg.protect(http.MethodGet, "/files/:id/download", perm.ToolFileList, deps.File.Download)
	reg.protect(http.MethodPost, "/files", perm.ToolFileUpload, deps.File.Upload)
	reg.protect(http.MethodDelete, "/files/:id", perm.ToolFileDelete, deps.File.Delete)

	// ---- 系统监控 ----
	reg.protect(http.MethodGet, "/sessions", perm.MonitorSessionList, deps.Session.List)
	reg.protect(http.MethodDelete, "/sessions/:hash", perm.MonitorSessionKick, deps.Session.Kill)
	// 强退挂在用户下：入口在用户列表上，不在会话列表里
	reg.protect(http.MethodDelete, "/users/:id/sessions", perm.MonitorSessionKick, deps.Session.KillUserSessions)

	reg.protect(http.MethodGet, "/login-logs", perm.MonitorLoginLogList, deps.Log.ListLoginLogs)
	reg.protect(http.MethodGet, "/oper-logs", perm.MonitorOperLogList, deps.Log.ListOperLogs)

	if err := MountStatic(r, deps.WebDir); err != nil {
		return nil, nil, err
	}
	return r, reg.table, nil
}

// rateLimit 在未配置时回落到默认值。0 或负数都视为「没配」。
func rateLimit[T int | float64](configured, fallback T) T {
	if configured <= 0 {
		return fallback
	}
	return configured
}
