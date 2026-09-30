package repository

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 前端源码树相对本包的位置。
const (
	webPagesDir = "../../../web/src/pages"
	webDictDir  = "../../../web/src/lib/i18n"
)

// noMenuPages 是不进菜单的页面（相对 src/pages 的模块路径，不带扩展名）。
//
// 每条都必须有理由；想加新条目时先确认「这真的是故意不进菜单」，
// 而不是「加了页面忘了写菜单种子」。
var noMenuPages = map[string]string{
	"profile": "个人中心从右上角的用户菜单进入，不是菜单项",
}

// TestMenuSeedResolvesToFrontend 检查菜单种子与前端真实存在的页面、文案是否对得上。
//
// 三者是「约定耦合」：迁移里的 component 是相对 src/pages 的模块路径，title_key 是 i18n 键，
// 名字写歪了不会有任何编译错误，只会在界面上表现为「菜单点开是空白页」或「标题显示原始键名」；
// 反过来，加了页面忘了写菜单种子，页面就永远点不进去。两个方向都查，所以迁移与前端任一
// 侧被改动都会在这里失败。
//
// 与权限码那条线的关系：权限码由 perm 包的 TestFrontendDictCoversPermKeys 兜住，
// 而菜单与权限码之间没有可推导的命名关系（一个页面可以调多个权限码），所以这里不跨层拼名字。
//
// ⚠️ 与 perm / httpx 里那两个用例同因：输入在包目录之外，Go 的测试缓存追踪不到，
// 必须用 `make test` 或 `go test -count=1` 运行，否则改了前端再跑会拿到过期的 ok。
func TestMenuSeedResolvesToFrontend(t *testing.T) {
	keyRe := regexp.MustCompile(`'([^']+)'\s*:`)

	dicts := map[string]map[string]bool{}
	for _, name := range []string{"zh-CN.ts", "en-US.ts"} {
		path := filepath.Join(webDictDir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("跳过：读不到前端字典 %s（%v）。该用例需要完整的仓库布局。", path, err)
		}
		keys := map[string]bool{}
		for _, m := range keyRe.FindAllStringSubmatch(string(body), -1) {
			keys[m[1]] = true
		}
		dicts[name] = keys
	}

	db, err := NewDB(filepath.Join(t.TempDir(), "menus.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT title_key, path, component FROM sys_menus WHERE menu_type = 'menu' ORDER BY sort`)
	if err != nil {
		t.Fatalf("读 sys_menus: %v", err)
	}
	defer rows.Close()

	// 正向：每条菜单都要落到真实页面、真实文案上，且三者不重复
	byComponent := map[string]string{}
	byPath := map[string]string{}
	byKey := map[string]string{}
	menus := 0

	for rows.Next() {
		var titleKey, path, component string
		if err := rows.Scan(&titleKey, &path, &component); err != nil {
			t.Fatalf("扫描菜单行: %v", err)
		}
		menus++

		if titleKey == "" || component == "" {
			t.Errorf("菜单缺少 title_key（%q）或 component（%q）", titleKey, component)
			continue
		}
		if !strings.HasPrefix(path, "/") {
			t.Errorf("菜单 %s 的 path = %q，路由路径应以 / 开头", titleKey, path)
		}
		if _, err := os.Stat(filepath.Join(webPagesDir, component+".svelte")); err != nil {
			t.Errorf("菜单 %s 指向的页面不存在：web/src/pages/%s.svelte", titleKey, component)
		}
		for name, keys := range dicts {
			if !keys[titleKey] {
				t.Errorf("%s 缺少菜单标题键 %q", name, titleKey)
			}
		}
		if prev, ok := byComponent[component]; ok {
			t.Errorf("component %q 被两条菜单用着：%s 与 %s", component, prev, titleKey)
		}
		if prev, ok := byPath[path]; ok {
			t.Errorf("path %q 被两条菜单用着：%s 与 %s", path, prev, titleKey)
		}
		if prev, ok := byKey[titleKey]; ok {
			t.Errorf("title_key %q 重复：%s 与 %s", titleKey, prev, component)
		}
		byComponent[component] = titleKey
		byPath[path] = titleKey
		byKey[titleKey] = component
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历菜单行: %v", err)
	}
	// 菜单一条都没有时，上面的循环什么也不做，用例会假通过
	if menus == 0 {
		t.Fatal("sys_menus 里没有 menu 类型的菜单，种子数据没跑起来")
	}

	// 反向：页面文件不能有「谁都点不进去」的
	err = filepath.WalkDir(webPagesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".svelte") {
			return nil
		}
		rel, err := filepath.Rel(webPagesDir, p)
		if err != nil {
			return err
		}
		rel = strings.TrimSuffix(filepath.ToSlash(rel), ".svelte")
		if _, ok := byComponent[rel]; ok {
			return nil
		}
		if _, ok := noMenuPages[rel]; ok {
			return nil
		}
		t.Errorf("页面 web/src/pages/%s.svelte 没有被任何菜单引用（加了页面却忘了写菜单种子？）", rel)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历前端页面目录: %v", err)
	}
}
