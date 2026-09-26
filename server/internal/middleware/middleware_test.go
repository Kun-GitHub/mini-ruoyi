package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
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
	r.GET("/api/v1/devices", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset 的 Cache-Control = %q，期望含 immutable", cc)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if cc := w.Header().Get("Cache-Control"); cc != "" {
		t.Errorf("API 不应被加上 asset 缓存头，实际 %q", cc)
	}
}
