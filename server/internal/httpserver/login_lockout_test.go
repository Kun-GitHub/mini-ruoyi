// 有问题欢迎联系 hot_kun@hotmail.com
package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/auth"
)

// loginTry 提交一次登录尝试并返回响应体，供断言状态码与错误键。
func loginTry(t *testing.T, r *gin.Engine, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	return send(t, r, http.MethodPost, "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`)
}

func expectLogin(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantMsg string) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("登录返回 %d，期望 %d：%s", w.Code, wantStatus, w.Body.String())
	}
	if e := decode(t, w); e.Msg != wantMsg {
		t.Errorf("msg = %q，期望 %q", e.Msg, wantMsg)
	}
}

// TestLoginLocksAfterMaxFailures 覆盖登录失败锁定的触发。
//
// 只记日志不锁定（本项目此前的做法）对穷举几乎不设防：脚本可以在几秒内
// 试完上万个密码。锁定之后，**密码正确也进不去**——这一条必须钉死，
// 否则锁定了还能登录就等于没锁。
func TestLoginLocksAfterMaxFailures(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")
	createUser(t, r, admin, "lockme", "correct-password-1")

	for i := 1; i <= auth.MaxLoginFailures; i++ {
		w := loginTry(t, r, "lockme", "wrong-password")
		// 触发锁定的那一次仍然报「密码错误」，下一次才报「已锁定」
		expectLogin(t, w, http.StatusUnauthorized, "error.badCredentials")
	}

	expectLogin(t, loginTry(t, r, "lockme", "correct-password-1"),
		http.StatusTooManyRequests, "error.accountLocked")
}

// TestLoginLogRecordsLockout 确认「账号被锁」这件事在登录日志里留痕，
// 且 reason 用的就是响应里那个键——界面与日志必须显示同一句话，
// 否则对着日志排查时会看到一个界面上从来没出现过的字符串。
func TestLoginLogRecordsLockout(t *testing.T) {
	deps := testDeps(t, testWebDir(t))
	r, table, err := NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	testRouteTable = table

	admin := login(t, r, "admin", "admin123")
	createUser(t, r, admin, "locklog", "correct-password-1")

	for i := 0; i < auth.MaxLoginFailures; i++ {
		loginTry(t, r, "locklog", "wrong-password")
	}
	expectLogin(t, loginTry(t, r, "locklog", "correct-password-1"),
		http.StatusTooManyRequests, "error.accountLocked")

	// 日志是批量落库的，测试里显式刷一次
	if err := testLogSvc.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	w := call(t, r, http.MethodGet, "/api/v1/login-logs?username=locklog", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("登录日志返回 %d: %s", w.Code, w.Body.String())
	}
	var page loginLogPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Data.Total == 0 {
		t.Fatal("没有记录这次登录尝试")
	}
	// 列表按时间倒序，最后一条就是刚才那次被锁定挡住的尝试
	last := page.Data.List[0]
	if last.Status != "failed" || last.Reason != "error.accountLocked" {
		t.Errorf("最后一条 = %+v，期望 status=failed reason=error.accountLocked", last)
	}
}

// TestLoginFailureCounterResetsOnSuccess 确认计数只统计**连续**失败：
// 登录成功一次就清零。不清零的话，一个偶尔手滑的用户会在几天内攒够阈值，
// 某天突然被锁，而且他自己完全不知道为什么。
func TestLoginFailureCounterResetsOnSuccess(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")
	createUser(t, r, admin, "reset", "correct-password-1")

	// 连错几但不到阈值
	for i := 0; i < auth.MaxLoginFailures-2; i++ {
		expectLogin(t, loginTry(t, r, "reset", "wrong-password"),
			http.StatusUnauthorized, "error.badCredentials")
	}
	// 成功一次 → 计数清零，此后的失败要重新数满 MaxLoginFailures 次才锁
	expectLogin(t, loginTry(t, r, "reset", "correct-password-1"), http.StatusOK, "ok")

	for i := 1; i <= auth.MaxLoginFailures; i++ {
		expectLogin(t, loginTry(t, r, "reset", "wrong-password"),
			http.StatusUnauthorized, "error.badCredentials")
	}
	expectLogin(t, loginTry(t, r, "reset", "correct-password-1"),
		http.StatusTooManyRequests, "error.accountLocked")
}

// TestPasswordResetClearsLoginLock 确认管理员重置密码能解锁。
//
// 账号被锁之后本人无计可施，唯一自助通道就是找管理员——如果重置密码
// 不清零计数，那个人拿到新密码依然登不进来，会以为重置根本没生效。
func TestPasswordResetClearsLoginLock(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")
	id := createUser(t, r, admin, "unlockme", "correct-password-1")

	for i := 0; i < auth.MaxLoginFailures; i++ {
		loginTry(t, r, "unlockme", "wrong-password")
	}
	expectLogin(t, loginTry(t, r, "unlockme", "correct-password-1"),
		http.StatusTooManyRequests, "error.accountLocked")

	w := call(t, r, http.MethodPut, "/api/v1/users/"+strconv.FormatInt(id, 10)+"/password",
		`{"password":"brand-new-password-1"}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("重置密码返回 %d: %s", w.Code, w.Body.String())
	}

	expectLogin(t, loginTry(t, r, "unlockme", "brand-new-password-1"), http.StatusOK, "ok")
}

// TestExpiredLockUnlocksOnNextAttempt 覆盖锁定到期后的自动解锁。
//
// 这条分支是账号「还能不能自己回来」的开关：到期判断写反、或者解锁时忘了清零计数，
// 账号就永久锁死——而界面上只会一直显示「已锁定，请稍后再试」，看不出是 bug。
// 10 分钟等不起，所以直接把 locked_until 改成过去（格式与 repository.toDBTime 一致）。
func TestExpiredLockUnlocksOnNextAttempt(t *testing.T) {
	deps := testDeps(t, testWebDir(t))
	r, table, err := NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	testRouteTable = table

	admin := login(t, r, "admin", "admin123")
	createUser(t, r, admin, "expirelock", "correct-password-1")

	for i := 0; i < auth.MaxLoginFailures; i++ {
		loginTry(t, r, "expirelock", "wrong-password")
	}
	expectLogin(t, loginTry(t, r, "expirelock", "correct-password-1"),
		http.StatusTooManyRequests, "error.accountLocked")

	// 把到期时间挪到过去，等价于「锁定时间已经走完」
	if _, err := deps.DB.ExecContext(t.Context(),
		`UPDATE sys_users SET locked_until = ? WHERE username = 'expirelock'`,
		time.Now().UTC().Add(-time.Minute).Format("2006-01-02 15:04:05")); err != nil {
		t.Fatalf("把锁定时间改到过去: %v", err)
	}

	// 到期后的第一次尝试仍然输错密码：这一次必须只报「密码错误」。
	// 到期分支不清零的话，计数会从 5 直接推过阈值，账号被立刻重新锁上，
	// 下面那次正确密码也就进不来——表现就是「锁到期了也永远解不开」。
	expectLogin(t, loginTry(t, r, "expirelock", "wrong-password"),
		http.StatusUnauthorized, "error.badCredentials")

	expectLogin(t, loginTry(t, r, "expirelock", "correct-password-1"), http.StatusOK, "ok")

	// 锁定期一过，计数也该跟着清零（成功登录后读，同时钉住最终状态）
	var failedCount int
	var lockedUntil *time.Time
	if err := deps.DB.QueryRowContext(t.Context(),
		`SELECT failed_count, locked_until FROM sys_users WHERE username = 'expirelock'`).
		Scan(&failedCount, &lockedUntil); err != nil {
		t.Fatalf("读锁定状态: %v", err)
	}
	if failedCount != 0 || lockedUntil != nil {
		t.Errorf("到期解锁后 = failed_count %d / locked_until %v，期望 0 / NULL", failedCount, lockedUntil)
	}
}
