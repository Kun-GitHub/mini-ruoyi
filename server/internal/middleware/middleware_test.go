package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newEngine(mw gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// 与实际路由一致：本服务无反向代理，不信任任何 X-Forwarded-For
	_ = r.SetTrustedProxies(nil)
	r.Use(mw)
	r.GET("/t", func(c *gin.Context) { c.Status(http.StatusOK) })
	// 模拟真实 handler：必须真的读 body，MaxBytesReader 才会在超限时报错
	r.POST("/t", func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusOK)
	})
	return r
}

func request(r *gin.Engine, method, remoteAddr, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/t", reader)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestRateLimitIsPerIP 覆盖 B5：全进程令牌桶会让单个客户端把其他所有人一起打成 429。
func TestRateLimitIsPerIP(t *testing.T) {
	r := newEngine(RateLimit(1, 1, time.Minute))

	if w := request(r, http.MethodGet, "10.0.0.1:1111", ""); w.Code != http.StatusOK {
		t.Fatalf("第一个请求返回 %d，期望 200", w.Code)
	}
	if w := request(r, http.MethodGet, "10.0.0.1:1111", ""); w.Code != http.StatusTooManyRequests {
		t.Errorf("同一 IP 第二次请求返回 %d，期望 429", w.Code)
	}
	// 关键：另一个 IP 不受影响
	if w := request(r, http.MethodGet, "10.0.0.2:2222", ""); w.Code != http.StatusOK {
		t.Errorf("不同 IP 的请求返回 %d，期望 200（限流必须按 IP 而非全局）", w.Code)
	}
}

// TestRateLimitIgnoresSpoofedForwardedFor 保证客户端无法伪造 X-Forwarded-For 绕过限流。
func TestRateLimitIgnoresSpoofedForwardedFor(t *testing.T) {
	r := newEngine(RateLimit(1, 1, time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.RemoteAddr = "10.0.0.1:1111"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("首个请求返回 %d", w.Code)
	}

	// 换一个伪造的 X-Forwarded-For，仍然应该被同一个 IP 的配额拦住
	req2 := httptest.NewRequest(http.MethodGet, "/t", nil)
	req2.RemoteAddr = "10.0.0.1:1111"
	req2.Header.Set("X-Forwarded-For", "5.6.7.8")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("伪造 X-Forwarded-For 后返回 %d，期望 429（可绕过限流）", w2.Code)
	}
}

// TestSecureHeaders 给每个响应加安全头，且 CSP 里 script-src 不得放宽。
//
// 后半条是这个用例的重点：整个策略的收益几乎全来自 script-src 'self'。
// 一旦有人为了「先跑起来」给它加了 unsafe-inline，其余几条挡的都是小事，
// 而编译期没人会拦这种改动——只有测试能。
func TestSecureHeaders(t *testing.T) {
	r := newEngine(SecureHeaders())
	w := request(r, http.MethodGet, "10.0.0.1:1111", "")

	for _, tc := range []struct{ header, want string }{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Referrer-Policy", "no-referrer"},
		{"Permissions-Policy", "camera=(), microphone=(), geolocation=()"},
		{"X-Permitted-Cross-Domain-Policies", "none"},
		{"Strict-Transport-Security", "max-age=31536000"},
	} {
		if got := w.Header().Get(tc.header); got != tc.want {
			t.Errorf("%s = %q，期望 %q", tc.header, got, tc.want)
		}
	}

	directives := parseCSP(t, w.Header().Get("Content-Security-Policy"))
	if directives["script-src"] != "'self'" {
		t.Errorf("script-src = %q，期望严格的 'self'（放宽它等于放弃整条 CSP）",
			directives["script-src"])
	}
	// 样式必须保留 unsafe-inline：Svelte 组件用 style="..." 属性，去掉就没样式了
	if !strings.Contains(directives["style-src"], "'unsafe-inline'") {
		t.Errorf("style-src = %q，需要 'unsafe-inline'（组件依赖 style 属性）",
			directives["style-src"])
	}
	if directives["default-src"] != "'self'" {
		t.Errorf("default-src = %q，期望 'self'", directives["default-src"])
	}
}

// parseCSP 把 CSP 串拆成「指令 → 参数」，让用例能断言**某一条**指令，
// 而不是用 Contains 在整串里碰运气（"unsafe-inline" 同时出现在两条指令里）。
func parseCSP(t *testing.T, raw string) map[string]string {
	t.Helper()
	if raw == "" {
		t.Fatal("没有 Content-Security-Policy 头")
	}
	out := map[string]string{}
	for _, part := range strings.Split(raw, ";") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		out[fields[0]] = strings.Join(fields[1:], " ")
	}
	return out
}

// TestRequestIDReplacesInvalidUpstreamValue 覆盖日志注入防线：
// 上游给什么就写进日志行是不行的，换行符能凭空造出一条记录。
func TestRequestIDReplacesInvalidUpstreamValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(RequestID())
	r.GET("/t", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 合法的上游 ID 要被沿用，这样网关/代理两侧的日志才能对上
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("X-Request-Id", "upstream-abc.123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := w.Header().Get("X-Request-Id"); got != "upstream-abc.123" {
		t.Errorf("合法的上游 ID 被替换成了 %q", got)
	}

	// 含换行的 ID 必须被换掉
	req = httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("X-Request-Id", "injected\nlevel=ERROR")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	got := w.Header().Get("X-Request-Id")
	if strings.ContainsAny(got, "\n ") || got == "" {
		t.Errorf("非法 ID 未被替换：%q", got)
	}
	if !regexp.MustCompile(`^[a-f0-9]{16}$`).MatchString(got) {
		t.Errorf("替换后的 ID = %q，期望 16 位十六进制", got)
	}
}

// TestRequestLoggerLogsRequestIDAndLevel 断言访问日志是**结构化**的：
// 带 request_id，且 5xx 记为 ERROR、2xx 记为 INFO。
//
// 直接接管默认 logger 读输出，而不是猜字段——这条用例要防的正是
// 「有人改回拼字符串」这种不动任何接口、只让日志变难查的改动。
func TestRequestLoggerLogsRequestIDAndLevel(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(RequestID(), RequestLogger())
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/boom", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })

	wOK := httptest.NewRecorder()
	r.ServeHTTP(wOK, httptest.NewRequest(http.MethodGet, "/ok", nil))

	logs := buf.String()
	id := wOK.Header().Get("X-Request-Id")
	if !strings.Contains(logs, "request_id="+id) {
		t.Errorf("日志里没有本次请求的 request_id（%s）:\n%s", id, logs)
	}
	if !strings.Contains(logs, "status=200") || !strings.Contains(logs, "duration_ms=") {
		t.Errorf("日志缺少 status/duration_ms 字段:\n%s", logs)
	}
	if !strings.Contains(logs, "level=INFO") {
		t.Errorf("2xx 期望 INFO 级:\n%s", logs)
	}

	buf.Reset()
	wBoom := httptest.NewRecorder()
	r.ServeHTTP(wBoom, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if !strings.Contains(buf.String(), "level=ERROR") || !strings.Contains(buf.String(), "status=500") {
		t.Errorf("5xx 期望 ERROR 级且带 status=500:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "request_id="+wBoom.Header().Get("X-Request-Id")) {
		t.Errorf("5xx 日志缺少 request_id:\n%s", buf.String())
	}
}

func TestBodyLimitRejectsOversizedBody(t *testing.T) {
	r := newEngine(BodyLimit(16))

	if w := request(r, http.MethodPost, "10.0.0.1:1111", "small"); w.Code != http.StatusOK {
		t.Fatalf("小 body 返回 %d，期望 200", w.Code)
	}
	// MaxBytesReader 只在 body 被读取时报错，所以这里要求 handler 读一次
	if w := request(r, http.MethodPost, "10.0.0.1:1111", strings.Repeat("x", 1024)); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("超大 body 返回 %d，期望 413（未限制请求体大小）", w.Code)
	}
}

func TestImmutableAssetsOnlyMatchesPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ImmutableAssets("/assets/"))
	r.GET("/assets/:file", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/v1/anything", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset 的 Cache-Control = %q，期望含 immutable", cc)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if cc := w.Header().Get("Cache-Control"); cc != "" {
		t.Errorf("API 不应被加上 asset 缓存头，实际 %q", cc)
	}
}
