package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/perm"
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

type envelope struct {
	Code   int    `json:"code"`
	Msg    string `json:"msg"`
	Errors []struct {
		Field string `json:"field"`
		Rule  string `json:"rule"`
		Param string `json:"param"`
	} `json:"errors"`
	Data json.RawMessage `json:"data"`
}

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("响应不是合法 JSON: %s", w.Body.String())
	}
	return e
}

// ---------- 安全边界：哪些端点不需要登录 ----------

// TestPublicEndpointsAreMinimal 是 fail-closed 的兜底断言。
//
// 注册端点的入口只有 registrar 的三个方法，其中 protect 强制要求权限码，
// 漏声明在结构上不可能发生。这里再钉死「公开端点」的集合：
// 新增任何不需要登录的端点都会让这个用例失败，从而被迫经过一次评审。
func TestPublicEndpointsAreMinimal(t *testing.T) {
	_, table := newTestRouter(t)

	want := map[string]bool{"POST /api/v1/auth/login": true}
	got := map[string]bool{}
	for _, r := range table.Public {
		got[r.Method+" "+r.Path] = true
		if r.Reason == "" {
			t.Errorf("公开端点 %s %s 没有写明理由", r.Method, r.Path)
		}
	}

	for k := range got {
		if !want[k] {
			t.Errorf("出现了新的公开端点 %s：请确认它真的不需要登录，然后更新本用例", k)
		}
	}
	for k := range want {
		if !got[k] {
			t.Errorf("预期的公开端点 %s 不存在了", k)
		}
	}
}

// TestEveryProtectedEndpointHasADeclaredPerm 确认所有需权限的端点都带着有效的权限码。
func TestEveryProtectedEndpointHasADeclaredPerm(t *testing.T) {
	_, table := newTestRouter(t)

	if len(table.Protected) == 0 && len(table.Self) == 0 {
		t.Fatal("没有任何受保护的端点，路由注册可能没生效")
	}
	for _, r := range table.Protected {
		if r.Perm == "" {
			t.Errorf("端点 %s %s 没有权限码", r.Method, r.Path)
		}
	}
}

// TestUnauthenticatedRequestsAreRejected 确认受保护端点在没有会话时一律 401，
// 而不是「没权限就放行」。
func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	r, table := newTestRouter(t)

	for _, route := range append(append([]Route{}, table.Self...), table.Protected...) {
		w := send(t, r, route.Method, route.Path, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 未登录时返回 %d，期望 401", route.Method, route.Path, w.Code)
		}
		if e := decode(t, w); e.Msg != "error.unauthorized" {
			t.Errorf("%s %s 的 msg = %q，期望 error.unauthorized", route.Method, route.Path, e.Msg)
		}
	}
}

// ---------- 认证全链路 ----------

type sessionInfo struct {
	cookie    string
	csrfToken string
	userID    int64
	username  string
	isAdmin   bool
	perms     []string
}

func login(t *testing.T, r *gin.Engine, username, password string) sessionInfo {
	t.Helper()

	w := send(t, r, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("登录失败: %d %s", w.Code, w.Body.String())
	}

	var body struct {
		Data struct {
			User struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"user"`
			IsAdmin   bool     `json:"is_admin"`
			Perms     []string `json:"perms"`
			CSRFToken string   `json:"csrf_token"`
			Menus     []any    `json:"menus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析登录响应: %v", err)
	}

	var cookie string
	for _, c := range w.Result().Cookies() {
		if c.Name == "mr_session" {
			cookie = c.Value
			if !c.HttpOnly {
				t.Error("会话 cookie 必须是 HttpOnly")
			}
			// MaxAge 为 0 会让它变成「关浏览器就失效」的会话 cookie，
			// 配置的 SessionTTL 就静默失去了意义
			if want := int(auth.SessionTTL.Seconds()); c.MaxAge != want {
				t.Errorf("cookie MaxAge = %d，期望 %d（SessionTTL）", c.MaxAge, want)
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie SameSite = %v，期望 Lax", c.SameSite)
			}
		}
	}
	if cookie == "" {
		t.Fatal("登录响应没有下发会话 cookie")
	}
	// 解析出空 payload 说明信封路径写错了，立刻报出来，
	// 否则会退化成一堆「期望 true 实际 false」的噪声
	if body.Data.User.ID == 0 || body.Data.CSRFToken == "" {
		t.Fatalf("登录响应解析为空，检查信封路径: %s", w.Body.String())
	}

	return sessionInfo{
		cookie:    cookie,
		csrfToken: body.Data.CSRFToken,
		userID:    body.Data.User.ID,
		username:  body.Data.User.Username,
		isAdmin:   body.Data.IsAdmin,
		perms:     body.Data.Perms,
	}
}

func withSession(method, path, body string, s sessionInfo, csrf bool) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "mr_session", Value: s.cookie})
	if csrf {
		req.Header.Set("X-CSRF-Token", s.csrfToken)
	}
	return req
}

func TestLoginMeLogoutFlow(t *testing.T) {
	r, _ := newTestRouter(t)
	s := login(t, r, "admin", "admin123")

	if !s.isAdmin {
		t.Error("内置管理员登录后 is_admin 应为 true")
	}
	// 管理员应拿到全部已声明的权限码，前端只需一种判断
	if len(s.perms) == 0 {
		t.Error("管理员登录后应返回全部权限码")
	}
	if s.csrfToken == "" {
		t.Error("登录响应必须下发 CSRF 令牌")
	}

	// /me 用同一个 cookie 应能恢复状态
	w := httptest.NewRecorder()
	r.ServeHTTP(w, withSession(http.MethodGet, "/api/v1/auth/me", "", s, false))
	if w.Code != http.StatusOK {
		t.Fatalf("/me 返回 %d: %s", w.Code, w.Body.String())
	}
	var me struct {
		Data struct {
			User struct {
				ID int64 `json:"id"`
			} `json:"user"`
			CSRFToken string `json:"csrf_token"`
			Menus     []struct {
				TitleKey string `json:"title_key"`
				Children []any  `json:"children"`
			} `json:"menus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Data.User.ID != s.userID {
		t.Errorf("/me 返回的 user.id = %d，期望 %d", me.Data.User.ID, s.userID)
	}
	if me.Data.CSRFToken != s.csrfToken {
		t.Error("/me 应返回与登录一致的 CSRF 令牌（刷新页面后靠它恢复）")
	}
	if len(me.Data.Menus) != 3 || len(me.Data.Menus[0].Children) != 4 {
		t.Errorf("管理员应看到完整的菜单树，实际 %+v", me.Data.Menus)
	}

	// 登出
	w = httptest.NewRecorder()
	r.ServeHTTP(w, withSession(http.MethodPost, "/api/v1/auth/logout", "", s, true))
	if w.Code != http.StatusOK {
		t.Fatalf("登出返回 %d: %s", w.Code, w.Body.String())
	}

	// 登出后同一个 cookie 必须失效
	w = httptest.NewRecorder()
	r.ServeHTTP(w, withSession(http.MethodGet, "/api/v1/auth/me", "", s, false))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("登出后 /me 返回 %d，期望 401（会话必须服务端失效）", w.Code)
	}
}

// TestLogoutRequiresCSRF 确认写操作缺少 CSRF 头时被拒。
func TestLogoutRequiresCSRF(t *testing.T) {
	r, _ := newTestRouter(t)
	s := login(t, r, "admin", "admin123")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, withSession(http.MethodPost, "/api/v1/auth/logout", "", s, false))
	if w.Code != http.StatusForbidden {
		t.Fatalf("无 CSRF 头时返回 %d，期望 403", w.Code)
	}
	if e := decode(t, w); e.Msg != "error.csrfInvalid" {
		t.Errorf("msg = %q，期望 error.csrfInvalid", e.Msg)
	}

	// 伪造的令牌同样要被拒
	req := withSession(http.MethodPost, "/api/v1/auth/logout", "", s, false)
	req.Header.Set("X-CSRF-Token", "forged")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("伪造 CSRF 令牌时返回 %d，期望 403", w.Code)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	r, _ := newTestRouter(t)

	cases := []struct{ name, body string }{
		{"密码错误", `{"username":"admin","password":"wrong-password"}`},
		{"用户不存在", `{"username":"nobody","password":"whatever"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := send(t, r, http.MethodPost, "/api/v1/auth/login", tc.body)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("返回 %d，期望 401", w.Code)
			}
			// 两种情况的错误必须一模一样，否则可以据此枚举有效用户名
			if e := decode(t, w); e.Msg != "error.badCredentials" {
				t.Errorf("msg = %q，期望 error.badCredentials", e.Msg)
			}
		})
	}
}

// TestSessionIsRevokedWhenUserDisabled 覆盖「停用用户要立刻生效」：
// 不能等 cookie 自己过期。
func TestSessionIsRevokedWhenUserDisabled(t *testing.T) {
	deps := testDeps(t, testWebDir(t))
	r, _, err := NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	s := login(t, r, "admin", "admin123")

	// 直接改库把用户停用
	if _, err := deps.DB.Exec(`UPDATE sys_users SET status = 'inactive' WHERE id = ?`, s.userID); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, withSession(http.MethodGet, "/api/v1/auth/me", "", s, false))
	if w.Code == http.StatusOK {
		t.Fatal("账号被停用后仍能访问 /me")
	}
	if e := decode(t, w); e.Msg != "error.accountDisabled" {
		t.Errorf("msg = %q，期望 error.accountDisabled", e.Msg)
	}
}

// ---------- 响应契约 ----------

func TestSuccessEnvelope(t *testing.T) {
	r, _ := newTestRouter(t)

	w := get(t, r, "/healthz")
	if w.Code != http.StatusOK {
		t.Fatalf("返回 %d", w.Code)
	}
	e := decode(t, w)
	if e.Code != http.StatusOK || e.Msg != "ok" {
		t.Errorf("成功信封 = %+v，期望 code=200 msg=ok", e)
	}
}

// TestValidationFailureReturnsFieldErrors 校验失败必须展开成字段级数组，
// 且只给「字段 / 规则 / 参数」三个机器可读信息：
// 字段名要用 json tag（username）而不是 Go 字段名（Username），文案归于前端 i18n。
func TestValidationFailureReturnsFieldErrors(t *testing.T) {
	r, _ := newTestRouter(t)

	w := send(t, r, http.MethodPost, "/api/v1/auth/login", `{"username":"","password":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("校验失败返回 %d，期望 400", w.Code)
	}
	e := decode(t, w)
	if e.Msg != "error.validationFailed" {
		t.Errorf("msg = %q，期望 i18n 键 error.validationFailed", e.Msg)
	}

	got := map[string]string{}
	for _, fe := range e.Errors {
		got[fe.Field] = fe.Rule
	}
	if got["username"] != "required" {
		t.Errorf("username 的字段错误 = %q，实际响应 %s", got["username"], w.Body.String())
	}
	if got["password"] != "required" {
		t.Errorf("password 的字段错误 = %q，实际响应 %s", got["password"], w.Body.String())
	}
	if strings.Contains(w.Body.String(), "validation for") {
		t.Errorf("响应泄露了 validator 内部描述: %s", w.Body.String())
	}
}

// TestPasswordLengthIsBoundedByBcryptLimit 覆盖 bcrypt 的 72 字节上限。
// 不在校验层挡住的话，用户会拿到 500 而不是「参数不合法」。
func TestPasswordLengthIsBoundedByBcryptLimit(t *testing.T) {
	r, _ := newTestRouter(t)

	long := strings.Repeat("x", 100)
	w := send(t, r, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"`+long+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("超长密码返回 %d，期望 400（而不是 500）", w.Code)
	}
	e := decode(t, w)
	if len(e.Errors) == 0 || e.Errors[0].Field != "password" || e.Errors[0].Rule != "max" {
		t.Errorf("期望 password 的 max 规则错误，实际 %+v", e.Errors)
	}
}

func TestMalformedJSONReturnsGeneric400(t *testing.T) {
	r, _ := newTestRouter(t)

	w := send(t, r, http.MethodPost, "/api/v1/auth/login", `{"username":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("返回 %d，期望 400", w.Code)
	}
	if e := decode(t, w); e.Msg != "error.malformedBody" {
		t.Errorf("msg = %q，期望 error.malformedBody", e.Msg)
	}
	if strings.Contains(w.Body.String(), "unexpected end of JSON") ||
		strings.Contains(w.Body.String(), "invalid character") {
		t.Errorf("响应泄露了解析器细节: %s", w.Body.String())
	}
}

func TestOversizedBodyReturns413(t *testing.T) {
	r, _ := newTestRouter(t)

	huge := `{"username":"` + strings.Repeat("x", 2<<20) + `"}`
	w := send(t, r, http.MethodPost, "/api/v1/auth/login", huge)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("超大 body 返回 %d，期望 413: %s", w.Code, w.Body.String())
	}
}

// TestErrorResponsesCarryI18nKeys 保证失败响应里不会出现自然语言文案：
// 一旦出现，前端就没法翻译它。
func TestErrorResponsesCarryI18nKeys(t *testing.T) {
	r, _ := newTestRouter(t)

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantKey    string
	}{
		{"未知路由", http.MethodGet, "/api/v1/nope", "", http.StatusNotFound, "error.notFound"},
		{"未登录", http.MethodGet, "/api/v1/auth/me", "", http.StatusUnauthorized, "error.unauthorized"},
		{"非法 JSON", http.MethodPost, "/api/v1/auth/login", `{"username":`, http.StatusBadRequest, "error.malformedBody"},
		{"校验失败", http.MethodPost, "/api/v1/auth/login", `{}`, http.StatusBadRequest, "error.validationFailed"},
		{"凭据错误", http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"nope"}`, http.StatusUnauthorized, "error.badCredentials"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := send(t, r, tc.method, tc.path, tc.body)
			if w.Code != tc.wantStatus {
				t.Fatalf("返回 %d，期望 %d", w.Code, tc.wantStatus)
			}
			if e := decode(t, w); e.Msg != tc.wantKey {
				t.Errorf("msg = %q，期望 %q", e.Msg, tc.wantKey)
			}
		})
	}
}

// TestRouterRejectsIncompleteDeps 覆盖装配遗漏。
//
// 少注入一个 handler 时，请求会 panic 并被 Recovery 兜成 500，
// 排查时会往业务逻辑上找。所以在装配阶段就拒绝，并指名是哪个字段。
func TestRouterRejectsIncompleteDeps(t *testing.T) {
	full := testDeps(t, testWebDir(t))

	cases := []struct {
		name      string
		breakDeps func(Deps) Deps
		wantField string
	}{
		{"缺 Auth", func(d Deps) Deps { d.Auth = nil; return d }, "Auth"},
		{"缺 User", func(d Deps) Deps { d.User = nil; return d }, "User"},
		{"缺 Role", func(d Deps) Deps { d.Role = nil; return d }, "Role"},
		{"缺 Menu", func(d Deps) Deps { d.Menu = nil; return d }, "Menu"},
		{"缺 AuthMW", func(d Deps) Deps { d.AuthMW = nil; return d }, "AuthMW"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := NewRouter(tc.breakDeps(full))
			if err == nil {
				t.Fatalf("依赖缺失时 NewRouter 应返回错误")
			}
			if !strings.Contains(err.Error(), tc.wantField) {
				t.Errorf("错误信息未指出缺失的字段 %s: %v", tc.wantField, err)
			}
		})
	}
}

// TestPanicReturnsEnvelope 覆盖「panic 也要返回信封」。
//
// gin 自带的 Recovery 返回的是空 body 的 500，而前端靠「响应是不是信封格式」
// 来区分「后端没起来」（代理返回 502 + text/plain）和「后端出错了」。
// 空 body 会让后者被误报成前者，排查方向直接错掉。
func TestPanicReturnsEnvelope(t *testing.T) {
	r := gin.New()
	r.Use(RecoveryForTest())
	r.GET("/panic", func(c *gin.Context) { panic("boom") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("返回 %d，期望 500", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q，期望 JSON（空 body 的 500 会让前端误判成后端没起来）", ct)
	}
	e := decode(t, w)
	if e.Code != http.StatusInternalServerError || e.Msg != "error.internal" {
		t.Errorf("响应 = %+v，期望 {code:500, msg:error.internal}", e)
	}
}

// TestEveryDeclaredPermIsUsedByARoute 确认权限清单里没有「死条目」。
//
// 声明了却没有路由引用的权限码会出现在授权界面里：管理员勾上它、保存成功、
// 然后发现什么也没发生。这种「看着像权限、其实是装饰」的记录只能靠用例堵住，
// 因为它在任何一层都不会报错。
func TestEveryDeclaredPermIsUsedByARoute(t *testing.T) {
	_, table := newTestRouter(t)

	used := map[string]bool{}
	for _, r := range table.Protected {
		used[string(r.Perm)] = true
	}
	// 反向也要成立：路由上写了的码必须是声明过的
	declared := map[string]bool{}
	for _, c := range perm.All() {
		declared[string(c)] = true
	}

	for _, code := range perm.All() {
		if !used[string(code)] {
			t.Errorf("权限码 %s 已声明但没有任何路由引用（会出现在授权界面里却不起作用）", code)
		}
	}
	for _, r := range table.Protected {
		if !declared[string(r.Perm)] {
			t.Errorf("%s %s 用了未声明的权限码 %s", r.Method, r.Path, r.Perm)
		}
	}
}

// TestPermCatalogueListsEndpoints 确认权限清单把「权限码 ↔ 接口」的对应关系带出来了。
//
// 只显示「查询用户」这种文案时，没人知道它对应 GET /api/v1/users，
// 排查「为什么这个角色还能调某个接口」就只能去翻代码。
func TestPermCatalogueListsEndpoints(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodGet, "/api/v1/perms", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("返回 %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Data struct {
			Groups []struct {
				Perms []struct {
					Code      string `json:"code"`
					Endpoints []struct {
						Method string `json:"method"`
						Path   string `json:"path"`
					} `json:"endpoints"`
				} `json:"perms"`
			} `json:"groups"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, g := range res.Data.Groups {
		for _, p := range g.Perms {
			if p.Code != "system:user:list" {
				continue
			}
			for _, e := range p.Endpoints {
				if e.Method == http.MethodGet && e.Path == "/api/v1/users" {
					found = true
				}
			}
			if len(p.Endpoints) == 0 {
				t.Errorf("system:user:list 没有带出任何接口")
			}
		}
	}
	if !found {
		t.Error("system:user:list 的接口列表里没有 GET /api/v1/users")
	}
}

// TestAPIOnlyModeExplainsItself 覆盖纯 API 模式下打开首页的提示。
//
// 这是最常见的困惑点：看到一句 JSON 404 会以为服务坏了。
// 明说是「前端没部署」，比笼统的 not found 省掉一轮排查。
func TestAPIOnlyModeExplainsItself(t *testing.T) {
	r, _, err := NewRouter(testDeps(t, filepath.Join(t.TempDir(), "nonexistent")))
	if err != nil {
		t.Fatalf("WebDir 不存在时不应阻断启动: %v", err)
	}

	// 首页给的是「前端未部署」，而不是笼统的 not found
	w := get(t, r, "/")
	if w.Code != http.StatusNotFound {
		t.Fatalf("返回 %d，期望 404", w.Code)
	}
	if e := decode(t, w); e.Msg != "error.frontendDisabled" {
		t.Errorf("msg = %q，期望 error.frontendDisabled", e.Msg)
	}

	// 但 API 与探活必须照常工作——这才是纯 API 模式的用途
	if w := get(t, r, "/healthz"); w.Code != http.StatusOK {
		t.Errorf("/healthz 返回 %d，期望 200", w.Code)
	}
	if w := send(t, r, http.MethodPost, "/api/v1/auth/login",
		`{"username":"admin","password":"admin123"}`); w.Code != http.StatusOK {
		t.Errorf("登录返回 %d，期望 200（纯 API 模式下接口必须照常可用）", w.Code)
	}
	// 未知 API 路径仍是 notFound：它有可能是前端写错了地址
	if e := decode(t, get(t, r, "/api/v1/nope")); e.Msg != "error.notFound" {
		t.Errorf("未知 API 的 msg = %q，期望 error.notFound", e.Msg)
	}
}

// TestTrustedProxiesHonourForwardedFor 覆盖反向代理下的来源 IP 判定。
//
// 默认不信任任何 X-Forwarded-For（防伪造）；但声明了可信代理之后必须采信它，
// 否则 nginx 后面所有请求的 ClientIP 都是 127.0.0.1，
// 按 IP 限流会退化成全局限流：一个人刷满配额，所有人一起被拒。
func TestTrustedProxiesHonourForwardedFor(t *testing.T) {
	clientIP := func(t *testing.T, trusted []string) string {
		t.Helper()
		deps := testDeps(t, testWebDir(t))
		deps.TrustedProxies = trusted
		r, _, err := NewRouter(deps)
		if err != nil {
			t.Fatal(err)
		}

		var got string
		r.GET("/whoami", func(c *gin.Context) { got = c.ClientIP() })

		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		req.RemoteAddr = "127.0.0.1:5555" // 请求来自本机的反向代理
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		r.ServeHTTP(httptest.NewRecorder(), req)
		return got
	}

	if got := clientIP(t, nil); got != "127.0.0.1" {
		t.Errorf("默认（不信任代理）时 ClientIP = %q，期望 127.0.0.1（必须忽略可伪造的头）", got)
	}
	if got := clientIP(t, []string{"127.0.0.1"}); got != "203.0.113.7" {
		t.Errorf("声明可信代理后 ClientIP = %q，期望 203.0.113.7（否则限流退化成全局）", got)
	}
}
