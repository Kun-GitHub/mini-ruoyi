// Package config 负责加载配置。
//
// 优先级：**环境变量 > 配置文件 > 内置默认值**。
// 环境变量优先是为了让容器与 systemd 能覆盖单个值，而不必去改文件；
// 文件存在与否都不影响启动，所以「clone 下来直接跑」这条性质保持住了。
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath 是配置文件的默认位置，相对进程工作目录。
//
// 这个路径在 .gitignore 里：它含部署相关信息（监听地址、代理地址），
// 每个部署各不相同。可提交的样例见同目录的 config.example.yaml。
const DefaultConfigPath = "config/config.yaml"

type Config struct {
	Server   Server   `yaml:"server"`
	Database Database `yaml:"database"`
	Web      Web      `yaml:"web"`
	Upload   Upload   `yaml:"upload"`
	Log      Log      `yaml:"log"`
}

type Server struct {
	Addr string `yaml:"addr"`
	// Env 为 "dev" 时使用 gin 的调试模式
	Env string `yaml:"env"`
	// SecureCookie 给会话 cookie 加 Secure 标记（仅 HTTPS 传输）。
	// 本服务自身不监听 TLS，所以默认 false；前面有 TLS 终止层时必须打开。
	SecureCookie bool `yaml:"secure_cookie"`
	// TrustedProxies 是可信反向代理的地址（IP 或 CIDR）。
	//
	// 默认必须为空：本服务可以直接对外监听，无条件采信 X-Forwarded-For
	// 会让客户端随便伪装成任意 IP，按 IP 限流形同虚设。
	//
	// 但反过来，**nginx 前置时必须配置它**，否则所有请求的来源 IP 都是
	// 127.0.0.1——按 IP 限流会退化成全局限流，登录日志里的 IP 也全是代理地址。
	TrustedProxies []string  `yaml:"trusted_proxies"`
	RateLimit      RateLimit `yaml:"rate_limit"`
}

type RateLimit struct {
	// RPS 是每个 IP 的限流速率，Burst 是允许的瞬时突发量。
	// 批量运维操作（导入用户、批删）被自己的限流挡住时调大这两个值。
	RPS   float64 `yaml:"rps"`
	Burst int     `yaml:"burst"`
}

type Database struct {
	// Path 是 SQLite 文件路径，相对进程工作目录。
	Path string `yaml:"path"`
}

type Web struct {
	// Dir 是前端构建产物目录。**留空表示自动查找**：
	// 二进制同级 web/ → ./web → ../bin/web → bin/web。
	// 自动查找是为了让 go run 和 IDE 启动也能找到产物（那时二进制在临时目录里）。
	Dir string `yaml:"dir"`
}

type Upload struct {
	Dir string `yaml:"dir"`
	// MaxMB 是单文件上限，QuotaMB 是所有文件的总容量上限。
	MaxMB   int64 `yaml:"max_mb"`
	QuotaMB int64 `yaml:"quota_mb"`
}

type Log struct {
	// RetentionDays 是操作日志与登录日志的保留天数。
	// 日志表只增不改，不设上限迟早会把磁盘吃满。
	RetentionDays int `yaml:"retention_days"`
}

// Load 按「默认值 → 配置文件 → 环境变量」的顺序组装配置，并做校验。
func Load() (Config, error) {
	cfg := defaults()

	if err := loadFile(&cfg); err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}

	// 前端目录留空表示自动查找。放在这里而不是默认值里：
	// 文件或环境变量都可能把它显式设成空来「恢复自动」。
	if cfg.Web.Dir == "" {
		cfg.Web.Dir = defaultWebDir()
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaults() Config {
	return Config{
		Server: Server{
			Addr:      ":8080",
			Env:       "prod",
			RateLimit: RateLimit{RPS: 20, Burst: 40},
		},
		Database: Database{Path: "data.db"},
		Upload:   Upload{Dir: "uploads", MaxMB: 20, QuotaMB: 512},
		Log:      Log{RetentionDays: 30},
	}
}

// loadFile 读取配置文件。文件不存在不是错误——那样才能「clone 下来直接跑」。
func loadFile(cfg *Config) error {
	path := getEnv("APP_CONFIG", DefaultConfigPath)

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("打开配置文件 %s: %w", path, err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	// 未知字段直接报错。少了这个，配置里写错一个键名（比如 upload.max_size）
	// 不会有任何提示，用户只会觉得「我明明配了怎么不生效」。
	dec.KnownFields(true)

	if err := dec.Decode(cfg); err != nil {
		// 空文件是合法的：全部走默认值
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("解析配置文件 %s: %w", path, err)
	}
	return nil
}

// applyEnv 用环境变量覆盖文件里的值。只覆盖显式设置了的环境变量。
//
// 取值非法一律报错而不是忽略：静默忽略的话，一个打错的 APP_UPLOAD_MAX_MB
// 会安静地退回默认值，用户只会觉得「我明明设了怎么不生效」。
func applyEnv(c *Config) error {
	var bad []string

	// 字符串项直接覆盖
	setStr("APP_ADDR", &c.Server.Addr)
	setStr("APP_ENV", &c.Server.Env)
	setStr("APP_DB_PATH", &c.Database.Path)
	setStr("APP_WEB_DIR", &c.Web.Dir)
	setStr("APP_UPLOAD_DIR", &c.Upload.Dir)

	// 数值/布尔项解析失败要记下来
	setNum("APP_SECURE_COOKIE", &c.Server.SecureCookie, parseBool, &bad)
	setNum("APP_RATE_LIMIT_RPS", &c.Server.RateLimit.RPS, parseFloat, &bad)
	setNum("APP_RATE_LIMIT_BURST", &c.Server.RateLimit.Burst, parseInt, &bad)
	setNum("APP_UPLOAD_MAX_MB", &c.Upload.MaxMB, parseInt64, &bad)
	setNum("APP_UPLOAD_QUOTA_MB", &c.Upload.QuotaMB, parseInt64, &bad)
	setNum("APP_LOG_RETENTION_DAYS", &c.Log.RetentionDays, parseInt, &bad)

	if v := os.Getenv("APP_TRUSTED_PROXIES"); v != "" {
		c.Server.TrustedProxies = splitList(v)
	}

	if len(bad) > 0 {
		return fmt.Errorf("环境变量取值非法: %s", strings.Join(bad, ", "))
	}
	return nil
}

// Validate 校验配置。配置错误必须让进程起不来——
// 带着一个错值跑起来，问题会在很久之后以完全无关的形式出现。
func (c Config) Validate() error {
	if strings.TrimSpace(c.Server.Addr) == "" {
		return errors.New("server.addr 不能为空")
	}
	if c.Server.RateLimit.RPS <= 0 {
		return fmt.Errorf("server.rate_limit.rps 必须大于 0，当前 %v", c.Server.RateLimit.RPS)
	}
	if c.Server.RateLimit.Burst <= 0 {
		return fmt.Errorf("server.rate_limit.burst 必须大于 0，当前 %d", c.Server.RateLimit.Burst)
	}
	if strings.TrimSpace(c.Database.Path) == "" {
		return errors.New("database.path 不能为空")
	}
	if strings.TrimSpace(c.Upload.Dir) == "" {
		return errors.New("upload.dir 不能为空")
	}
	if c.Upload.MaxMB <= 0 {
		return fmt.Errorf("upload.max_mb 必须大于 0，当前 %d", c.Upload.MaxMB)
	}
	if c.Upload.QuotaMB <= 0 {
		return fmt.Errorf("upload.quota_mb 必须大于 0，当前 %d", c.Upload.QuotaMB)
	}
	// 配额比单文件上限还小的话，一个文件都传不上去——
	// 这种配置能跑起来但必然失败，不如直接拒绝
	if c.Upload.QuotaMB < c.Upload.MaxMB {
		return fmt.Errorf("upload.quota_mb（%d）不能小于 upload.max_mb（%d），否则一个文件都传不上去",
			c.Upload.QuotaMB, c.Upload.MaxMB)
	}
	if c.Log.RetentionDays <= 0 {
		return fmt.Errorf("log.retention_days 必须大于 0，当前 %d", c.Log.RetentionDays)
	}
	return nil
}

// Debug 为 true 时使用 gin 的调试模式。
func (c Config) Debug() bool { return c.Server.Env == "dev" }

// UploadMaxBytes / UploadQuotaBytes 把 MiB 换算成字节，供存储层使用。
func (c Config) UploadMaxBytes() int64   { return c.Upload.MaxMB << 20 }
func (c Config) UploadQuotaBytes() int64 { return c.Upload.QuotaMB << 20 }

// Summary 是启动时打印的一行摘要。
//
// 刻意手写而不是直接 dump 整个结构体：以后加了密钥类配置项时，
// dump 会把它打进日志，而手写的摘要不会。
//
// 路径一律显示**绝对路径**：「数据在哪」「文件传到哪」是运维最常问的两个问题，
// 而配置里写的 `data.db`、`uploads` 是相对工作目录的，光看它答不上来。
func (c Config) Summary(absUploadDir string) string {
	proxies := "无"
	if len(c.Server.TrustedProxies) > 0 {
		proxies = strings.Join(c.Server.TrustedProxies, ",")
	}
	return fmt.Sprintf(
		"监听 %s（%s）| 数据库 %s | 前端 %s | 上传 %s（单文件 %d MiB / 共 %d MiB）| 日志保留 %d 天 | 可信代理 %s",
		c.Server.Addr, c.Server.Env, absPath(c.Database.Path), absPath(c.Web.Dir), absUploadDir,
		c.Upload.MaxMB, c.Upload.QuotaMB, c.Log.RetentionDays, proxies)
}

// absPath 尽量转成绝对路径；失败时原样返回（摘要不该因为它报错）。
func absPath(p string) string {
	if p == "" {
		return "(未配置)"
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// defaultWebDir 自动查找前端产物目录，取第一个含 index.html 的候选。
//
// 只看「二进制同级」是不够的：
//   - 部署时二进制和 web/ 放一起 → 命中第一个
//   - `go run` / IDE 启动时，二进制在临时目录，工作目录可能是仓库根或 server/
//     → 需要后两个候选
//
// 都找不到时返回首个候选，让 MountStatic 的报错信息有个具体路径。
func defaultWebDir() string {
	candidates := webDirCandidates()
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
			return dir
		}
	}
	return candidates[0]
}

func webDirCandidates() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), "web"))
	}
	return append(out,
		"web",                             // 工作目录就是仓库根
		filepath.Join("..", "bin", "web"), // 工作目录是 server/（IDE 与 go run）
		filepath.Join("bin", "web"),
	)
}

// splitList 解析逗号分隔的列表，忽略空白项。
func splitList(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func setStr(key string, dst *string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

// setNum 读一个数值/布尔环境变量。包级函数而非闭包：Go 不允许泛型函数字面量。
func setNum[T any](key string, dst *T, parse func(string) (T, error), bad *[]string) {
	v := os.Getenv(key)
	if v == "" {
		return
	}
	got, err := parse(strings.TrimSpace(v))
	if err != nil {
		*bad = append(*bad, fmt.Sprintf("%s=%q", key, v))
		return
	}
	*dst = got
}

func parseInt(s string) (int, error)       { return strconv.Atoi(s) }
func parseInt64(s string) (int64, error)   { return strconv.ParseInt(s, 10, 64) }
func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
func parseBool(s string) (bool, error)     { return strconv.ParseBool(s) }
