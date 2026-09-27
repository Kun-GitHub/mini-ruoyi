package handler

import (
	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/httpx"
	"mini-ruoyi/internal/perm"
)

// RouteInfo 是「一个权限码守护了哪个接口」。
//
// 由 httpserver 的路由表提供，不查数据库：权限点的真源是代码，
// 数据库里存的只是「哪个角色拥有它」。
type RouteInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Perm   string `json:"perm"`
}

// PermHandler 提供权限清单，给授权界面与「API 权限」页面用。
type PermHandler struct {
	// routes 是延迟取值：路由表由 NewRouter 在装配时生成，而 /perms 本身
	// 也要在装配期注册，所以传函数而不是值。
	routes func() []RouteInfo
}

func NewPermHandler(routes func() []RouteInfo) *PermHandler {
	return &PermHandler{routes: routes}
}

// Catalogue 返回全部权限点，按资源分组，并带上每个权限点保护的接口。
//
// 带上接口列表是为了让「这个权限到底是干什么的」一眼可见——
// 只显示「查询用户」这种文案时，没人知道它对应 GET /api/v1/users，
// 排查「为什么这个角色还能调某个接口」就只能去翻代码。
func (h *PermHandler) Catalogue(c *gin.Context) {
	var routes []RouteInfo
	if h.routes != nil {
		routes = h.routes()
	}

	byCode := map[string][]RouteInfo{}
	for _, r := range routes {
		if r.Perm == "" {
			continue
		}
		byCode[r.Perm] = append(byCode[r.Perm], r)
	}

	groups := make([]gin.H, 0)
	for _, g := range perm.Groups() {
		items := make([]gin.H, 0, len(g.Codes))
		for _, code := range g.Codes {
			endpoints := byCode[string(code)]
			if endpoints == nil {
				endpoints = []RouteInfo{}
			}
			items = append(items, gin.H{
				"code":      string(code),
				"label_key": perm.LabelKey(code),
				"endpoints": endpoints,
			})
		}
		groups = append(groups, gin.H{"title_key": g.TitleKey, "perms": items})
	}

	httpx.Success(c, gin.H{"groups": groups})
}
