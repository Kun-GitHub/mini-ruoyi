package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mini-ruoyi/internal/domain"
)

// keyPattern 从 response.go 源码里提取全部错误键常量。
//
// ⚠️ 键清单必须**从源码提取**，不能在这个文件里手写一份：
// 手写的话，新增错误键时忘了同步这份列表，下面的用例就会静默通过——
// 这个坑真实发生过一次（error.fileTooLarge / error.quotaExceeded / error.invalidFile 就是这么漏掉的），
// 而漏掉的后果是界面上直接显示 error.fileTooLarge 这个原始键名。
var keyPattern = regexp.MustCompile(`Key\w*\s*=\s*"(error\.\w+)"`)

func allErrorKeys(t *testing.T) []string {
	t.Helper()

	body, err := os.ReadFile("response.go")
	if err != nil {
		t.Fatalf("读取 response.go: %v", err)
	}

	matches := keyPattern.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		t.Fatal("没从 response.go 里提取到任何错误键，提取规则可能已经失效")
	}

	seen := map[string]bool{}
	var keys []string
	for _, m := range matches {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		keys = append(keys, m[1])
	}
	return keys
}

// TestErrorKeysAreUniqueAndNonEmpty 防止复制粘贴出重复的键值
// （两个常量指向同一个字符串，会导致前端只翻译得出其中一种语义）。
func TestErrorKeysAreUniqueAndNonEmpty(t *testing.T) {
	body, err := os.ReadFile("response.go")
	if err != nil {
		t.Fatalf("读取 response.go: %v", err)
	}

	// 常量名 -> 键值，用来发现「两个常量共用一个键」
	decls := regexp.MustCompile(`(Key\w*)\s*=\s*"(error\.\w+)"`).FindAllStringSubmatch(string(body), -1)
	if len(decls) == 0 {
		t.Fatal("没提取到任何错误键常量")
	}

	byValue := map[string]string{}
	naming := regexp.MustCompile(`^error\.[a-zA-Z]+$`)
	for _, d := range decls {
		name, key := d[1], d[2]
		if !naming.MatchString(key) {
			t.Errorf("%s = %q，不符合 error.<驼峰> 的命名约定", name, key)
		}
		if prev, dup := byValue[key]; dup {
			t.Errorf("%s 与 %s 用了同一个键 %q", name, prev, key)
		}
		byValue[key] = name
	}
}

// TestFrontendDictCoversErrorKeys 检查每个错误键都在前端字典里登记了。
//
// 后端只出键、前端出文案，两侧靠字符串约定耦合。漏登记不会报错，
// 界面只会显示 "error.notFound" 这样的原始键名，所以必须由用例兜住。
//
// ⚠️ 必须用 `go test -count=1` 或 `make test` 运行，原因见
// internal/perm/perm_test.go 的 TestFrontendDictCoversPermKeys。
func TestFrontendDictCoversErrorKeys(t *testing.T) {
	keys := allErrorKeys(t)

	for _, name := range []string{"zh-CN.ts", "en-US.ts"} {
		have := frontendDict(t, name)
		for _, key := range keys {
			if !have[key] {
				t.Errorf("%s 缺少错误键 %q", name, key)
			}
		}
	}
}

// TestFrontendGeneratedErrorKeysAreTranslated 检查**前端自己产生**的错误键也在字典里。
//
// 有两个键后端从不返回，只由 client.ts 产生：请求没有得到任何应答（error.network）、
// 有响应但不是本服务的信封（error.backendUnreachable）。它们是「后端没起来」时
// 用户唯一会看到的东西，显示成原始键名尤其糟糕。
//
// 其中 backendUnreachable 恰好被 response.go 的常量覆盖到了，而 **network 没有**——
// 所以键清单必须从 client.ts 扫，只依赖 response.go 会漏掉它。
//
// ⚠️ 必须用 `go test -count=1` 或 `make test` 运行，原因见
// internal/perm/perm_test.go 的 TestFrontendDictCoversPermKeys。
func TestFrontendGeneratedErrorKeysAreTranslated(t *testing.T) {
	clientPath := filepath.Join("..", "..", "..", "web", "src", "lib", "api", "client.ts")
	body, err := os.ReadFile(clientPath)
	if err != nil {
		t.Skipf("跳过：读不到 %s（%v）。该用例需要完整的仓库布局。", clientPath, err)
	}

	// client.ts 里以字符串字面量写死的错误键
	keys := map[string]bool{}
	for _, m := range regexp.MustCompile(`'(error\.[a-zA-Z]+)'`).FindAllStringSubmatch(string(body), -1) {
		keys[m[1]] = true
	}
	if len(keys) == 0 {
		t.Fatal("没从 client.ts 里提取到错误键，本用例的正则或写法可能已经变了")
	}

	for _, name := range []string{"zh-CN.ts", "en-US.ts"} {
		have := frontendDict(t, name)
		for key := range keys {
			if !have[key] {
				t.Errorf("%s 缺少前端自产的错误键 %q（界面会直接显示这个键名）", name, key)
			}
		}
	}
}

// frontendDict 读一份前端字典，返回其中的全部键。
func frontendDict(t *testing.T, name string) map[string]bool {
	t.Helper()

	path := filepath.Join("..", "..", "..", "web", "src", "lib", "i18n", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("跳过：读不到前端字典 %s（%v）。该用例需要完整的仓库布局。", path, err)
	}

	have := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([^']+)'\s*:`).FindAllStringSubmatch(string(body), -1) {
		have[m[1]] = true
	}
	return have
}

// TestFrontendDictCoversFieldNames 检查每个可能出现在 errors[].field 里的字段名都能渲染成文案。
//
// 字段名来自后端请求结构体的 json tag（RegisterJSONFieldNames 让 gin 报 json 名
// 而不是 Go 字段名），前端在 lib/i18n/errors.ts 的 fieldLabelKeys 里登记对应文案键。
// 两侧漏一个都不会报错——界面只会显示 "field.old_password" 这样的原始键名。
//
// 字段清单**从 handler 源码扫出来**，不手写：手写的清单在新增字段时会静默过期，
// 而那正是这个用例要防的事。
//
// ⚠️ 必须用 `go test -count=1` 或 `make test` 运行，原因见
// internal/perm/perm_test.go 的 TestFrontendDictCoversPermKeys。
func TestFrontendDictCoversFieldNames(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "handler", "*.go"))
	if err != nil || len(paths) == 0 {
		t.Skipf("跳过：扫不到 handler 源码（%v）。该用例需要完整的仓库布局。", err)
	}

	// 同一行里既要有 json tag 又要有 binding tag，才算「会变成字段级校验错误的字段」
	fieldLine := regexp.MustCompile(`json:"([A-Za-z_][A-Za-z0-9_]*)[^"]*"\s*binding:"([^"]+)"`)
	// 这两个 tag 不是校验规则，不会出现在 errors[].rule 里
	notARule := map[string]bool{"omitempty": true, "dive": true}

	fields := map[string]bool{}
	rules := map[string]bool{}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", path, err)
		}
		for _, m := range fieldLine.FindAllStringSubmatch(string(body), -1) {
			fields[m[1]] = true
			for _, part := range strings.Split(m[2], ",") {
				name, _, _ := strings.Cut(part, "=")
				if name != "" && !notARule[name] {
					rules[name] = true
				}
			}
		}
	}
	if len(fields) == 0 {
		t.Fatalf("没从 %d 个 handler 文件里解析出字段，本用例的正则或 tag 写法变了", len(paths))
	}

	labelBody, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "lib", "i18n", "errors.ts"))
	if err != nil {
		t.Skipf("跳过：读不到 errors.ts（%v）。该用例需要完整的仓库布局。", err)
	}
	labeled := map[string]bool{}
	for _, m := range regexp.MustCompile(`'field\.([A-Za-z0-9_]+)'`).FindAllStringSubmatch(string(labelBody), -1) {
		labeled[m[1]] = true
	}

	dicts := map[string]string{}
	for _, name := range []string{"zh-CN.ts", "en-US.ts"} {
		path := filepath.Join("..", "..", "..", "web", "src", "lib", "i18n", name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("跳过：读不到前端字典 %s（%v）。该用例需要完整的仓库布局。", path, err)
		}
		dicts[name] = string(body)
	}

	want := make([]string, 0, len(fields))
	for f := range fields {
		want = append(want, f)
	}
	sort.Strings(want)

	for _, f := range want {
		if !labeled[f] {
			t.Errorf("lib/i18n/errors.ts 的 fieldLabelKeys 缺字段 %q（后端会报 field.%s）", f, f)
		}
		for name, body := range dicts {
			if !strings.Contains(body, "'field."+f+"'") {
				t.Errorf("%s 缺文案键 %q", name, "field."+f)
			}
		}
	}

	wantRules := make([]string, 0, len(rules))
	for r := range rules {
		wantRules = append(wantRules, r)
	}
	sort.Strings(wantRules)

	for _, r := range wantRules {
		for name, body := range dicts {
			if !strings.Contains(body, "'validation."+r+"'") {
				t.Errorf("%s 缺文案键 %q", name, "validation."+r)
			}
		}
	}
}

// TestEnvelopeCodeMatchesHTTPStatus 钉住「信封 code 恒等于 HTTP 状态码」这条不变式。
//
// 这是个刻意的选择：Go 的 int 零值是 0，若用 0 表示成功，
// 忘了给 Code 赋值的新响应路径会**静默返回成功**。
//
// 实现上靠 httpx.write() 做唯一出口保证（调用方根本碰不到 Code），
// 这条用例是用来兜住「有人绕过 write() 直接 c.JSON」的。
//
// 它比 TestEveryFailurePathRecordsResultKey 多覆盖了 **Success**——
// 那个用例只跑失败路径，成功响应从来没被断言过。
func TestEnvelopeCodeMatchesHTTPStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name       string
		call       func(c *gin.Context)
		wantStatus int
	}{
		{"Success", func(c *gin.Context) { Success(c, gin.H{"id": 1}) }, http.StatusOK},
		{"Fail/notFound", func(c *gin.Context) { Fail(c, http.StatusNotFound, KeyNotFound) }, http.StatusNotFound},
		{"Fail/forbidden", func(c *gin.Context) { Fail(c, http.StatusForbidden, KeyForbidden) }, http.StatusForbidden},
		{"Fail/tooManyRequests", func(c *gin.Context) { Fail(c, http.StatusTooManyRequests, KeyTooManyRequests) }, http.StatusTooManyRequests},
		{"FailValidation", func(c *gin.Context) {
			FailValidation(c, []FieldError{{Field: "name", Rule: "required"}})
		}, http.StatusBadRequest},
		{"FailFromError/notFound", func(c *gin.Context) { FailFromError(c, domain.ErrNotFound) }, http.StatusNotFound},
		{"FailFromError/forbidden", func(c *gin.Context) { FailFromError(c, domain.ErrForbidden) }, http.StatusForbidden},
		{"FailFromError/unauthorized", func(c *gin.Context) { FailFromError(c, domain.ErrUnauthorized) }, http.StatusUnauthorized},
		{"FailFromError/fileTooLarge", func(c *gin.Context) { FailFromError(c, domain.ErrFileTooLarge) }, http.StatusRequestEntityTooLarge},
		{"FailFromError/quota", func(c *gin.Context) { FailFromError(c, domain.ErrQuotaExceeded) }, http.StatusRequestEntityTooLarge},
		{"FailFromError/badCron", func(c *gin.Context) { FailFromError(c, domain.ErrInvalidJobCron) }, http.StatusBadRequest},
		{"FailFromError/wrongOldPassword", func(c *gin.Context) {
			FailFromError(c, domain.ErrWrongOldPassword)
		}, http.StatusBadRequest},
		{"FailFromError/lastAdmin", func(c *gin.Context) { FailFromError(c, domain.ErrLastAdmin) }, http.StatusConflict},
		{"FailFromError/internal", func(c *gin.Context) { FailFromError(c, errors.New("boom")) }, http.StatusInternalServerError},
		{"FailFromError/dependents", func(c *gin.Context) {
			FailFromError(c, domain.HasDependents(map[string]int64{"child_menus": 1}))
		}, http.StatusConflict},
		{"FailFromError/duplicate", func(c *gin.Context) {
			FailFromError(c, domain.Duplicate("username"))
		}, http.StatusConflict},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)
			tc.call(c)

			if w.Code != tc.wantStatus {
				t.Fatalf("HTTP 状态码 = %d，期望 %d", w.Code, tc.wantStatus)
			}

			var resp Response
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("响应不是 JSON: %v（body=%q）", err, w.Body.String())
			}
			if resp.Code != w.Code {
				t.Errorf("信封 code = %d，HTTP 状态码 = %d，两者必须相等", resp.Code, w.Code)
			}
			if resp.Code == 0 {
				t.Error("信封 code 为 0：这不是合法值（多半是绕过了 write()）")
			}
		})
	}
}

// TestEveryFailurePathRecordsResultKey 确认所有失败响应都记下了 i18n 键。
//
// 这个键给操作日志用（「谁在什么时候因为什么被拒绝」）。
// 漏一个失败分支的表现是日志里 result 为空——审计记录还在，
// 但「为什么失败」没了，而这恰恰是审计最关心的一半。
func TestEveryFailurePathRecordsResultKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		call func(c *gin.Context)
		want string
	}{
		{"Fail", func(c *gin.Context) { Fail(c, http.StatusNotFound, KeyNotFound) }, KeyNotFound},
		{"FailValidation", func(c *gin.Context) {
			FailValidation(c, []FieldError{{Field: "name", Rule: "required"}})
		}, KeyValidationFailed},
		{"FailFromError/notFound", func(c *gin.Context) { FailFromError(c, domain.ErrNotFound) }, KeyNotFound},
		{"FailFromError/forbidden", func(c *gin.Context) { FailFromError(c, domain.ErrForbidden) }, KeyForbidden},
		{"FailFromError/fileTooLarge", func(c *gin.Context) { FailFromError(c, domain.ErrFileTooLarge) }, KeyFileTooLarge},
		{"FailFromError/quota", func(c *gin.Context) { FailFromError(c, domain.ErrQuotaExceeded) }, KeyQuotaExceeded},
		{"FailFromError/badCron", func(c *gin.Context) { FailFromError(c, domain.ErrInvalidJobCron) }, KeyInvalidJobCron},
		{"FailFromError/unknown", func(c *gin.Context) { FailFromError(c, errors.New("boom")) }, KeyInternal},
		{"FailFromError/dependents", func(c *gin.Context) {
			FailFromError(c, domain.HasDependents(map[string]int64{"child_menus": 1}))
		}, KeyHasDependents},
		{"FailFromError/duplicate", func(c *gin.Context) {
			FailFromError(c, domain.Duplicate("username"))
		}, KeyDuplicate},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)
			tc.call(c)

			if got := ResultKey(c); got != tc.want {
				t.Errorf("ResultKey = %q，期望 %q", got, tc.want)
			}
		})
	}
}
