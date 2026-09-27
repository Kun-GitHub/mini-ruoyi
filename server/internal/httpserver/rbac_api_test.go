package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/perm"
)

// call 带会话与 CSRF 头发请求。写操作默认带 CSRF，除非显式关掉。
func call(t *testing.T, r *gin.Engine, method, path, body string, s sessionInfo, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "mr_session", Value: s.cookie})
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-CSRF-Token", s.csrfToken)
	}
	for _, opt := range opts {
		opt(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// createUser 以管理员身份建一个用户，返回其 id。
func createUser(t *testing.T, r *gin.Engine, admin sessionInfo, username, password string) int64 {
	t.Helper()
	w := call(t, r, http.MethodPost, "/api/v1/users",
		`{"username":"`+username+`","password":"`+password+`","nickname":"`+username+`"}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("创建用户 %s 失败: %d %s", username, w.Code, w.Body.String())
	}
	var res struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.Data.ID
}

// ---------- 权限隔离 ----------

// TestNonAdminIsDeniedWithoutPermission 是权限模型的核心用例。
//
// 建一个普通用户、不给任何权限，他应当：能登录、能看自己的资料，
// 但访问任何业务端点都是 403 —— 而不是 500，也不是放行。
func TestNonAdminIsDeniedWithoutPermission(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	createUser(t, r, admin, "operator", "operator-pw")

	// 普通用户没有内置角色，权限码为空
	op := login(t, r, "operator", "operator-pw")
	if op.isAdmin {
		t.Fatal("普通用户的 is_admin 不该为 true")
	}
	if len(op.perms) != 0 {
		t.Fatalf("未授权的普通用户不该拿到权限码，实际 %v", op.perms)
	}

	// 自己的资料可以看
	if w := call(t, r, http.MethodGet, "/api/v1/auth/me", "", op); w.Code != http.StatusOK {
		t.Errorf("普通用户访问 /auth/me 返回 %d，期望 200", w.Code)
	}

	// 业务端点一律 403
	for _, ep := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodGet, "/api/v1/roles"},
		{http.MethodGet, "/api/v1/menus"},
		{http.MethodGet, "/api/v1/perms"},
	} {
		w := call(t, r, ep.method, ep.path, "", op)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s 返回 %d，期望 403", ep.method, ep.path, w.Code)
		}
		if e := decode(t, w); e.Msg != "error.forbidden" {
			t.Errorf("%s %s 的 msg = %q，期望 error.forbidden", ep.method, ep.path, e.Msg)
		}
	}
}

// TestGrantedPermissionUnlocksOnlyThatEndpoint 确认授权是「按权限码精确放行」，
// 而不是「有角色就都能用」。
func TestGrantedPermissionUnlocksOnlyThatEndpoint(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")
	userID := createUser(t, r, admin, "viewer", "viewer-pw")

	// 建角色并只授予「查询用户」
	w := call(t, r, http.MethodPost, "/api/v1/roles",
		`{"code":"viewer","name":"只读"}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("建角色失败: %d %s", w.Code, w.Body.String())
	}
	var roleRes struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &roleRes)
	roleID := roleRes.Data.ID

	w = call(t, r, http.MethodPut, "/api/v1/roles/"+itoa(roleID)+"/grants",
		`{"menu_ids":[],"perm_codes":["system:user:list"]}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("授权失败: %d %s", w.Code, w.Body.String())
	}

	// 绑角色
	w = call(t, r, http.MethodPut, "/api/v1/users/"+itoa(userID)+"/roles",
		`{"role_ids":[`+itoa(roleID)+`]}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("绑角色失败: %d %s", w.Code, w.Body.String())
	}

	viewer := login(t, r, "viewer", "viewer-pw")
	if len(viewer.perms) != 1 || viewer.perms[0] != "system:user:list" {
		t.Fatalf("viewer 的权限码 = %v，期望只有 system:user:list", viewer.perms)
	}

	// 有 system:user:list → 放行
	if w := call(t, r, http.MethodGet, "/api/v1/users", "", viewer); w.Code != http.StatusOK {
		t.Errorf("有 list 权限时 GET /users 返回 %d，期望 200", w.Code)
	}
	// 没有 system:role:list → 仍然 403
	if w := call(t, r, http.MethodGet, "/api/v1/roles", "", viewer); w.Code != http.StatusForbidden {
		t.Errorf("无 role:list 权限时 GET /roles 返回 %d，期望 403", w.Code)
	}
	// 有 list 但没有 add → 写操作 403
	if w := call(t, r, http.MethodPost, "/api/v1/users",
		`{"username":"x1","password":"12345678"}`, viewer); w.Code != http.StatusForbidden {
		t.Errorf("无 user:add 权限时 POST /users 返回 %d，期望 403", w.Code)
	}
}

// TestUnknownPermCodeIsRejected 覆盖「库里不允许出现代码未声明的权限码」。
// 否则会形成一条永远不生效的授权，用户以为给了权限其实没给。
func TestUnknownPermCodeIsRejected(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodPost, "/api/v1/roles", `{"code":"temp","name":"临时"}`, admin)
	var res struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)

	w = call(t, r, http.MethodPut, "/api/v1/roles/"+itoa(res.Data.ID)+"/grants",
		`{"menu_ids":[],"perm_codes":["system:user:bogus"]}`, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未知权限码返回 %d，期望 400: %s", w.Code, w.Body.String())
	}
	if e := decode(t, w); e.Msg != "error.invalidPermCode" {
		t.Errorf("msg = %q，期望 error.invalidPermCode", e.Msg)
	}
}

// ---------- 删除守卫（HTTP 层） ----------

func TestDeleteMenuRequiresCascadeConfirmation(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 种子里的「系统管理」目录带 3 个子菜单
	w := call(t, r, http.MethodDelete, "/api/v1/menus/1", "", admin)
	if w.Code != http.StatusConflict {
		t.Fatalf("删除有子菜单的目录返回 %d，期望 409: %s", w.Code, w.Body.String())
	}
	e := decode(t, w)
	if e.Msg != "error.hasDependents" {
		t.Errorf("msg = %q，期望 error.hasDependents", e.Msg)
	}
	var impact struct {
		ChildMenus    int64 `json:"child_menus"`
		AffectedRoles int64 `json:"affected_roles"`
	}
	if err := json.Unmarshal(e.Data, &impact); err != nil {
		t.Fatalf("409 响应应携带影响面: %s", string(e.Data))
	}
	if impact.ChildMenus != 4 {
		t.Errorf("child_menus = %d，期望 4（前端弹框要展示这个数字）", impact.ChildMenus)
	}

	// 被拦下后菜单必须还在
	if w := call(t, r, http.MethodGet, "/api/v1/menus/1", "", admin); w.Code != http.StatusOK {
		t.Fatalf("被拦下后菜单不该消失: %d", w.Code)
	}

	// 带 cascade 才真的删
	w = call(t, r, http.MethodDelete, "/api/v1/menus/1?cascade=true", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("带 cascade 删除返回 %d，期望 200: %s", w.Code, w.Body.String())
	}
	var tree struct {
		Data struct {
			Tree []struct {
				TitleKey string `json:"title_key"`
				Children []any  `json:"children"`
			} `json:"tree"`
		} `json:"data"`
	}
	w = call(t, r, http.MethodGet, "/api/v1/menus", "", admin)
	json.Unmarshal(w.Body.Bytes(), &tree)

	// 被删的是「系统管理」整棵子树；另外两个目录是独立的根，必须不受影响
	if len(tree.Data.Tree) != 2 {
		t.Fatalf("级联删除后还剩 %d 个根菜单，期望 2", len(tree.Data.Tree))
	}
	var keys []string
	for _, n := range tree.Data.Tree {
		keys = append(keys, n.TitleKey)
	}
	// 顺序即 sort：系统监控(2) 在系统工具(3) 之前
	if strings.Join(keys, ",") != "menu.monitor,menu.tool" {
		t.Errorf("剩下的根菜单是 %v，期望 [menu.monitor menu.tool]（不该连带删掉别的目录）", keys)
	}
}

func TestDeleteRoleRequiresCascadeConfirmation(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 建角色并绑给一个用户
	w := call(t, r, http.MethodPost, "/api/v1/roles", `{"code":"temp","name":"临时角色"}`, admin)
	var roleRes struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &roleRes)
	roleID := roleRes.Data.ID

	userID := createUser(t, r, admin, "holder", "holder-pw")
	call(t, r, http.MethodPut, "/api/v1/users/"+itoa(userID)+"/roles",
		`{"role_ids":[`+itoa(roleID)+`]}`, admin)

	w = call(t, r, http.MethodDelete, "/api/v1/roles/"+itoa(roleID), "", admin)
	if w.Code != http.StatusConflict {
		t.Fatalf("删除被持有的角色返回 %d，期望 409", w.Code)
	}
	var impact struct {
		AffectedUsers int64 `json:"affected_users"`
	}
	if err := json.Unmarshal(decode(t, w).Data, &impact); err != nil {
		t.Fatal(err)
	}
	if impact.AffectedUsers != 1 {
		t.Errorf("affected_users = %d，期望 1", impact.AffectedUsers)
	}

	if w := call(t, r, http.MethodDelete, "/api/v1/roles/"+itoa(roleID)+"?cascade=true", "", admin); w.Code != http.StatusOK {
		t.Fatalf("带 cascade 删除返回 %d: %s", w.Code, w.Body.String())
	}
}

// TestBuiltinRoleIsProtected 确认内置角色即使带 cascade 也删不掉。
func TestBuiltinRoleIsProtected(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodDelete, "/api/v1/roles/1?cascade=true", "", admin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("删除内置角色返回 %d，期望 403: %s", w.Code, w.Body.String())
	}
	if e := decode(t, w); e.Msg != "error.protected" {
		t.Errorf("msg = %q，期望 error.protected", e.Msg)
	}
}

// TestCannotDeleteSelfOverHTTP 确认「不能删自己」用的是会话里的身份，
// 而不是请求体里传的 id。
func TestCannotDeleteSelfOverHTTP(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodDelete, "/api/v1/users/"+itoa(admin.userID), "", admin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("删除自己返回 %d，期望 403: %s", w.Code, w.Body.String())
	}
	if e := decode(t, w); e.Msg != "error.cannotDeleteSelf" {
		t.Errorf("msg = %q，期望 error.cannotDeleteSelf", e.Msg)
	}
}

// TestCannotDeleteLastAdmin 分两步覆盖：还有别的管理员时允许删，
// 只剩一个时拒绝。
//
// 这两步必须都测——只测「拒绝」的话，把守卫写成无条件拒绝也能通过。
func TestCannotDeleteLastAdmin(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 造第二个管理员来执行后续删除，从而绕开「不能删自己」
	secondID := createUser(t, r, admin, "admin2", "admin2-pw")
	if w := call(t, r, http.MethodPut, "/api/v1/users/"+itoa(secondID)+"/roles",
		`{"role_ids":[1]}`, admin); w.Code != http.StatusOK {
		t.Fatalf("给 admin2 绑管理员角色失败: %d %s", w.Code, w.Body.String())
	}
	second := login(t, r, "admin2", "admin2-pw")

	// 步骤 1：有两个管理员，删掉第一个应当成功
	if w := call(t, r, http.MethodDelete, "/api/v1/users/"+itoa(admin.userID), "", second); w.Code != http.StatusOK {
		t.Fatalf("还有另一个管理员时删除应成功，返回 %d: %s", w.Code, w.Body.String())
	}

	// 步骤 2：现在 second 是唯一的管理员。
	// 再建一个管理员来执行删除是没意义的（那又变成两个），所以直接验证
	// service 层的判定：让 second 去删「自己之外不存在第二个管理员」的场景
	// 由下面这步覆盖——先取消它自己的管理员角色，再尝试删除会失去权限，
	// 因此改用「停用最后一个管理员」这条等价路径。
	secondUser := second.userID
	w := call(t, r, http.MethodPut, "/api/v1/users/"+itoa(secondUser),
		`{"nickname":"admin2","status":"inactive"}`, second)
	if w.Code != http.StatusConflict {
		t.Fatalf("停用最后一个管理员返回 %d，期望 409: %s", w.Code, w.Body.String())
	}
	if e := decode(t, w); e.Msg != "error.lastAdmin" {
		t.Errorf("msg = %q，期望 error.lastAdmin", e.Msg)
	}
}

// ---------- 唯一性冲突 ----------

// TestDuplicateUsernameReturnsConflictWithField 覆盖唯一冲突：
// 必须是 409 + 字段名（前端据此高亮输入框），而不是 500。
func TestDuplicateUsernameReturnsConflictWithField(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	createUser(t, r, admin, "dup", "dup-password")

	w := call(t, r, http.MethodPost, "/api/v1/users",
		`{"username":"dup","password":"another-pw"}`, admin)
	if w.Code != http.StatusConflict {
		t.Fatalf("用户名重复返回 %d，期望 409: %s", w.Code, w.Body.String())
	}
	e := decode(t, w)
	if e.Msg != "error.duplicate" {
		t.Errorf("msg = %q，期望 error.duplicate", e.Msg)
	}
	if len(e.Errors) != 1 || e.Errors[0].Field != "username" || e.Errors[0].Rule != "unique" {
		t.Errorf("期望 errors=[{username,unique}]，实际 %+v", e.Errors)
	}
}

func TestDuplicateRoleCodeReturnsConflict(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodPost, "/api/v1/roles", `{"code":"admin","name":"伪造"}`, admin)
	// code=admin 是保留标识，应当先被 ErrProtected 拦下
	if w.Code != http.StatusForbidden {
		t.Errorf("用保留编码 admin 建角色返回 %d，期望 403", w.Code)
	}

	call(t, r, http.MethodPost, "/api/v1/roles", `{"code":"dup","name":"第一"}`, admin)
	w = call(t, r, http.MethodPost, "/api/v1/roles", `{"code":"dup","name":"第二"}`, admin)
	if w.Code != http.StatusConflict {
		t.Fatalf("角色编码重复返回 %d，期望 409", w.Code)
	}
	if e := decode(t, w); len(e.Errors) != 1 || e.Errors[0].Field != "code" {
		t.Errorf("期望字段 code 的冲突，实际 %+v", e.Errors)
	}
}

// ---------- 其它契约 ----------

func TestMenuRejectsInvalidParentOverHTTP(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 把目录挂到自己的子菜单下面（种子：菜单 1 是目录，2/3/4 是子菜单）
	w := call(t, r, http.MethodPut, "/api/v1/menus/1",
		`{"parent_id":2,"sort":1,"menu_type":"directory","title_key":"menu.system","status":"active"}`, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("环引用返回 %d，期望 400: %s", w.Code, w.Body.String())
	}
	if e := decode(t, w); e.Msg != "error.invalidParent" {
		t.Errorf("msg = %q，期望 error.invalidParent", e.Msg)
	}
}

func TestStatusValueIsValidated(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 非法状态值必须在校验层被挡住，而不是一路走到数据库的 CHECK 变成 500
	w := call(t, r, http.MethodPost, "/api/v1/roles",
		`{"code":"bad","name":"非法状态","status":"deleted"}`, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 status 返回 %d，期望 400（而不是 500）: %s", w.Code, w.Body.String())
	}
	if e := decode(t, w); len(e.Errors) == 0 || e.Errors[0].Field != "status" {
		t.Errorf("期望 status 的字段级错误，实际 %+v", e.Errors)
	}
}

func TestResetPasswordRevokesSessions(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	userID := createUser(t, r, admin, "victim", "victim-pw")
	victim := login(t, r, "victim", "victim-pw")

	// 改密码前 victim 的会话有效
	if w := call(t, r, http.MethodGet, "/api/v1/auth/me", "", victim); w.Code != http.StatusOK {
		t.Fatalf("改密前 /me 返回 %d", w.Code)
	}

	w := call(t, r, http.MethodPut, "/api/v1/users/"+itoa(userID)+"/password",
		`{"password":"brand-new-pw"}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("重置密码失败: %d %s", w.Code, w.Body.String())
	}

	// 改密码的常见动机就是「怀疑账号被盗」，不踢会话等于没改
	if w := call(t, r, http.MethodGet, "/api/v1/auth/me", "", victim); w.Code != http.StatusUnauthorized {
		t.Errorf("重置密码后旧会话仍有效（返回 %d），期望 401", w.Code)
	}
	// 新密码可登录
	login(t, r, "victim", "brand-new-pw")
}

func TestPermCatalogueComesFromCode(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodGet, "/api/v1/perms", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("返回 %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Data struct {
			Groups []struct {
				TitleKey string `json:"title_key"`
				Perms    []struct {
					Code     string `json:"code"`
					LabelKey string `json:"label_key"`
				} `json:"perms"`
			} `json:"groups"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Data.Groups) == 0 {
		t.Fatal("权限清单为空")
	}
	// 清单来自代码声明，而不是数据库——管理员角色在库里没有任何 sys_role_perms 行
	var total int
	for _, g := range res.Data.Groups {
		total += len(g.Perms)
		for _, p := range g.Perms {
			if p.LabelKey == "" {
				t.Errorf("权限码 %s 没有 label_key", p.Code)
			}
		}
	}
	if total != len(perm.All()) {
		t.Errorf("清单有 %d 个权限码，代码里声明了 %d 个", total, len(perm.All()))
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
