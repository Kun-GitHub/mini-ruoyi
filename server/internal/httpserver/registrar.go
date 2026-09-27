package httpserver

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/handler"
	"mini-ruoyi/internal/middleware"
	"mini-ruoyi/internal/perm"
)

// Route 是一条已注册的路由，供审计与测试断言。
type Route struct {
	Method string
	Path   string
	// Perm 仅 protected 路由有值。
	Perm perm.Code
	// Reason 仅公开路由需要填，写明「为什么它可以不登录」。
	Reason string
}

// RouteTable 记录三类路由，是「端点是否受保护」的可断言快照。
type RouteTable struct {
	// Public 无需登录。
	Public []Route
	// Self 需要登录，但不需要权限码（操作自己的数据）。
	Self []Route
	// Protected 需要登录 + 指定权限码。
	Protected []Route
}

// ProtectedRoutes 返回所有需要权限码的路由。
func (t *RouteTable) ProtectedRoutes() []Route {
	out := make([]Route, len(t.Protected))
	copy(out, t.Protected)
	return out
}

// PermRoutes 把受保护的路由转成权限清单接口需要的形状。
//
// 放在这里而不是让调用方各自转换：main.go 和测试 fixture 都要用它，
// 两处各写一遍一定会有一处忘记跟着改。
func (t *RouteTable) PermRoutes() []handler.RouteInfo {
	out := make([]handler.RouteInfo, 0, len(t.Protected))
	for _, r := range t.Protected {
		out = append(out, handler.RouteInfo{Method: r.Method, Path: r.Path, Perm: string(r.Perm)})
	}
	return out
}

// Endpoints 返回所有 API 端点的数量，供测试与启动日志使用。
func (t *RouteTable) Endpoints() int {
	return len(t.Public) + len(t.Self) + len(t.Protected)
}

// registrar 是注册 API 端点的唯一入口。
//
// 之所以不直接用 gin 的 group，是为了让「忘记声明权限」在结构上不可能发生：
// 注册业务端点只有 protect 一条路，而它的参数里必须给权限码。
// 想新增一个不校验权限的端点，只能走 open（必须写明理由）或 self（仅登录），
// 两个方法的调用点都很少，且被测试断言了具体集合——新增时会立刻被注意到。
type registrar struct {
	v1     *gin.RouterGroup
	authed *gin.RouterGroup
	table  *RouteTable
}

func newRegistrar(v1, authed *gin.RouterGroup) *registrar {
	return &registrar{v1: v1, authed: authed, table: &RouteTable{}}
}

// protect 注册需要权限码的路由。
//
// 权限码必须是已在 internal/perm 声明过的：写错一个字母就会在启动时 panic，
// 而不是上线后表现为「所有人都没权限」。
func (r *registrar) protect(method, path string, code perm.Code, h gin.HandlerFunc) {
	if !perm.Declared(code) {
		panic(fmt.Sprintf(
			"路由 %s %s 声明了未定义的权限码 %q。请先在 internal/perm 里声明它。",
			method, path, code))
	}
	r.authed.Handle(method, path, middleware.RequirePerm(code), h)
	// 记录完整路径：路由表会被测试直接用来发请求，相对路径会打到 NoRoute 上
	r.table.Protected = append(r.table.Protected, Route{
		Method: method, Path: r.v1.BasePath() + path, Perm: code,
	})
}

// self 注册「只需登录、不需权限码」的路由，例如查看自己的资料、登出。
func (r *registrar) self(method, path string, h gin.HandlerFunc) {
	r.authed.Handle(method, path, h)
	r.table.Self = append(r.table.Self, Route{Method: method, Path: r.v1.BasePath() + path})
}

// open 注册无需登录的路由。reason 是必填的——它会被测试断言，
// 强迫新增公开端点的作者当面解释一次。
func (r *registrar) open(method, path, reason string, h gin.HandlerFunc) {
	if reason == "" {
		panic(fmt.Sprintf("公开路由 %s %s 必须写明理由", method, path))
	}
	r.v1.Handle(method, path, h)
	r.table.Public = append(r.table.Public, Route{
		Method: method, Path: r.v1.BasePath() + path, Reason: reason,
	})
}
