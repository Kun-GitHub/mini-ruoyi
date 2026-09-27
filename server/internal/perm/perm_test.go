package perm

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGroupsCoverAllCodesExactlyOnce(t *testing.T) {
	groups := Groups()

	seen := map[Code]int{}
	for _, g := range groups {
		if g.TitleKey == "" {
			t.Errorf("分组 %v 缺少 TitleKey", g.Codes)
		}
		for _, c := range g.Codes {
			seen[c]++
		}
	}

	for _, c := range All() {
		switch seen[c] {
		case 1:
		case 0:
			t.Errorf("权限码 %s 不属于任何分组（权限界面会看不到它）", c)
		default:
			t.Errorf("权限码 %s 出现在 %d 个分组里", c, seen[c])
		}
	}
	for c, n := range seen {
		if n > 0 && !Declared(c) {
			t.Errorf("分组里出现了未声明的权限码 %s", c)
		}
	}
}

func TestDeclared(t *testing.T) {
	if !Declared(SystemUserAdd) {
		t.Error("已声明的权限码被判定为未声明")
	}
	if Declared("system:user:bogus") {
		t.Error("未声明的权限码被判定为已声明")
	}
}

func TestLabelKeyConvention(t *testing.T) {
	cases := map[Code]string{
		SystemUserAdd:      "perm.system.user.add",
		SystemRoleList:     "perm.system.role.list",
		SystemMenuEdit:     "perm.system.menu.edit",
		SystemPermList:     "perm.system.perm.list",
		SystemUserResetPwd: "perm.system.user.resetPwd",
	}
	for code, want := range cases {
		if got := LabelKey(code); got != want {
			t.Errorf("LabelKey(%s) = %q，期望 %q", code, got, want)
		}
	}
}

func TestUnknownAndValidate(t *testing.T) {
	if got := Unknown([]string{string(SystemUserAdd), "system:user:bogus"}); len(got) != 1 || got[0] != "system:user:bogus" {
		t.Errorf("Unknown = %v，期望只报 system:user:bogus", got)
	}
	if err := Validate([]string{string(SystemUserAdd)}); err != nil {
		t.Errorf("全部已知时不应报错: %v", err)
	}
	err := Validate([]string{"nope:nope:nope"})
	if err == nil {
		t.Fatal("未知权限码应当报错")
	}
	// 错误信息要能指出是哪个码，否则运维看到日志也不知道改什么
	if !strings.Contains(err.Error(), "nope:nope:nope") {
		t.Errorf("错误信息未包含未知权限码: %v", err)
	}
}

// TestFrontendDictCoversPermKeys 检查 Go 按约定推导出的键在 zh-CN / en-US 字典里真实存在。
//
// 权限码与文案的关系是「约定耦合」：Go 侧按 system:user:add → perm.system.user.add 推导，
// 前端按同名规则登记。两侧名字写歪了不会有任何编译错误，界面只会显示原始键名。
// 这个用例就是为了让这类不一致在 CI 里失败。
//
// ⚠️ 必须用 `go test -count=1` 或 `make test` 运行：
// Go 的测试缓存不会追踪测试运行期 os.ReadFile 打开的文件，而这里的输入是
// 包目录之外的前端 TS 文件。缓存的 "ok" 可能在字典缺键之后依然生效。
// CI 是冷缓存所以不受影响，但本地直接跑 go test 会被骗过。
func TestFrontendDictCoversPermKeys(t *testing.T) {
	dicts := map[string]string{}
	for _, name := range []string{"zh-CN.ts", "en-US.ts"} {
		path := filepath.Join("..", "..", "..", "web", "src", "lib", "i18n", name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("跳过：读不到前端字典 %s（%v）。该用例需要完整的仓库布局。", path, err)
		}
		dicts[name] = string(body)
	}

	re := regexp.MustCompile(`'([^']+)'\s*:`)

	var want []string
	for _, g := range Groups() {
		want = append(want, g.TitleKey)
	}
	for _, c := range All() {
		want = append(want, LabelKey(c))
	}

	for name, body := range dicts {
		have := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			have[m[1]] = true
		}
		for _, key := range want {
			if !have[key] {
				t.Errorf("%s 缺少文案键 %q", name, key)
			}
		}
	}
}
