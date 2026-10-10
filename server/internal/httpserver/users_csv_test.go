package httpserver

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// importPassword 是导入时整批共用的初始密码。
const importPassword = "batch-password"

// importReport 对应 Import 返回的 data。
type importReport struct {
	Total    int `json:"total"`
	Valid    int `json:"valid"`
	Imported int `json:"imported"`
	Errors   []struct {
		Row   int    `json:"row"`
		Field string `json:"field"`
		Rule  string `json:"rule"`
		Param string `json:"param"`
	} `json:"errors"`
}

// importRequest 拼一个带文件的 multipart 请求并发出。
// 不能复用 call()：它写死了 application/json，而 multipart 的
// Content-Type 里必须带 boundary。
func importRequest(
	t *testing.T, r *gin.Engine, s sessionInfo, fileContent string, dryRun bool,
) *httptest.ResponseRecorder {
	t.Helper()
	return importRequestWithPassword(t, r, s, fileContent, importPassword, dryRun)
}

// importRequestWithPassword 同上，但能换掉那个初始密码。
// 密码的长度上限只能在这一层试探：binding 的 max=72 数的是**字符**。
func importRequestWithPassword(
	t *testing.T, r *gin.Engine, s sessionInfo, fileContent, password string, dryRun bool,
) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("password", password); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "users.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(fileContent)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	path := "/api/v1/users/import"
	if dryRun {
		path += "?dry_run=1"
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", s.csrfToken)
	req.AddCookie(&http.Cookie{Name: "mr_session", Value: s.cookie})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// parseImportReport 解出导入报告；非 200 时归零，交给调用方断言状态码。
func parseImportReport(t *testing.T, w *httptest.ResponseRecorder) importReport {
	t.Helper()

	var report importReport
	if w.Code != http.StatusOK {
		return report
	}
	if err := json.Unmarshal(decode(t, w).Data, &report); err != nil {
		t.Fatalf("data 不是导入报告: %v（%s）", err, w.Body.String())
	}
	return report
}

// readCSV 解析导出结果。用标准库而不是 csvx：验证的应当是「写出去的文件
// 到底长什么样」，拿被测代码自己的解析器去读，两边错成一样也照样通过。
func readCSV(t *testing.T, body string) [][]string {
	t.Helper()

	recs, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("导出的 body 不是合法 CSV: %v（%s）", err, body)
	}
	return recs
}

// ---------- 导出 ----------

func TestExportUsersWritesCSV(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	createUser(t, r, admin, "alice", "alice-password")

	w := call(t, r, http.MethodGet, "/api/v1/users/export", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("导出返回 %d，期望 200（%s）", w.Code, w.Body.String())
	}

	// 是文件下载而不是 JSON：浏览器靠这两个头决定「存盘」而不是「渲染」
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q，期望 text/csv", ct)
	}
	disp := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disp, "attachment") || !strings.Contains(disp, "users-") {
		t.Errorf("Content-Disposition = %q，期望 attachment + users-*.csv", disp)
	}

	body := w.Body.String()
	// BOM：Excel 双击打开靠它判断编码，没有它中文是乱码
	if !strings.HasPrefix(body, "\ufeff") {
		t.Error("导出的 CSV 没有 BOM")
	}
	// 密码列绝不能出现：哈希导出去没意义，明文更不该出。
	// 顺带挡住「以后有人图省事把 password 加进 userExportHeader」。
	if strings.Contains(strings.ToLower(body), "password") {
		t.Errorf("导出的 CSV 里出现了密码相关列: %s", body)
	}

	recs := readCSV(t, strings.TrimPrefix(body, "\ufeff"))
	if len(recs) < 2 {
		t.Fatalf("导出的 CSV 只有 %d 行，期望表头 + 至少一条数据（种子管理员）", len(recs))
	}
	header := map[string]int{}
	for i, name := range recs[0] {
		header[name] = i
	}
	for _, col := range []string{"username", "nickname", "status", "created_at"} {
		if _, ok := header[col]; !ok {
			t.Errorf("导出的表头缺少 %s 列: %v", col, recs[0])
		}
	}

	found := false
	for _, rec := range recs[1:] {
		if rec[header["username"]] != "alice" {
			continue
		}
		found = true
		if rec[header["nickname"]] != "alice" {
			t.Errorf("alice 的昵称 = %q，期望 alice", rec[header["nickname"]])
		}
		if rec[header["status"]] != "active" {
			t.Errorf("alice 的状态 = %q，期望 active", rec[header["status"]])
		}
		// 时间是 RFC3339（带时区），不是 Excel 那种没时区的写法
		if created := rec[header["created_at"]]; !strings.Contains(created, "T") ||
			!strings.HasSuffix(created, "Z") {
			t.Errorf("created_at = %q，期望 RFC3339 UTC", created)
		}
	}
	if !found {
		t.Errorf("导出的 CSV 里没有 alice: %s", body)
	}
}

// TestExportUsersFollowsFilter 导出必须跟列表用同一套筛选：
// 看的是筛过的记录，导出的却是全部，是审计场景里最容易被忽略的错。
func TestExportUsersFollowsFilter(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	createUser(t, r, admin, "alice", "alice-password")
	createUser(t, r, admin, "bob", "bob-password")

	w := call(t, r, http.MethodGet, "/api/v1/users/export?username=alice", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("导出返回 %d: %s", w.Code, w.Body.String())
	}
	recs := readCSV(t, strings.TrimPrefix(w.Body.String(), "\ufeff"))
	if len(recs) != 2 {
		t.Fatalf("筛选 alice 后导出了 %d 行数据，期望 1 行: %v", len(recs)-1, recs)
	}
	if recs[1][0] != "alice" {
		t.Errorf("导出的第一列 = %q，期望 alice", recs[1][0])
	}
}

// TestExportOperLogs 覆盖操作日志导出。
//
// 日志是批量落库的，所以先显式刷盘（testLogSvc），否则导出的是个空文件——
// 那样用例会因为在测「刷盘时机」而变得脆弱。
func TestExportOperLogs(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	createUser(t, r, admin, "logged-user", "logged-password")
	if err := testLogSvc.Flush(t.Context()); err != nil {
		t.Fatalf("刷盘失败: %v", err)
	}

	// 带筛选导出，顺便确认筛选参数接上了
	w := call(t, r, http.MethodGet, "/api/v1/oper-logs/export?path=/users", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("导出返回 %d，期望 200（%s）", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q，期望 text/csv", ct)
	}

	body := w.Body.String()
	recs := readCSV(t, strings.TrimPrefix(body, "\ufeff"))
	if len(recs) < 2 {
		t.Fatalf("导出的操作日志只有 %d 行: %s", len(recs), body)
	}
	header := recs[0]
	// user_agent 是列表页不显示、导出才有的列：追查「这条请求是不是人点的」全靠它
	if !strings.Contains(strings.Join(header, ","), "user_agent") {
		t.Errorf("导出的表头没有 user_agent 列: %v", header)
	}
	for _, rec := range recs[1:] {
		if len(rec) != len(header) {
			t.Fatalf("行列数与表头不一致: %v", rec)
		}
	}
	if !strings.Contains(body, "/api/v1/users") {
		t.Errorf("筛选 /users 后没有导出对应的记录: %s", body)
	}
}

// TestExportRolesAndLoginLogs 覆盖角色与登录日志两个导出端点。
//
// 两个端点的共同点是筛选必须与列表一致（导出的就是屏幕上的那些行），
// 所以两段都带筛选参数断言；登录日志另断言 reason 是 i18n 键而非译文——
// 导出的文件要能被程序再解析，翻成中文反而丢了结构。
func TestExportRolesAndLoginLogs(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	submitLogin(t, r, "admin", "wrong-password")
	if err := testLogSvc.Flush(t.Context()); err != nil {
		t.Fatalf("刷盘失败: %v", err)
	}

	w := call(t, r, http.MethodGet, "/api/v1/roles/export?code=admin", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("角色导出返回 %d，期望 200（%s）", w.Code, w.Body.String())
	}
	body := strings.TrimPrefix(w.Body.String(), "\ufeff")
	recs := readCSV(t, body)
	if len(recs) < 2 {
		t.Fatalf("导出的角色只有 %d 行: %s", len(recs), body)
	}
	if got, want := strings.Join(recs[0], ","), "id,created_at,updated_at,status,code,name,remark"; got != want {
		t.Errorf("角色表头 = %q，期望 %q", got, want)
	}
	if !strings.Contains(body, "admin") {
		t.Errorf("筛选 code=admin 后没有导出对应的角色: %s", body)
	}
	for _, rec := range recs[1:] {
		if len(rec) != len(recs[0]) {
			t.Fatalf("行列数与表头不一致: %v", rec)
		}
	}

	w = call(t, r, http.MethodGet, "/api/v1/login-logs/export?status=failed", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("登录日志导出返回 %d，期望 200（%s）", w.Code, w.Body.String())
	}
	body = strings.TrimPrefix(w.Body.String(), "\ufeff")
	recs = readCSV(t, body)
	if len(recs) < 2 {
		t.Fatalf("导出的登录日志只有 %d 行: %s", len(recs), body)
	}
	if got, want := strings.Join(recs[0], ","), "created_at,username,status,reason,ip,user_agent"; got != want {
		t.Errorf("登录日志表头 = %q，期望 %q", got, want)
	}
	for _, rec := range recs[1:] {
		if len(rec) != len(recs[0]) {
			t.Fatalf("行列数与表头不一致: %v", rec)
		}
		if rec[2] != "failed" {
			t.Errorf("筛选 status=failed 却导出了 %q: %v", rec[2], rec)
		}
		if rec[3] == "" {
			t.Errorf("失败记录缺 reason: %v", rec)
		}
	}
	if !strings.Contains(body, "error.badCredentials") {
		t.Errorf("reason 应导出 i18n 键而非译文: %s", body)
	}
}

// ---------- 导入 ----------

func TestImportUsersCreatesAllRows(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	csvText := "username,nickname,mobile,email,status\n" +
		"imp-alice,爱丽丝,13800000001,alice@example.com,active\n" +
		"imp-bob,鲍勃,,,inactive\n"

	report := parseImportReport(t, importRequest(t, r, admin, csvText, false))
	if report.Total != 2 || report.Valid != 2 || report.Imported != 2 {
		t.Fatalf("报告 = %+v，期望 total/valid/imported 都是 2", report)
	}
	if len(report.Errors) != 0 {
		t.Errorf("本该全部成功的导入报了错: %+v", report.Errors)
	}

	// 见到库里才算数
	w := call(t, r, http.MethodGet, "/api/v1/users?username=imp-", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("查列表返回 %d: %s", w.Code, w.Body.String())
	}
	var page struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(decode(t, w).Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Errorf("导入后库里有 %d 个用户，期望 2", page.Total)
	}

	// 整批共用的密码必须真的能用：否则导入成功的用户一个都登不进来
	login(t, r, "imp-alice", importPassword)
}

// TestImportUsersDryRunReportsWithoutWriting 覆盖「校验」这一步。
//
// dryRun 的价值在于先给一份与真正导入**同形**的报告：预览说没问题、
// 导入却报一堆错，那这个预览还不如没有。
func TestImportUsersDryRunReportsWithoutWriting(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 第 3 行的用户名只有 1 个字符（min=2）
	csvText := "username,nickname\nimp-carol,卡罗尔\na,太短\n"
	report := parseImportReport(t, importRequest(t, r, admin, csvText, true))

	if report.Total != 2 || report.Valid != 1 || report.Imported != 0 {
		t.Errorf("校验报告 = %+v，期望 total=2 valid=1 imported=0", report)
	}
	if len(report.Errors) != 1 || report.Errors[0].Row != 3 {
		t.Errorf("逐行错误 = %+v，期望只有第 3 行的 username 太短", report.Errors)
	}

	// 校验不该写库——写了就变成「点了预览，数据就已经进去了」
	w := call(t, r, http.MethodGet, "/api/v1/users?username=imp-carol", "", admin)
	var page struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(decode(t, w).Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("dry_run 之后库里出现了 %d 个用户", page.Total)
	}
}

// TestImportUsersWritesNothingWhenOneRowFails 是导入最关键的约束。
//
// 一行不合格就一行都不写：整批一个事务，避免「导了一半」——
// 列表里多了几十个用户，哪些进来了没人说得清。
func TestImportUsersWritesNothingWhenOneRowFails(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	// 一行合法两行有问题（用户名太短、邮箱不合法、与库里已有的重名）
	createUser(t, r, admin, "taken", "taken-password")
	csvText := "username,nickname,email\n" +
		"imp-dave,戴夫,\n" +
		"x,太短,\n" +
		"imp-eve,伊芙,not-an-email\n" +
		"taken,重名,\n"

	report := parseImportReport(t, importRequest(t, r, admin, csvText, false))
	if report.Imported != 0 {
		t.Fatalf("有不合格的行时导入了 %d 行，期望 0", report.Imported)
	}
	if report.Total != 4 || report.Valid != 1 {
		t.Errorf("报告 = %+v，期望 total=4 valid=1", report)
	}
	if len(report.Errors) != 3 {
		t.Fatalf("逐行错误 = %+v，期望 3 条", report.Errors)
	}

	// 行号必须指回文件里那一行，否则用户无从下手
	byRow := map[int]string{}
	for _, e := range report.Errors {
		if e.Field != "username" && e.Field != "email" {
			t.Errorf("第 %d 行的错误字段 = %q", e.Row, e.Field)
		}
		byRow[e.Row] = e.Rule
	}
	if byRow[3] != "min" {
		t.Errorf("第 3 行（x）的规则 = %q，期望 min", byRow[3])
	}
	if byRow[4] != "email" {
		t.Errorf("第 4 行的规则 = %q，期望 email", byRow[4])
	}
	if byRow[5] != "unique" {
		t.Errorf("第 5 行（与库里重名）的规则 = %q，期望 unique", byRow[5])
	}

	// 那一行合法的也不能进去
	w := call(t, r, http.MethodGet, "/api/v1/users?username=imp-dave", "", admin)
	var page struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(decode(t, w).Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("整批失败时仍然写入了 %d 行", page.Total)
	}
}

// TestImportUsersReportsDuplicateRowsInFile 覆盖同一个文件里的重复。
//
// 数据库的唯一索引是最后一道防线，但那时行号已经丢了；用户拿到
// 「username 已存在」却不知道是哪两行撞了。
func TestImportUsersReportsDuplicateRowsInFile(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	csvText := "username\nimp-frank\nimp-frank\nimp-frank\n"
	report := parseImportReport(t, importRequest(t, r, admin, csvText, false))

	if len(report.Errors) != 2 {
		t.Fatalf("逐行错误 = %+v，期望后两行各一条", report.Errors)
	}
	for i, e := range report.Errors {
		if e.Rule != "duplicateRow" {
			t.Errorf("第 %d 条错误的规则 = %q，期望 duplicateRow", i, e.Rule)
		}
		if e.Param != "2" {
			t.Errorf("第 %d 条错误指向的首次出现行号 = %q，期望 2", i, e.Param)
		}
	}
	if report.Errors[0].Row != 3 || report.Errors[1].Row != 4 {
		t.Errorf("行号 = %d/%d，期望 3/4", report.Errors[0].Row, report.Errors[1].Row)
	}
}

// TestImportUsersAcceptsExportedFile 覆盖真实用法：导出 → 改一改 → 导回。
//
// 导出的文件里有 login_ip、login_at 这些导入不认的列，
// 它们必须被忽略而不是当成缺列或错列。
func TestImportUsersAcceptsExportedFile(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	createUser(t, r, admin, "roundtrip", "roundtrip-password")

	w := call(t, r, http.MethodGet, "/api/v1/users/export?username=roundtrip", "", admin)
	if w.Code != http.StatusOK {
		t.Fatalf("导出返回 %d: %s", w.Code, w.Body.String())
	}
	exported := w.Body.String()

	// 原样导回去：用户名已存在，报告应当只有一条 unique 错误，
	// 而不是「缺列」或「列错位」这类配置问题
	report := parseImportReport(t, importRequest(t, r, admin, exported, true))
	if report.Total != 1 {
		t.Fatalf("导出的文件解析出 %d 行，期望 1: %s", report.Total, exported)
	}
	if len(report.Errors) != 1 || report.Errors[0].Rule != "unique" {
		t.Errorf("把导出文件导回去的报告 = %+v，期望只有一条 unique", report)
	}
}

// TestImportUsersKeepsEscapedValues 盯住「导出 → 改 → 导回」这条路的值保真。
//
// 导出时 +8613800000000 会被写成 '+8613800000000（公式注入防护），导入时
// 那个引号必须被剥掉。没有 csvx.Unescape 的话它就会留在库里，
// 而 mobile 只校验长度不校验格式，静默存进去，谁也不会发现。
func TestImportUsersKeepsEscapedValues(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	w := call(t, r, http.MethodPost, "/api/v1/users",
		`{"username":"rt-orig","password":"rt-password","mobile":"+8613800000000"}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("建号返回 %d: %s", w.Code, w.Body.String())
	}

	exported := ""
	if w := call(t, r, http.MethodGet, "/api/v1/users/export?username=rt-orig", "", admin); w.Code == http.StatusOK {
		exported = w.Body.String()
	} else {
		t.Fatalf("导出返回 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(exported, "'+8613800000000") {
		t.Fatalf("导出没有转义手机号（公式注入防护失效？）: %s", exported)
	}

	// 改掉用户名（避开唯一冲突）后导回，别的列一个字都不动
	bounced := strings.Replace(exported, "rt-orig", "rt-copy", 1)
	report := parseImportReport(t, importRequest(t, r, admin, bounced, false))
	if report.Imported != 1 || len(report.Errors) != 0 {
		t.Fatalf("导回报告 = %+v，期望导入 1 行无错", report)
	}

	var page struct {
		List []struct {
			Mobile string `json:"mobile"`
		} `json:"list"`
	}
	if err := json.Unmarshal(decode(t, call(t, r, http.MethodGet, "/api/v1/users?username=rt-copy", "", admin)).Data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.List) != 1 {
		t.Fatalf("导回后查不到 rt-copy，列表 = %+v", page.List)
	}
	if page.List[0].Mobile != "+8613800000000" {
		t.Errorf("导回后 mobile = %q，期望 %q（转义没被剥掉）", page.List[0].Mobile, "+8613800000000")
	}
}

// TestImportRejectsPasswordOver72Bytes 盯住非 ASCII 长密码：
// 25 个汉字是 25 个字符、75 个字节，过得了 binding:"max=72"，过不了 bcrypt。
// 这个差别必须回 400「密码太长」，不能落到 500。
func TestImportRejectsPasswordOver72Bytes(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	csvText := "username,nickname\nlongpw,\n"
	w := importRequestWithPassword(t, r, admin, csvText, strings.Repeat("密", 25), false)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("长密码返回 %d，期望 400（而不是 500）: %s", w.Code, w.Body.String())
	}
	if e := decode(t, w); e.Msg != "error.passwordTooLong" {
		t.Errorf("msg = %q，期望 error.passwordTooLong", e.Msg)
	}
}

// TestImportUsersRejectsBadRequests 覆盖真正算请求失败的情况：
// 报告放不下这些信息（没有行号可报），所以走 4xx + i18n 键。
func TestImportUsersRejectsBadRequests(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	t.Run("没有文件", func(t *testing.T) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if err := mw.WriteField("password", importPassword); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/import", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-CSRF-Token", admin.csrfToken)
		req.AddCookie(&http.Cookie{Name: "mr_session", Value: admin.cookie})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("返回 %d，期望 400（%s）", w.Code, w.Body.String())
		}
		if e := decode(t, w); e.Msg != "error.importNoFile" {
			t.Errorf("msg = %q，期望 error.importNoFile", e.Msg)
		}
	})

	t.Run("缺少 username 列", func(t *testing.T) {
		w := importRequest(t, r, admin, "nickname\n张三\n", false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("返回 %d，期望 400（%s）", w.Code, w.Body.String())
		}
		if e := decode(t, w); e.Msg != "error.importMissingColumns" {
			t.Errorf("msg = %q，期望 error.importMissingColumns", e.Msg)
		}
	})

	t.Run("行数超限", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString("username\n")
		for i := 0; i < 501; i++ {
			sb.WriteString("imp-user-")
			sb.WriteString(itoa(int64(i)))
			sb.WriteString("\n")
		}
		w := importRequest(t, r, admin, sb.String(), false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("返回 %d，期望 400（%s）", w.Code, w.Body.String())
		}
		if e := decode(t, w); e.Msg != "error.importTooManyRows" {
			t.Errorf("msg = %q，期望 error.importTooManyRows", e.Msg)
		}
	})

	t.Run("空文件", func(t *testing.T) {
		w := importRequest(t, r, admin, "", false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("返回 %d，期望 400", w.Code)
		}
		if e := decode(t, w); e.Msg != "error.malformedBody" {
			t.Errorf("msg = %q，期望 error.malformedBody", e.Msg)
		}
	})

	t.Run("某行列数与表头不一致", func(t *testing.T) {
		w := importRequest(t, r, admin, "username,nickname\na,b,c\n", false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("返回 %d，期望 400", w.Code)
		}
		if e := decode(t, w); e.Msg != "error.malformedBody" {
			t.Errorf("msg = %q，期望 error.malformedBody", e.Msg)
		}
	})

	t.Run("密码太短", func(t *testing.T) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if err := mw.WriteField("password", "short"); err != nil {
			t.Fatal(err)
		}
		fw, err := mw.CreateFormFile("file", "users.csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte("username\nsomeone\n")); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/import", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-CSRF-Token", admin.csrfToken)
		req.AddCookie(&http.Cookie{Name: "mr_session", Value: admin.cookie})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("返回 %d，期望 400（%s）", w.Code, w.Body.String())
		}
		e := decode(t, w)
		if e.Msg != "error.validationFailed" {
			t.Fatalf("msg = %q，期望 error.validationFailed", e.Msg)
		}
		// 字段名必须是 json tag（password）而不是 Go 字段名（Password）
		if len(e.Errors) == 0 || e.Errors[0].Field != "password" {
			t.Errorf("字段错误 = %+v，期望字段名 password", e.Errors)
		}
	})
}

// TestCSVEndpointsRequirePermission 确认这些导出端点都挡在权限后面，
// 且没有权限的用户拿到的是 403 而不是「能调但看不到数据」。
func TestCSVEndpointsRequirePermission(t *testing.T) {
	r, _ := newTestRouter(t)
	admin := login(t, r, "admin", "admin123")

	createUser(t, r, admin, "nobody", "nobody-password")
	plain := login(t, r, "nobody", "nobody-password")

	for _, path := range []string{
		"/api/v1/users/export",
		"/api/v1/roles/export",
		"/api/v1/oper-logs/export",
		"/api/v1/login-logs/export",
	} {
		if w := call(t, r, http.MethodGet, path, "", plain); w.Code != http.StatusForbidden {
			t.Errorf("无权限 GET %s 返回 %d，期望 403", path, w.Code)
		}
		if w := send(t, r, http.MethodGet, path, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("未登录 GET %s 返回 %d，期望 401", path, w.Code)
		}
	}
	if w := importRequest(t, r, plain, "username\nsomeone\n", false); w.Code != http.StatusForbidden {
		t.Errorf("无权限导入返回 %d，期望 403", w.Code)
	}
}
