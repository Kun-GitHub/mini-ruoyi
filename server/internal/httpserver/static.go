package httpserver

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/handler"
)

const AssetsPrefix = "/assets/"

// MountStatic 从磁盘目录挂载前端构建产物。
//
// 前端产物不 embed 进二进制，因此重新构建前端后无需重启后端，浏览器刷新即可生效。
// 代价是 index.html 必须不可缓存，否则浏览器会继续引用已不存在的旧 asset 哈希。
func MountStatic(r *gin.Engine, webDir string) error {
	indexPath := filepath.Join(webDir, "index.html")
	assetsDir := filepath.Join(webDir, "assets")

	// 整个目录不存在 = 这是一个纯 API 部署，只警告不阻断启动
	serveSPA := false
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		log.Printf("前端目录 %s 不存在，仅提供 API", webDir)
	} else { // 目录存在但结构不完整 = 部署配置错了，启动即失败好过运行期 404
		if _, err := os.Stat(indexPath); err != nil {
			return fmt.Errorf("前端目录 %q 缺少 index.html: %w", webDir, err)
		}
		if _, err := os.Stat(assetsDir); err != nil {
			return fmt.Errorf("前端目录 %q 缺少 assets/: %w", webDir, err)
		}
		serveSPA = true
		log.Printf("从 %s 托管前端", webDir)
		r.StaticFS(AssetsPrefix, http.Dir(assetsDir))
	}

	r.NoRoute(func(c *gin.Context) {
		// API 路径和缺失的静态资源绝不能回落到 index.html：
		// 否则前端会把一整页 HTML 当 JSON 解析，报错信息完全看不出真正原因
		if !serveSPA || strings.HasPrefix(c.Request.URL.Path, "/api/") ||
			strings.HasPrefix(c.Request.URL.Path, AssetsPrefix) {
			handler.Fail(c, http.StatusNotFound, "not found")
			return
		}
		// SPA fallback：刷新 /system/user 这类前端路由不返回 404。
		// no-cache 让浏览器每次回源校验，从而拿到最新的 asset 哈希。
		c.Header("Cache-Control", "no-cache")
		c.File(indexPath)
	})
	return nil
}
