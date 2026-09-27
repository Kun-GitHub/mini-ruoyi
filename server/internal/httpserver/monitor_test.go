package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// ---------- 会话管理 ----------

// TestSessionListAndKick 覆盖「踢人下线」的完整链路。
//
// 这是 sys_sessions 表存在的主要理由：服务端持有会话状态，
// 删除才能立即生效——JWT 那种无状态方案做不到这点。
func TestSessionListAndKick(t *testing.T) {
	r, _ := newTestRouter(t)

	admin := login(t, r, "admin", "admin123")
	createUser(t, r, admin, "sessionuser", "session-pw")
	victim := login(t, r, "sessionuser", "session-pw")

	// 两条会话都该在列表里
	w := call(t, r, http.MethodGet, "/api/v1/sessions", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("会话列表返回 %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Data struct {
			Total int64 `json:"total"`
			List  []struct {
				TokenHash string `json:"token_hash"`
				Username  string `json:"username"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Data.Total != 2 {
		t.Fatalf("在线会话 %d 条，期望 2", list.Data.Total)
	}

	// 找出被踢对象那条会话的 hash
	var victimHash string
	for _, s := range list.Data.List {
		if s.Username == "sessionuser" {
			victimHash = s.TokenHash
		}
	}
	if victimHash == "" {
		t.Fatal("会话列表里没有 sessionuser 的会话")
	}

	// 踢掉它
	if w := call(t, r, http.MethodDelete, "/api/v1/sessions/"+victimHash, "", admin); w.Code != http.StatusOK {
		t.Fatalf("踢会话返回 %d: %s", w.Code, w.Body.String())
	}

	// 对方的会话立刻失效——这就是「服务端持有状态」的意义
	if w := call(t, r, http.MethodGet, "/api/v1/auth/me", "", victim); w.Code != http.StatusUnauthorized {
		t.Errorf("被踢后对方仍能访问 /auth/me（返回 %d），期望 401", w.Code)
	}
	// 操作者自己不受影响
	if w := call(t, r, http.MethodGet, "/api/v1/auth/me", "", admin); w.Code != http.StatusOK {
		t.Errorf("踢人者自己被登出了（返回 %d）", w.Code)
	}
}

// TestCannotKickOwnSession 与「不能删自己」同理：踢自己就是突然被登出，
// 而用户本来就有退出登录按钮。
func TestCannotKickOwnSession(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodGet, "/api/v1/sessions", "", admin)
	var list struct {
		Data struct {
			List []struct {
				TokenHash string `json:"token_hash"`
			} `json:"list"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Data.List) == 0 {
		t.Fatal("会话列表为空")
	}

	own := list.Data.List[0].TokenHash
	w = call(t, r, http.MethodDelete, "/api/v1/sessions/"+own, "", admin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("踢自己返回 %d，期望 403: %s", w.Code, w.Body.String())
	}
	if e := decode(t, w); e.Msg != "error.cannotKickSelf" {
		t.Errorf("msg = %q，期望 error.cannotKickSelf", e.Msg)
	}
}

func TestKickNonExistentSessionReturns404(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodDelete, "/api/v1/sessions/does-not-exist", "", admin)
	if w.Code != http.StatusNotFound {
		t.Errorf("踢不存在的会话返回 %d，期望 404（不能返回 200 让人以为踢成功了）", w.Code)
	}
}

// TestKickUserSessions 覆盖用户列表上的「强退」。
func TestKickUserSessions(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")
	userID := createUser(t, r, admin, "multiuser", "multi-pw")

	// 同一账号登录两次，产生两条会话
	first := login(t, r, "multiuser", "multi-pw")
	_ = login(t, r, "multiuser", "multi-pw")

	if w := call(t, r, http.MethodDelete, "/api/v1/users/"+itoa(userID)+"/sessions", "", admin); w.Code != http.StatusOK {
		// 路径是 /api/v1/users/:id/sessions
		t.Fatalf("强退返回 %d: %s", w.Code, w.Body.String())
	}
	if w := call(t, r, http.MethodGet, "/api/v1/auth/me", "", first); w.Code != http.StatusUnauthorized {
		t.Errorf("强退后旧会话仍有效（返回 %d）", w.Code)
	}
}

func TestCannotForceLogoutSelf(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodDelete, "/api/v1/users/"+itoa(admin.userID)+"/sessions", "", admin)
	if w.Code != http.StatusForbidden {
		t.Errorf("强退自己返回 %d，期望 403", w.Code)
	}
}

// ---------- 日志 ----------

// submitLogin 提交一次登录尝试，不断言结果——失败的那些用例也要用它。
func submitLogin(t *testing.T, r *gin.Engine, username, password string) {
	t.Helper()
	send(t, r, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`)
}

type loginLogPage struct {
	Data struct {
		Total int64 `json:"total"`
		List  []struct {
			Username string `json:"username"`
			Status   string `json:"status"`
			Reason   string `json:"reason"`
			IP       string `json:"ip"`
		} `json:"list"`
	} `json:"data"`
}

type operLogPage struct {
	Data struct {
		Total int64 `json:"total"`
		List  []struct {
			Username string `json:"username"`
			Method   string `json:"method"`
			Path     string `json:"path"`
			Status   int    `json:"status"`
			Result   string `json:"result"`
		} `json:"list"`
	} `json:"data"`
}

// TestLoginLogsRecordSuccessAndFailure 覆盖登录日志。
//
// 失败也要记：只记成功的话，「有人在暴力破解」这件事完全看不出来。
func TestLoginLogsRecordSuccessAndFailure(t *testing.T) {
	deps := testDeps(t, testWebDir(t))
	r, table, err := NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	testRouteTable = table

	admin := login(t, r, "admin", "admin123")
	submitLogin(t, r, "admin", "wrong-password")
	submitLogin(t, r, "nobody", "whatever")

	// 日志是批量落库的，测试里显式刷一次，免得等 2 秒
	if err := testLogSvc.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	w := call(t, r, http.MethodGet, "/api/v1/login-logs", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("登录日志返回 %d: %s", w.Code, w.Body.String())
	}
	var page loginLogPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}

	var success, failed int
	for _, l := range page.Data.List {
		switch l.Status {
		case "success":
			success++
		case "failed":
			failed++
			// 失败原因用的是 i18n 键，与响应体保持一致
			if l.Reason == "" {
				t.Errorf("失败记录 %+v 缺少 reason", l)
			}
		}
	}
	if success == 0 {
		t.Error("没有记录成功的登录")
	}
	if failed < 2 {
		t.Errorf("记录了 %d 条失败登录，期望至少 2 条（密码错误 + 用户不存在）", failed)
	}
}

// TestOperLogsRecordWritesAndDenials 覆盖操作日志的记录范围。
func TestOperLogsRecordWritesAndDenials(t *testing.T) {
	deps := testDeps(t, testWebDir(t))
	r, table, err := NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	testRouteTable = table

	admin := login(t, r, "admin", "admin123")

	// 一次成功的写操作
	createUser(t, r, admin, "logtarget", "logtarget-pw")

	// 一次读操作——不该被记录
	call(t, r, http.MethodGet, "/api/v1/users", "", admin)

	// 一次被拒绝的写操作（越权）：用一个没有权限的账号
	createUser(t, r, admin, "noperm", "noperm-pw")
	noPerm := login(t, r, "noperm", "noperm-pw")
	call(t, r, http.MethodPost, "/api/v1/users", `{"username":"sneaky","password":"sneaky-pw"}`, noPerm)

	if err := testLogSvc.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	w := call(t, r, http.MethodGet, "/api/v1/oper-logs", "", admin)
	var page operLogPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}

	var sawCreate, sawRead, sawDenied bool
	for _, l := range page.Data.List {
		if l.Method == http.MethodGet {
			sawRead = true
		}
		if l.Username == "admin" && l.Method == http.MethodPost && l.Path == "/api/v1/users" {
			sawCreate = true
		}
		if l.Username == "noperm" && l.Status == http.StatusForbidden {
			sawDenied = true
			if l.Result != "error.forbidden" {
				t.Errorf("越权记录的 result = %q，期望 error.forbidden", l.Result)
			}
		}
	}

	if !sawCreate {
		t.Error("成功的写操作没有被记录")
	}
	if !sawDenied {
		t.Error("被拒绝的写操作没有被记录（越权试探是最该留痕的）")
	}
	if sawRead {
		t.Error("读操作被记录了——日志量会因此膨胀，且 GET 不是「操作」")
	}
}

// TestOperLogDoesNotContainRequestBody 确认日志里不含请求体。
//
// 登录、改密码的请求体里是明文密码。日志表的访问权限比用户表弱，
// 写进去等于自己制造一个泄密点。
func TestOperLogDoesNotContainRequestBody(t *testing.T) {
	deps := testDeps(t, testWebDir(t))
	r, table, err := NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	testRouteTable = table

	admin := login(t, r, "admin", "admin123")
	const secret = "SuperSecret-Password-123"
	createUser(t, r, admin, "secretuser", secret)

	if err := testLogSvc.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	w := call(t, r, http.MethodGet, "/api/v1/oper-logs?page_size=100", "", admin)
	if body := w.Body.String(); strings.Contains(body, secret) {
		t.Errorf("操作日志里出现了请求体中的密码:\n%s", body)
	}
}

// TestLoginLogsRecordIPAndUserAgent 确认记下了来源——排查靠它。
func TestLoginLogsRecordSource(t *testing.T) {
	deps := testDeps(t, testWebDir(t))
	r, table, err := NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	testRouteTable = table
	admin := login(t, r, "admin", "admin123")

	if err := testLogSvc.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	w := call(t, r, http.MethodGet, "/api/v1/login-logs", "", admin)
	var page loginLogPage
	json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Data.List) == 0 {
		t.Fatal("登录日志为空")
	}
	if page.Data.List[0].IP == "" {
		t.Error("登录日志没有记录来源 IP")
	}
}
