package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Addr   string // HTTP 监听地址
	DBPath string // SQLite 文件路径，相对进程工作目录
	WebDir string // 前端构建产物目录
	Debug  bool   // true 时使用 gin.DebugMode，输出路由与警告日志
}

func Load() Config {
	return Config{
		Addr:   getEnv("APP_ADDR", ":8080"),
		DBPath: getEnv("APP_DB_PATH", "data.db"),
		WebDir: getEnv("APP_WEB_DIR", defaultWebDir()),
		Debug:  getEnv("APP_ENV", "prod") == "dev",
	}
}

// defaultWebDir 优先取「二进制同级目录下的 web/」，这样部署时把二进制和 web/ 放在
// 一起即可，不依赖 systemd 的工作目录设置。
//
// 该目录不存在时回退到相对工作目录的 "web"，覆盖 `go run ./cmd/server` 的场景：
// go run 会把二进制放进临时目录，同级不会有 web/。
func defaultWebDir() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "web")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "web"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
