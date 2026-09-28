package httpx

import (
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
		path := filepath.Join("..", "..", "..", "web", "src", "lib", "i18n", name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("跳过：读不到前端字典 %s（%v）。该用例需要完整的仓库布局。", path, err)
		}

		have := map[string]bool{}
		for _, m := range regexp.MustCompile(`'([^']+)'\s*:`).FindAllStringSubmatch(string(body), -1) {
			have[m[1]] = true
		}
		for _, key := range keys {
			if !have[key] {
				t.Errorf("%s 缺少错误键 %q", name, key)
			}
		}
	}
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
