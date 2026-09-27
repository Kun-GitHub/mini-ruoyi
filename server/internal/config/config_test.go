package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig 写一个临时配置文件并把 APP_CONFIG 指过去。
// 必须显式指定路径：默认路径是相对工作目录的 config/config.yaml，
// 而测试的工作目录是包目录，会读到真实文件。
func writeConfig(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_CONFIG", path)
}

func TestDefaultsWhenNoConfigFile(t *testing.T) {
	// 指向一个不存在的路径：配置文件缺失不是错误，
	// 「clone 下来直接跑」这条性质靠的就是它
	t.Setenv("APP_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("addr = %q，期望 :8080", cfg.Server.Addr)
	}
	if cfg.Upload.MaxMB != 20 || cfg.Upload.QuotaMB != 512 {
		t.Errorf("upload = %+v，期望 20/512", cfg.Upload)
	}
	if cfg.Log.RetentionDays != 30 {
		t.Errorf("retention_days = %d，期望 30", cfg.Log.RetentionDays)
	}
	if cfg.Web.Dir == "" {
		t.Error("web.dir 留空时应自动查找到一个具体路径")
	}
}

func TestFileOverridesDefaultsPartially(t *testing.T) {
	// 只写要改的项，其余保持默认——这是配置文件的主要用法
	writeConfig(t, `
upload:
  max_mb: 5
`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Upload.MaxMB != 5 {
		t.Errorf("max_mb = %d，期望 5", cfg.Upload.MaxMB)
	}
	// 没写的项保持默认
	if cfg.Upload.QuotaMB != 512 {
		t.Errorf("quota_mb = %d，期望保持默认 512", cfg.Upload.QuotaMB)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("addr = %q，期望保持默认 :8080", cfg.Server.Addr)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	writeConfig(t, `
server:
  addr: ":9090"
  rate_limit:
    rps: 5
`)
	t.Setenv("APP_ADDR", ":7070")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":7070" {
		t.Errorf("addr = %q，期望被环境变量覆盖成 :7070", cfg.Server.Addr)
	}
	// 没设环境变量的项仍来自文件
	if cfg.Server.RateLimit.RPS != 5 {
		t.Errorf("rps = %v，期望来自文件里的 5", cfg.Server.RateLimit.RPS)
	}
}

// TestUnknownKeyInFileIsRejected 覆盖配置项写错名字。
//
// 不报错的话（比如 upload.max_size 写成 max_size），用户只会觉得
// 「我明明配了怎么不生效」，而这种问题极难往配置上想。
func TestUnknownKeyInFileIsRejected(t *testing.T) {
	writeConfig(t, `
upload:
  max_size: 5
`)
	_, err := Load()
	if err == nil {
		t.Fatal("配置文件里出现未知字段时应报错")
	}
	if !strings.Contains(err.Error(), "max_size") {
		t.Errorf("错误信息未指出是哪个字段: %v", err)
	}
}

// TestInvalidEnvValueIsRejected 覆盖环境变量取值非法。
//
// 静默忽略的话，一个打错的 APP_UPLOAD_MAX_MB 会安静地退回默认值，
// 同样表现为「我明明设了怎么不生效」。
func TestInvalidEnvValueIsRejected(t *testing.T) {
	t.Setenv("APP_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	t.Setenv("APP_UPLOAD_MAX_MB", "二十")

	_, err := Load()
	if err == nil {
		t.Fatal("环境变量不是数字时应报错")
	}
	if !strings.Contains(err.Error(), "APP_UPLOAD_MAX_MB") {
		t.Errorf("错误信息未指出是哪个变量: %v", err)
	}
}

// TestValidation 覆盖几条「能跑起来但必然出问题」的配置。
func TestValidation(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"配额小于单文件上限", "upload:\n  max_mb: 100\n  quota_mb: 10\n", "quota_mb"},
		{"限流速率为零", "server:\n  rate_limit:\n    rps: 0\n", "rps"},
		{"保留天数为零", "log:\n  retention_days: 0\n", "retention_days"},
		{"上传目录为空", "upload:\n  dir: \"\"\n", "dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeConfig(t, tc.yaml)
			_, err := Load()
			if err == nil {
				t.Fatal("应当报错")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("错误信息里没有 %q: %v", tc.wantErr, err)
			}
		})
	}
}

func TestEmptyConfigFileIsValid(t *testing.T) {
	writeConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("空配置文件应当等价于「全用默认值」: %v", err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("addr = %q，期望默认值", cfg.Server.Addr)
	}
}

// TestDefaultWebDirFindsBinWebFromServerDir 覆盖 IDE / `go run` 场景。
//
// 这两者把二进制放在临时目录，工作目录却是 server/ 或仓库根，
// 于是「二进制同级目录」根本不存在。之前只找那一个位置，
// 找不到就静默降级成纯 API 模式——表现为「打开 :8080 看到一句 JSON 404」，
// 很难联想到是前端目录的问题。
func TestDefaultWebDirFindsBinWebFromServerDir(t *testing.T) {
	repoRoot := t.TempDir()
	binWeb := filepath.Join(repoRoot, "bin", "web")
	if err := os.MkdirAll(binWeb, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binWeb, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	serverDir := filepath.Join(repoRoot, "server")
	if err := os.MkdirAll(serverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, serverDir)

	got := defaultWebDir()
	want := filepath.Join("..", "bin", "web")
	if got != want {
		t.Errorf("工作目录为 server/ 时 defaultWebDir() = %q，期望 %q", got, want)
	}
}

func TestDefaultWebDirPrefersWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	if got := defaultWebDir(); got != "web" {
		t.Errorf("defaultWebDir() = %q，期望 \"web\"", got)
	}
}

func TestDefaultWebDirFallsBackToFirstCandidate(t *testing.T) {
	chdir(t, t.TempDir())

	if got := defaultWebDir(); got == "" {
		t.Error("都不存在时不应返回空字符串（错误信息会失去可读的位置）")
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}
