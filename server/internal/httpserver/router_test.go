package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func send(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestDeleteReturnsEnvelope 覆盖 B1：DELETE 不再返回 204 空 body，
// 所有接口统一走响应信封，前端无需特判。
func TestDeleteReturnsEnvelope(t *testing.T) {
	r, _ := newStaticFixture(t)

	if w := send(t, r, http.MethodPost, "/api/v1/devices",
		`{"name":"sensor","location":"lab"}`); w.Code != http.StatusOK {
		t.Fatalf("创建失败: %d %s", w.Code, w.Body.String())
	}

	w := send(t, r, http.MethodDelete, "/api/v1/devices/1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE 返回 %d，期望 200（不再用 204 空 body）", w.Code)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("DELETE 响应不是信封格式: %s", w.Body.String())
	}
	if payload.Code != 0 || payload.Data.ID != 1 {
		t.Errorf("DELETE 信封内容不对: %s", w.Body.String())
	}
}

// TestNotFoundMapsTo404 覆盖 B2：错误映射集中在一处，handler 不再 import repository。
func TestNotFoundMapsTo404(t *testing.T) {
	r, _ := newStaticFixture(t)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/devices/999", ""},
		{http.MethodPatch, "/api/v1/devices/999", `{"enabled":true}`},
		{http.MethodDelete, "/api/v1/devices/999", ""},
	} {
		w := send(t, r, tc.method, tc.path, tc.body)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s 返回 %d，期望 404", tc.method, tc.path, w.Code)
		}
	}
}

// TestOversizedBodyReturns413 覆盖 HTTP 语义码：body 超限应当是 413 而不是笼统的 400。
func TestOversizedBodyReturns413(t *testing.T) {
	r, _ := newStaticFixture(t)

	huge := `{"name":"` + strings.Repeat("x", 2<<20) + `","location":"lab"}`
	w := send(t, r, http.MethodPost, "/api/v1/devices", huge)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("超大 body 返回 %d，期望 413: %s", w.Code, w.Body.String())
	}
}

// TestValidationFailureReturnsFieldErrors 校验失败必须展开成字段级数组，
// 且只给「字段 / 规则 / 参数」三个机器可读信息：
// 字段名要用 json tag（name）而不是 Go 字段名（Name），文案归于前端 i18n。
func TestValidationFailureReturnsFieldErrors(t *testing.T) {
	r, _ := newStaticFixture(t)

	w := send(t, r, http.MethodPost, "/api/v1/devices", `{"name":"a","location":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("校验失败返回 %d，期望 400", w.Code)
	}

	var payload struct {
		Code   int    `json:"code"`
		Msg    string `json:"msg"`
		Errors []struct {
			Field string `json:"field"`
			Rule  string `json:"rule"`
			Param string `json:"param"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是合法 JSON: %s", w.Body.String())
	}
	// msg 是稳定的 i18n 键，不是文案
	if payload.Msg != "error.validationFailed" {
		t.Errorf("msg = %q，期望 i18n 键 error.validationFailed", payload.Msg)
	}

	got := map[string]string{}
	for _, fe := range payload.Errors {
		got[fe.Field] = fe.Rule + "|" + fe.Param
	}
	if got["name"] != "min|2" {
		t.Errorf("name 的字段错误 = %q，实际响应 %s", got["name"], w.Body.String())
	}
	if got["location"] != "required|" {
		t.Errorf("location 的字段错误 = %q，实际响应 %s", got["location"], w.Body.String())
	}
	if _, ok := got["Name"]; ok {
		t.Errorf("字段名未使用 json tag，响应 %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "validation for") {
		t.Errorf("响应泄露了 validator 内部描述: %s", w.Body.String())
	}
}

// TestErrorResponsesCarryI18nKeys 保证失败响应里不会出现自然语言文案：
// 一旦出现，前端就没法翻译它。
func TestErrorResponsesCarryI18nKeys(t *testing.T) {
	r, _ := newStaticFixture(t)

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantKey    string
	}{
		{"不存在", http.MethodGet, "/api/v1/devices/999", "", http.StatusNotFound, "error.notFound"},
		{"id 非法", http.MethodGet, "/api/v1/devices/abc", "", http.StatusBadRequest, "error.invalidId"},
		{"非法 JSON", http.MethodPost, "/api/v1/devices", `{"name":`, http.StatusBadRequest, "error.malformedBody"},
		{"未知路由", http.MethodGet, "/api/v1/nope", "", http.StatusNotFound, "error.notFound"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := send(t, r, tc.method, tc.path, tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("返回 %d，期望 %d", w.Code, tc.wantStatus)
			}
			var payload struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatalf("响应不是 JSON: %s", w.Body.String())
			}
			if payload.Msg != tc.wantKey {
				t.Errorf("msg = %q，期望 %q", payload.Msg, tc.wantKey)
			}
		})
	}
}

// TestMalformedJSONReturnsGeneric400 不能把 json.SyntaxError 的内部细节暴露出去。
func TestMalformedJSONReturnsGeneric400(t *testing.T) {
	r, _ := newStaticFixture(t)

	w := send(t, r, http.MethodPost, "/api/v1/devices", `{"name":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("返回 %d，期望 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "unexpected end of JSON") ||
		strings.Contains(w.Body.String(), "invalid character") {
		t.Errorf("响应泄露了解析器细节: %s", w.Body.String())
	}
}

func TestSuccessEnvelope(t *testing.T) {
	r, _ := newStaticFixture(t)

	w := send(t, r, http.MethodGet, "/api/v1/devices", "")
	if w.Code != http.StatusOK {
		t.Fatalf("返回 %d", w.Code)
	}
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != 0 || payload.Msg != "ok" {
		t.Errorf("成功信封 = %+v，期望 code=0 msg=ok", payload)
	}
}
