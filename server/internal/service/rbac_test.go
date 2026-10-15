// 有问题欢迎联系 hot_kun@hotmail.com
package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/repository"
)

type rbacFixture struct {
	menuSvc *MenuService
	roleSvc *RoleService
	userSvc *UserService
	ctx     context.Context
}

func newRBACFixture(t *testing.T) *rbacFixture {
	t.Helper()

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "rbac.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	userRepo := repository.NewUserRepository(db)
	menuRepo := repository.NewMenuRepository(db)
	sessionSvc := auth.NewSessionService(repository.NewSessionRepository(db), userRepo)

	return &rbacFixture{
		menuSvc: NewMenuService(menuRepo),
		roleSvc: NewRoleService(repository.NewRoleRepository(db)),
		userSvc: NewUserService(userRepo, sessionSvc),
		ctx:     ctx,
	}
}

// impactOf 断言错误是带影响面的 *domain.DependentsError，并返回影响面。
func impactOf(t *testing.T, err error) map[string]int64 {
	t.Helper()
	if err == nil {
		t.Fatal("期望返回 DependentsError，实际为 nil")
	}
	if !errors.Is(err, domain.ErrHasDependents) {
		t.Fatalf("错误不是 ErrHasDependents: %v", err)
	}
	var dep *domain.DependentsError
	if !errors.As(err, &dep) {
		t.Fatalf("无法从错误里取出影响面: %v", err)
	}
	return dep.Impact
}

// ---------- 菜单 ----------

// 种子数据：一个「系统管理」目录，下挂 3 个子菜单。
func TestMenuTreeFromSeed(t *testing.T) {
	f := newRBACFixture(t)

	tree, err := f.menuSvc.Tree(f.ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(tree) != 3 {
		t.Fatalf("根节点有 %d 个，期望 3（系统管理 + 系统监控 + 系统工具）", len(tree))
	}
	if len(tree[0].Children) != 4 {
		t.Errorf("系统管理下有 %d 个子菜单，期望 4", len(tree[0].Children))
	}
	// children 必须是非 nil 切片，否则前端要特判 null
	if tree[0].Children[0].Children == nil {
		t.Error("叶子节点的 Children 是 nil，期望空切片")
	}
}

// TestMenuDeleteReportsDescendants 是「删除有子数据要确认」的核心用例。
func TestMenuDeleteReportsDescendants(t *testing.T) {
	f := newRBACFixture(t)

	dir, err := f.menuSvc.Tree(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	dirID := dir[0].ID

	// 不带 cascade：必须被拦下，并告知有 4 个子菜单
	err = f.menuSvc.Delete(f.ctx, dirID, false)
	impact := impactOf(t, err)
	if impact["child_menus"] != 4 {
		t.Errorf("child_menus = %d，期望 4", impact["child_menus"])
	}

	// 被拦下之后目录必须还在
	if _, err := f.menuSvc.Get(f.ctx, dirID); err != nil {
		t.Fatalf("被拦下后目录不该消失: %v", err)
	}

	// 带 cascade：整棵子树消失
	if err := f.menuSvc.Delete(f.ctx, dirID, true); err != nil {
		t.Fatalf("cascade 删除失败: %v", err)
	}
	tree, err := f.menuSvc.Tree(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 「系统管理」整棵子树消失，另外两个目录是独立的根，不该受影响
	if len(tree) != 2 {
		t.Fatalf("删目录后还剩 %d 个根菜单，期望 2（只应删掉被删目录的子树）", len(tree))
	}
	if tree[0].TitleKey != "menu.monitor" || tree[1].TitleKey != "menu.tool" {
		t.Errorf("剩下的根菜单是 %q/%q，期望 menu.monitor/menu.tool", tree[0].TitleKey, tree[1].TitleKey)
	}
}

// TestMenuImpactCountsRolesOfDescendants 覆盖「影响面必须算上后代菜单的授权」。
//
// 只数菜单自身的直接引用会低报影响面，用户就会在不知情下少掉一批授权。
func TestMenuImpactCountsRolesOfDescendants(t *testing.T) {
	f := newRBACFixture(t)

	tree, err := f.menuSvc.Tree(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	dirID, childID := tree[0].ID, tree[0].Children[0].ID

	// 验证：单独删子菜单时，影响面里不该出现那个目录
	childImpact, err := f.menuSvc.Impact(f.ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	if childImpact["child_menus"] != 0 {
		t.Errorf("子菜单的 child_menus = %d，期望 0", childImpact["child_menus"])
	}

	// 删目录时必须把 3 个后代都算进去
	dirImpact, err := f.menuSvc.Impact(f.ctx, dirID)
	if err != nil {
		t.Fatal(err)
	}
	if dirImpact["child_menus"] != 4 {
		t.Errorf("目录的 child_menus = %d，期望 4", dirImpact["child_menus"])
	}
}

func TestMenuLeafDeleteNeedsNoConfirm(t *testing.T) {
	f := newRBACFixture(t)

	tree, _ := f.menuSvc.Tree(f.ctx)
	leafID := tree[0].Children[0].ID

	// 叶子节点没有依赖，不该要求确认
	if err := f.menuSvc.Delete(f.ctx, leafID, false); err != nil {
		t.Fatalf("删除叶子菜单应直接成功，实际: %v", err)
	}
}

// TestMenuRejectsCyclicParent 覆盖「把菜单挂到自己的后代下面」——
// 会形成环，让整棵子树从界面上消失且无法在界面上恢复。
func TestMenuRejectsCyclicParent(t *testing.T) {
	f := newRBACFixture(t)

	tree, _ := f.menuSvc.Tree(f.ctx)
	dir := tree[0]
	child := dir.Children[0]

	// 把目录的父节点设成自己的子菜单 → 环
	bad := dir.Menu
	bad.ParentID = &child.ID
	if err := f.menuSvc.Update(f.ctx, bad); !errors.Is(err, domain.ErrInvalidParent) {
		t.Errorf("环引用应返回 ErrInvalidParent，实际: %v", err)
	}

	// 父节点设成自己
	self := dir.Menu
	self.ParentID = &dir.ID
	if err := f.menuSvc.Update(f.ctx, self); !errors.Is(err, domain.ErrInvalidParent) {
		t.Errorf("自引用应返回 ErrInvalidParent，实际: %v", err)
	}
}

// TestMenuParentMustBeDirectory 覆盖「菜单不能挂在菜单下面」。
func TestMenuParentMustBeDirectory(t *testing.T) {
	f := newRBACFixture(t)

	tree, _ := f.menuSvc.Tree(f.ctx)
	childID := tree[0].Children[0].ID

	_, err := f.menuSvc.Create(f.ctx, domain.Menu{
		Status:   domain.StatusActive,
		ParentID: &childID,
		MenuType: domain.MenuTypeMenu,
		TitleKey: "menu.system.new",
	})
	if !errors.Is(err, domain.ErrInvalidParent) {
		t.Errorf("挂在菜单下应返回 ErrInvalidParent，实际: %v", err)
	}
}

// ---------- 角色 ----------

func TestRoleBuiltinCannotBeDeleted(t *testing.T) {
	f := newRBACFixture(t)

	admin, err := f.roleSvc.repo.GetByCode(f.ctx, domain.RoleCodeAdmin)
	if err != nil {
		t.Fatal(err)
	}

	// 即使带 cascade 也必须拒绝
	if err := f.roleSvc.Delete(f.ctx, admin.ID, true); !errors.Is(err, domain.ErrProtected) {
		t.Errorf("删除内置角色应返回 ErrProtected，实际: %v", err)
	}
}

func TestRoleBuiltinCannotBeDisabled(t *testing.T) {
	f := newRBACFixture(t)

	admin, err := f.roleSvc.repo.GetByCode(f.ctx, domain.RoleCodeAdmin)
	if err != nil {
		t.Fatal(err)
	}
	admin.Status = domain.StatusInactive

	if err := f.roleSvc.Update(f.ctx, admin); !errors.Is(err, domain.ErrProtected) {
		t.Errorf("停用内置角色应返回 ErrProtected，实际: %v", err)
	}
}

func TestRoleDeleteReportsAffectedUsers(t *testing.T) {
	f := newRBACFixture(t)

	// 建一个普通角色并绑给一个普通用户。
	//
	// 不能拿内置 admin 来当这个「被影响的用户」：重绑会把它的 admin 角色换掉，
	// 那等同于弄丢唯一的管理员，会被最后管理员守卫拦下——
	// 那时的失败与影响面无关，测不出这里想测的东西。
	role, err := f.roleSvc.Create(f.ctx, domain.Role{
		Status: domain.StatusActive, Code: "auditor", Name: "审计员",
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.userSvc.Create(f.ctx, CreateUserInput{
		Username: "auditee", Password: "auditee-pw", Nickname: "被审计的人",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.userSvc.SetRoles(f.ctx, user.ID, []int64{role.ID}); err != nil {
		t.Fatal(err)
	}

	err = f.roleSvc.Delete(f.ctx, role.ID, false)
	impact := impactOf(t, err)
	if impact["affected_users"] != 1 {
		t.Errorf("affected_users = %d，期望 1", impact["affected_users"])
	}

	if err := f.roleSvc.Delete(f.ctx, role.ID, true); err != nil {
		t.Fatalf("cascade 删除失败: %v", err)
	}
}

func TestRoleRejectsCreatingBuiltinCode(t *testing.T) {
	f := newRBACFixture(t)

	_, err := f.roleSvc.Create(f.ctx, domain.Role{
		Status: domain.StatusActive, Code: domain.RoleCodeAdmin, Name: "伪造管理员",
	})
	if !errors.Is(err, domain.ErrProtected) {
		t.Errorf("自建 admin 角色应被拒绝，实际: %v", err)
	}
}

// ---------- 用户 ----------

func TestUserCannotDeleteSelf(t *testing.T) {
	f := newRBACFixture(t)

	admin, _ := f.userSvc.repo.GetByUsername(f.ctx, "admin")
	if err := f.userSvc.Delete(f.ctx, admin.ID, admin.ID); !errors.Is(err, domain.ErrCannotDeleteSelf) {
		t.Errorf("删自己应返回 ErrCannotDeleteSelf，实际: %v", err)
	}
}

// TestUserCannotDeleteLastAdmin 覆盖「删掉最后一个管理员后没人能进后台」。
func TestUserCannotDeleteLastAdmin(t *testing.T) {
	f := newRBACFixture(t)

	admin, _ := f.userSvc.repo.GetByUsername(f.ctx, "admin")
	// 让「另一个用户」来删，绕过删自己的守卫，此时应被最后管理员守卫拦下
	if err := f.userSvc.Delete(f.ctx, admin.ID, 9999); !errors.Is(err, domain.ErrLastAdmin) {
		t.Errorf("删最后一个管理员应返回 ErrLastAdmin，实际: %v", err)
	}
}

// TestUserCannotDisableLastAdmin 覆盖停用路径——被停用的管理员登不进来，
// 效果等同于删除，同样会把系统锁死。
func TestUserCannotDisableLastAdmin(t *testing.T) {
	f := newRBACFixture(t)

	admin, _ := f.userSvc.repo.GetByUsername(f.ctx, "admin")
	admin.Status = domain.StatusInactive

	if err := f.userSvc.Update(f.ctx, admin); !errors.Is(err, domain.ErrLastAdmin) {
		t.Errorf("停用最后一个管理员应返回 ErrLastAdmin，实际: %v", err)
	}
}

// TestUserDeleteWithAnotherAdminPresent 确认守卫不是「一刀切禁止」：
// 还有一个管理员时，删除是允许的。
func TestUserDeleteWithAnotherAdminPresent(t *testing.T) {
	f := newRBACFixture(t)

	adminRole, _ := f.roleSvc.repo.GetByCode(f.ctx, domain.RoleCodeAdmin)

	second, err := f.userSvc.repo.Create(f.ctx, domain.User{
		Status: domain.StatusActive, Username: "admin2", Password: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.userSvc.SetRoles(f.ctx, second.ID, []int64{adminRole.ID}); err != nil {
		t.Fatal(err)
	}

	first, _ := f.userSvc.repo.GetByUsername(f.ctx, "admin")
	// 现在有两个管理员，删掉第一个应当成功
	if err := f.userSvc.Delete(f.ctx, first.ID, second.ID); err != nil {
		t.Errorf("还有其他管理员时删除应成功，实际: %v", err)
	}
}

// TestUserDeleteNormalUserNotBlocked 确认普通用户不受最后管理员守卫影响——
// 否则删一个普通用户却报「最后一个管理员」会让人完全摸不着头脑。
func TestUserDeleteNormalUserNotBlocked(t *testing.T) {
	f := newRBACFixture(t)

	normal, err := f.userSvc.repo.Create(f.ctx, domain.User{
		Status: domain.StatusActive, Username: "operator", Password: "x",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.userSvc.Delete(f.ctx, normal.ID, 9999); err != nil {
		t.Errorf("删除普通用户应成功，实际: %v", err)
	}
}

// TestPaginationBoundaries 覆盖分页的两个边界行为。
//
// 它们在界面上表现为「不可能的状态」：
//   - page_size 超上限时悄悄换成默认值 → 要 101 条却拿到 20 条，
//     调用方会以为数据只有这么多
//   - page 越界时不钳位 → 响应里的 page 还是 99，前端分页器显示 "99 / 2"，
//     点「上一页」仍是空白
func TestPaginationBoundaries(t *testing.T) {
	f := newRBACFixture(t)

	// 造 25 个用户，加上种子里的 admin 共 26 个
	for i := 0; i < 25; i++ {
		if _, err := f.userSvc.Create(f.ctx, CreateUserInput{
			Username: fmt.Sprintf("pg%02d", i), Password: "password-x",
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("page_size 超上限钳到上限", func(t *testing.T) {
		got, err := f.userSvc.List(f.ctx, repository.UserFilter{}, 1, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if got.PageSize != 100 {
			t.Errorf("page_size = %d，期望钳到 100（而不是悄悄换成默认值 20）", got.PageSize)
		}
		if len(got.List) != 26 {
			t.Errorf("返回 %d 条，期望 26（总共就这么多）", len(got.List))
		}
	})

	t.Run("page 越界钳到最后一页", func(t *testing.T) {
		got, err := f.userSvc.List(f.ctx, repository.UserFilter{}, 99, 20)
		if err != nil {
			t.Fatal(err)
		}
		// 26 条 / 每页 20 → 最后一页是第 2 页
		if got.Page != 2 {
			t.Errorf("page = %d，期望钳到 2（否则前端显示 99 / 2）", got.Page)
		}
		if len(got.List) != 6 {
			t.Errorf("最后一页返回 %d 条，期望 6", len(got.List))
		}
	})

	t.Run("空结果页码归一", func(t *testing.T) {
		got, err := f.userSvc.List(f.ctx, repository.UserFilter{Username: "不存在的人"}, 99, 20)
		if err != nil {
			t.Fatal(err)
		}
		if got.Total != 0 || got.Page != 1 {
			t.Errorf("total=%d page=%d，期望 0 和 1", got.Total, got.Page)
		}
		if got.List == nil {
			t.Error("空结果的 list 必须是 [] 而不是 nil")
		}
	})
}

// ---------- 停用角色的实际后果 ----------

// TestDisabledRoleStopsGrantingPermissions 覆盖「停用角色只是改了个字段」这个缺陷。
//
// 角色列表上显示「停用」,但菜单/权限查询若不过滤 sys_roles.status,
// 该角色带来的全部能力仍然放行——停用就成了一个只影响界面的开关。
func TestDisabledRoleStopsGrantingPermissions(t *testing.T) {
	f := newRBACFixture(t)

	role, err := f.roleSvc.Create(f.ctx, domain.Role{
		Status: domain.StatusActive, Code: "auditor2", Name: "审计员",
	})
	if err != nil {
		t.Fatal(err)
	}

	menus, err := f.menuSvc.repo.List(f.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(menus) == 0 {
		t.Fatal("种子菜单为空,测试没法进行")
	}
	if err := f.roleSvc.SetGrants(f.ctx, role.ID, Grants{
		MenuIDs: []int64{menus[0].ID},
	}); err != nil {
		t.Fatal(err)
	}

	user, err := f.userSvc.Create(f.ctx, CreateUserInput{
		Username: "auditee2", Password: "auditee-pw", RoleIDs: []int64{role.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	before, err := f.menuSvc.TreeForIdentity(f.ctx, Identity{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("停用前应能看到菜单,否则这个测试证明不了任何东西")
	}

	// 停用角色
	role.Status = domain.StatusInactive
	if err := f.roleSvc.Update(f.ctx, role); err != nil {
		t.Fatal(err)
	}

	after, err := f.menuSvc.TreeForIdentity(f.ctx, Identity{User: user})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Errorf("角色已停用,菜单仍有 %d 项,期望 0", len(after))
	}
}

// TestRebindRejectsRemovingLastAdmin 覆盖「角色重绑绕过最后管理员守卫」这个缺陷。
//
// 删除和停用都被 guardLastAdmin 拦住了,但 SetRoles 不经过那条路:
// 把唯一管理员的 admin 角色换成别的(或换成空列表),系统同样再没人能进后台,
// 而且没有任何界面能把它恢复回来。
func TestRebindRejectsRemovingLastAdmin(t *testing.T) {
	f := newRBACFixture(t)

	admin, _ := f.userSvc.repo.GetByUsername(f.ctx, "admin")

	t.Run("换成其它角色被拒", func(t *testing.T) {
		other, err := f.roleSvc.Create(f.ctx, domain.Role{
			Status: domain.StatusActive, Code: "viewer", Name: "只读",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.userSvc.SetRoles(f.ctx, admin.ID, []int64{other.ID}); !errors.Is(err, domain.ErrLastAdmin) {
			t.Errorf("夺走最后一个管理员的 admin 角色应返回 ErrLastAdmin,实际: %v", err)
		}
	})

	t.Run("清空角色被拒", func(t *testing.T) {
		if err := f.userSvc.SetRoles(f.ctx, admin.ID, nil); !errors.Is(err, domain.ErrLastAdmin) {
			t.Errorf("清空最后一个管理员的角色应返回 ErrLastAdmin,实际: %v", err)
		}
	})

	t.Run("保留 admin 角色则放行", func(t *testing.T) {
		adminRole, _ := f.roleSvc.repo.GetByCode(f.ctx, domain.RoleCodeAdmin)
		if err := f.userSvc.SetRoles(f.ctx, admin.ID, []int64{adminRole.ID}); err != nil {
			t.Errorf("仍然保留 admin 角色时应允许,实际: %v", err)
		}
	})

	t.Run("还有别的管理员时允许卸任", func(t *testing.T) {
		adminRole, _ := f.roleSvc.repo.GetByCode(f.ctx, domain.RoleCodeAdmin)
		second, err := f.userSvc.Create(f.ctx, CreateUserInput{
			Username: "admin3", Password: "admin3-pw", RoleIDs: []int64{adminRole.ID},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.userSvc.SetRoles(f.ctx, second.ID, nil); err != nil {
			t.Errorf("还有另一个管理员时应允许卸任,实际: %v", err)
		}
	})

	// 并发子用例：两个管理员同时把自己摘成非管理员。
	//
	// 如果守卫是「先查管理员人数、再写绑定」两步走，两边都会先看到
	// 「管理员有 2 个」，各自放行，结果一个管理员都不剩。
	// 守卫放进 ReplaceRoles 的事务后，SQLite 的单写者模型保证后提交的
	// 那个看到的是前一个提交后的状态，于是至少留一个人。
	t.Run("并发卸任至少留下一名管理员", func(t *testing.T) {
		adminRole, err := f.roleSvc.repo.GetByCode(f.ctx, domain.RoleCodeAdmin)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := f.roleSvc.Create(f.ctx, domain.Role{
			Status: domain.StatusActive, Code: "plain", Name: "普通",
		})
		if err != nil {
			t.Fatal(err)
		}

		second, err := f.userSvc.Create(f.ctx, CreateUserInput{
			Username: "admin4", Password: "admin4-pw", RoleIDs: []int64{adminRole.ID},
		})
		if err != nil {
			t.Fatal(err)
		}
		first, err := f.userSvc.repo.GetByUsername(f.ctx, "admin")
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		for _, id := range []int64{first.ID, second.ID} {
			wg.Add(1)
			go func(userID int64) {
				defer wg.Done()
				// 错误无所谓：被守卫拦住和成功提交都是可以接受的结果，
				// 不能接受的是两个人都摘掉之后没人管得动系统。
				_ = f.userSvc.SetRoles(f.ctx, userID, []int64{plain.ID})
			}(id)
		}
		wg.Wait()

		admins, err := f.userSvc.repo.CountOtherAdmins(f.ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		if admins < 1 {
			t.Errorf("并发卸任后管理员为 %d 个,应该至少留 1 个", admins)
		}
	})
}

// TestCreateUserBindsRolesAtomically 覆盖「建用户与绑角色是两件事」这个缺陷。
//
// 界面上一张表单同时填了账号和角色,后端就必须一次做完:
// 若是两次请求,第二次失败会留下一个没有任何角色的用户,
// 用户以为建好了,登进去什么都看不见。
func TestCreateUserBindsRolesAtomically(t *testing.T) {
	f := newRBACFixture(t)

	role, err := f.roleSvc.Create(f.ctx, domain.Role{
		Status: domain.StatusActive, Code: "operator2", Name: "操作员",
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := f.userSvc.Create(f.ctx, CreateUserInput{
		Username: "withroles", Password: "withroles-pw", RoleIDs: []int64{role.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	detail, err := f.userSvc.GetDetail(f.ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.RoleIDs) != 1 || detail.RoleIDs[0] != role.ID {
		t.Errorf("角色应随创建一起绑定,实际 RoleIDs = %v", detail.RoleIDs)
	}

	t.Run("角色不存在时不留半成品", func(t *testing.T) {
		_, err := f.userSvc.Create(f.ctx, CreateUserInput{
			Username: "partial", Password: "partial-pw", RoleIDs: []int64{999999},
		})
		if err == nil {
			t.Fatal("绑定不存在的角色应当报错")
		}
		// 事务整体回滚,这个用户名不该被占住
		exists, existsErr := f.userSvc.repo.ExistsUsername(f.ctx, "partial", 0)
		if existsErr != nil {
			t.Fatal(existsErr)
		}
		if exists {
			t.Error("绑定失败后用户不应残留(事务应整体回滚)")
		}
	})
}
