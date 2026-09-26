package config

import "os"

type Config struct {
	Addr   string // HTTP 监听地址
	DBPath string // SQLite 文件路径
}

func Load() Config {
	return Config{
		Addr:   getEnv("APP_ADDR", ":8080"),
		DBPath: getEnv("APP_DB_PATH", "data.db"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
