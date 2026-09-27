package repository

import (
	"context"
	"path/filepath"
	"testing"

	"mini-ruoyi/internal/domain"
)

func newUserRepo(t *testing.T) (*UserRepository, context.Context) {
	t.Helper()

	db, err := NewDB(filepath.Join(t.TempDir(), "filter.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewUserRepository(db), ctx
}

func TestLikePatternEscapesWildcards(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc", `%abc%`},
		// 反斜杠必须最先替换，否则后插入的转义符会被二次转义
		{"a%c", `%a\%c%`},
		{"a_c", `%a\_c%`},
		{`a\b`, `%a\\b%`},
		// 混合：% → \%、_ → \_、\ → \\、% → \%，最后再包一层 %
		{`%_\%`, `%\%\_\\\%%`},
		{"张三", `%张三%`},
	}
	for _, tc := range cases {
		if got := likePattern(tc.in); got != tc.want {
			t.Errorf("likePattern(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

// TestFilterDoesNotTreatUserInputAsWildcard 是这一组里最重要的用例。
//
// 不转义通配符的话，用户输入一个 `%` 就会拿到全部记录；
// 输入 `_` 会把任意单字符当成匹配位。不是注入，但结果是用户在筛选、
// 实际看到的是全量数据，很难意识到。
func TestFilterDoesNotTreatUserInputAsWildcard(t *testing.T) {
	repo, ctx := newUserRepo(t)

	// 注意避开种子里的 admin，否则会撞唯一约束
	for _, name := range []string{"alice", "bob", "carol"} {
		if _, err := repo.Create(ctx, domain.User{
			Status: domain.StatusActive, Username: name, Password: "x",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 种子数据里的 admin 已经存在，所以这里共 4 个用户
	all, err := repo.Count(ctx, UserFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if all != 4 {
		t.Fatalf("无筛选时共 %d 个用户，期望 4", all)
	}

	for _, wildcard := range []string{"%", "_", "%%", "__"} {
		n, err := repo.Count(ctx, UserFilter{Username: wildcard})
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("按 %q 筛选得到 %d 条，期望 0（用户输入不该被当成通配符）", wildcard, n)
		}
	}
}

func TestUserFilterMatchesExpectedRows(t *testing.T) {
	repo, ctx := newUserRepo(t)

	mk := func(username, nickname, mobile, status string) {
		t.Helper()
		if _, err := repo.Create(ctx, domain.User{
			Username: username, Nickname: nickname, Mobile: mobile,
			Password: "x", Status: status,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("zhangsan", "张三", "13800138000", domain.StatusActive)
	mk("lisi", "李四", "13900139000", domain.StatusInactive)
	mk("wangwu", "王五", "13800138001", domain.StatusActive)

	cases := []struct {
		name   string
		filter UserFilter
		want   int64
	}{
		{"按用户名模糊", UserFilter{Username: "zhang"}, 1},
		{"按昵称模糊", UserFilter{Nickname: "李"}, 1},
		{"按手机号前缀", UserFilter{Mobile: "138"}, 2},
		{"按状态", UserFilter{Status: domain.StatusActive}, 3}, // admin + zhangsan + wangwu
		{"状态 + 模糊组合", UserFilter{Status: domain.StatusActive, Mobile: "13800138000"}, 1},
		{"无匹配", UserFilter{Username: "nobody"}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := repo.Count(ctx, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if n != tc.want {
				t.Errorf("Count = %d，期望 %d", n, tc.want)
			}

			// List 与 Count 必须用同一套条件：不一致会让「共 N 条」和实际行数对不上，
			// 而且这种错很难被注意到
			list, err := repo.List(ctx, tc.filter, 100, 0)
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(list)) != tc.want {
				t.Errorf("List 返回 %d 条，Count 说 %d 条，两者条件不一致", len(list), tc.want)
			}
		})
	}
}

func TestRoleFilter(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "rolefilter.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := NewRoleRepository(db)

	// 种子里有 admin 角色
	n, err := repo.Count(ctx, RoleFilter{Code: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("按 code=admin 筛选得到 %d 条，期望 1", n)
	}

	n, err = repo.Count(ctx, RoleFilter{Name: "超级"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("按名称模糊筛选得到 %d 条，期望 1", n)
	}

	// 通配符同样必须被转义
	n, err = repo.Count(ctx, RoleFilter{Code: "%"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("按 %% 筛选得到 %d 条，期望 0", n)
	}
}
