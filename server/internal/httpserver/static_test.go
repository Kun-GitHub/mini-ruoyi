package httpserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/handler"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

// newStaticFixture 建一个最小的 Vite 产物目录，并返回可直接请求的路由。
func newStaticFixture(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	webDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>mini-ruoyi</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "static.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := repository.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	svc := service.NewDeviceService(repository.NewDeviceRepository(db))
	r, err := NewRouter(handler.NewDeviceHandler(svc), db, webDir)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return r, webDir
}

func get(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestStaticServesIndexAndSpaFallback(t *testing.T) {
	r, _ := newStaticFixture(t)

	for _, path := range []string{"/", "/system/user"} {
		w := get(t, r, path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s 返回 %d，期望 200", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "mini-ruoyi") {
			t.Errorf("GET %s 未返回 index.html: %s", path, w.Body.String())
		}
		// index.html 必须不可缓存，否则前端重新构建后刷新拿不到最新版本
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s 的 Cache-Control = %q，期望 no-cache", path, cc)
		}
	}
}

func TestStaticAssetsAreImmutable(t *testing.T) {
	r, _ := newStaticFixture(t)

	w := get(t, r, "/assets/app-abc123.js")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /assets/app-abc123.js 返回 %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset 的 Cache-Control = %q，期望含 immutable", cc)
	}
}

// TestAPINotFoundReturnsJSON 保证未知 API 路径返回 JSON 而不是 index.html，
// 否则前端会把一整页 HTML 当 JSON 解析，报错信息完全看不出原因。
func TestAPINotFoundReturnsJSON(t *testing.T) {
	r, _ := newStaticFixture(t)

	w := get(t, r, "/api/v1/nope")
	if w.Code != http.StatusNotFound {
		t.Fatalf("返回 %d，期望 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q，期望 application/json", ct)
	}
	if strings.Contains(w.Body.String(), "mini-ruoyi") {
		t.Errorf("API 404 回落到了 index.html: %s", w.Body.String())
	}
}

// TestMissingAssetIsNotHtml 保证缺失的静态资源不会回落成 index.html，
// 否则浏览器会把 HTML 当 JS 执行，报语法错误。
func TestMissingAssetIsNotHtml(t *testing.T) {
	r, _ := newStaticFixture(t)

	w := get(t, r, "/assets/missing.js")
	if w.Code != http.StatusNotFound {
		t.Errorf("返回 %d，期望 404", w.Code)
	}
	if strings.Contains(w.Body.String(), "mini-ruoyi") {
		t.Errorf("缺失 asset 回落到了 index.html: %s", w.Body.String())
	}
}

// TestNewRouterFailsFastWhenWebDirIncomplete 覆盖「目录存在但部署错了」：
// 这种情况应该启动即失败，而不是运行期才 404。
func TestNewRouterFailsFastWhenWebDirIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "boot.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	svc := service.NewDeviceService(repository.NewDeviceRepository(db))
	h := handler.NewDeviceHandler(svc)

	// 空目录：缺 index.html
	if _, err := NewRouter(h, db, t.TempDir()); err == nil {
		t.Error("前端目录为空时 NewRouter 应返回错误")
	}

	// 有 index.html 但没有 assets/
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRouter(h, db, dir); err == nil {
		t.Error("缺少 assets/ 时 NewRouter 应返回错误")
	}
}

// TestAPIOnlyDeployment 覆盖 WebDir 整体不存在（纯 API 部署）：
// 不应该阻断启动，且所有未知路径都返回 JSON。
func TestAPIOnlyDeployment(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "apionly.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := repository.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	svc := service.NewDeviceService(repository.NewDeviceRepository(db))
	r, err := NewRouter(handler.NewDeviceHandler(svc), db, filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("WebDir 不存在时不应阻断启动: %v", err)
	}

	for _, path := range []string{"/", "/api/v1/nope", "/assets/app.js"} {
		w := get(t, r, path)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s 返回 %d，期望 404", path, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("GET %s 的 Content-Type = %q，期望 JSON", path, w.Header().Get("Content-Type"))
		}
	}
}
