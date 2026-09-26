package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/handler"
	"mini-ruoyi/internal/repository"
	"mini-ruoyi/internal/service"
)

func newTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "handler.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := repository.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	h := handler.NewDeviceHandler(service.NewDeviceService(repository.NewDeviceRepository(db)))
	r := gin.New()
	r.POST("/devices", h.Create)
	r.GET("/devices", h.List)
	r.GET("/devices/:id", h.Get)
	r.PATCH("/devices/:id", h.Update)
	r.DELETE("/devices/:id", h.Delete)
	return r
}

func do(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestUpdateRequiresEnabled 是 A2 的回归用例：
// 用 bool 时 PATCH {} 会把 enabled 静默改成 false。
func TestUpdateRequiresEnabled(t *testing.T) {
	r := newTestEngine(t)

	created := do(t, r, http.MethodPost, "/devices", `{"name":"sensor","location":"lab","enabled":true}`)
	if created.Code != http.StatusOK {
		t.Fatalf("创建失败: %d %s", created.Code, created.Body.String())
	}

	// 不带 enabled 的 PATCH 必须被拒绝，而不是把 true 静默改成 false
	w := do(t, r, http.MethodPatch, "/devices/1", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("PATCH {} 返回 %d，期望 400（不能静默改 enabled）", w.Code)
	}

	// 显式传 false 必须生效
	w = do(t, r, http.MethodPatch, "/devices/1", `{"enabled":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH {\"enabled\":false} 返回 %d: %s", w.Code, w.Body.String())
	}

	got := do(t, r, http.MethodGet, "/devices/1", "")
	var payload struct {
		Data struct {
			Enabled bool `json:"enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if payload.Data.Enabled {
		t.Error("enabled 仍是 true，PATCH 未生效")
	}
}

// TestListEmptyReturnsEmptyArray 是 A4 的回归用例：
// data.list 必须是 []，不能是 null。
func TestListEmptyReturnsEmptyArray(t *testing.T) {
	r := newTestEngine(t)

	w := do(t, r, http.MethodGet, "/devices", "")
	if w.Code != http.StatusOK {
		t.Fatalf("返回 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"list":[]`) {
		t.Errorf("空列表响应体是 %s，期望包含 \"list\":[]", w.Body.String())
	}
}

// TestListReturnsTotal 是 A5 的回归用例。
func TestListReturnsTotal(t *testing.T) {
	r := newTestEngine(t)
	for _, name := range []string{"aa", "bb", "cc"} {
		w := do(t, r, http.MethodPost, "/devices", `{"name":"`+name+`","location":"lab"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("创建 %s 失败: %d %s", name, w.Code, w.Body.String())
		}
	}

	w := do(t, r, http.MethodGet, "/devices?page=1&page_size=2", "")
	var payload struct {
		Data struct {
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
			List     []struct {
				ID int64 `json:"id"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应: %v: %s", err, w.Body.String())
	}
	if payload.Data.Total != 3 {
		t.Errorf("total = %d，期望 3", payload.Data.Total)
	}
	if len(payload.Data.List) != 2 {
		t.Errorf("第 1 页返回 %d 条，期望 2", len(payload.Data.List))
	}
}

// TestCreateResponseUsesSnakeCaseKeys 覆盖 Device 缺少 json tag 的问题：
// 不加 tag 会序列化成 "Name"/"CreatedAt"，而请求体用的是小写 name。
func TestCreateResponseUsesSnakeCaseKeys(t *testing.T) {
	r := newTestEngine(t)

	w := do(t, r, http.MethodPost, "/devices", `{"name":"sensor","location":"lab"}`)
	body := w.Body.String()
	for _, key := range []string{`"id"`, `"name"`, `"location"`, `"enabled"`, `"created_at"`} {
		if !strings.Contains(body, key) {
			t.Errorf("响应缺少字段 %s: %s", key, body)
		}
	}
	if strings.Contains(body, `"CreatedAt"`) {
		t.Errorf("响应出现大驼峰字段: %s", body)
	}
}
