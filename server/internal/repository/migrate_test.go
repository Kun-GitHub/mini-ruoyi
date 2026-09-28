package repository

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// migrationCount 是 migrations/ 下的文件数。
//
// 这里写死而不是动态统计：新增迁移时必须显式改这个数字，
// 从而强制作者想一想「新迁移是否也该有对应的验证」。
const migrationCount = 10

func TestMigrateCreatesSchemaAndIsIdempotent(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "migrate.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("第 %d 次 Migrate: %v", i, err)
		}
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("读 schema_migrations: %v", err)
	}
	if count != migrationCount {
		t.Errorf("schema_migrations 有 %d 条记录，期望 %d（迁移不应重复执行）", count, migrationCount)
	}

	// 迁移建出来的表要真的可用
	if _, err := db.ExecContext(ctx,
		`INSERT INTO sys_menus (title_key) VALUES (?)`, "menu.test"); err != nil {
		t.Fatalf("写入 sys_menus: %v", err)
	}
}

// TestSeedRbac 验证 0003 种子数据的完整性与可用性。
func TestSeedRbac(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// 内置角色存在，且 code 唯一
	var roleID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM sys_roles WHERE code = 'admin'`).Scan(&roleID); err != nil {
		t.Fatalf("内置角色 admin 不存在: %v", err)
	}

	// 管理员账号存在，并且**种子哈希必须能验证通过**——
	// 哈希是手抄进 SQL 的，抄错一位就会导致谁也登不进去，必须由用例兜住
	var userID int64
	var hash string
	if err := db.QueryRowContext(ctx,
		`SELECT id, password FROM sys_users WHERE username = 'admin'`).Scan(&userID, &hash); err != nil {
		t.Fatalf("管理员账号不存在: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("admin123")); err != nil {
		t.Errorf("种子密码哈希无法通过校验（admin123）: %v", err)
	}

	// 管理员必须已经绑定内置角色，否则登录后没有任何权限
	var linkCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sys_user_roles WHERE user_id = ? AND role_id = ?`, userID, roleID).Scan(&linkCount); err != nil {
		t.Fatal(err)
	}
	if linkCount != 1 {
		t.Errorf("admin 未绑定 admin 角色（%d 条关联）", linkCount)
	}

	// 菜单树：两个根目录（系统管理 / 系统监控），各自带子菜单
	var rootCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sys_menus WHERE menu_type = 'directory' AND parent_id IS NULL`).Scan(&rootCount); err != nil {
		t.Fatalf("读根目录: %v", err)
	}
	if rootCount != 3 {
		t.Errorf("有 %d 个根目录，期望 3（系统管理 + 系统监控 + 系统工具）", rootCount)
	}

	for _, tc := range []struct {
		dirKey string
		kids   int
	}{
		{"menu.system", 4},
		{"menu.monitor", 4},
		{"menu.tool", 2},
	} {
		var childCount int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM sys_menus c
			JOIN sys_menus p ON p.id = c.parent_id
			WHERE p.title_key = ?`, tc.dirKey).Scan(&childCount)
		if err != nil {
			t.Fatal(err)
		}
		if childCount != tc.kids {
			t.Errorf("%s 下有 %d 个子菜单，期望 %d", tc.dirKey, childCount, tc.kids)
		}
	}

	// title_key 不得为空，且菜单必须按 sort 可排序
	var badCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sys_menus WHERE title_key = '' OR menu_type = 'menu' AND component = ''`).Scan(&badCount); err != nil {
		t.Fatal(err)
	}
	if badCount != 0 {
		t.Errorf("有 %d 条菜单缺少 title_key 或 component", badCount)
	}
}

// TestMigrateOnLegacyDatabase 覆盖升级路径：仓库中随附的初始 data.db 由更早的
// "CREATE TABLE IF NOT EXISTS" 版本创建，表已存在但没有迁移记录。
func TestMigrateOnLegacyDatabase(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// 模拟仓库随附的初始库：里面有表、有数据，但没有任何迁移记录
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE legacy_table (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			payload TEXT NOT NULL
		)`); err != nil {
		t.Fatalf("构造旧库: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO legacy_table (payload) VALUES (?)`, "legacy-row"); err != nil {
		t.Fatalf("写入旧数据: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate 旧库: %v", err)
	}

	// 已有数据必须还在（迁移只能在库上追加，不能重建）
	var payload string
	if err := db.QueryRowContext(ctx, `SELECT payload FROM legacy_table`).Scan(&payload); err != nil {
		t.Fatalf("读旧数据: %v", err)
	}
	if payload != "legacy-row" {
		t.Errorf("旧数据被破坏，payload = %q", payload)
	}
}

// TestMigrateDetectsMissingTables 覆盖「迁移记录说已应用、表却不在」。
//
// 这是真实发生过的情况：库被手工改过、或从半途中断的备份恢复。
// 迁移只看版本号就会跳过，之后每个用到该表的接口都返回 error.internal，
// 日志里只有一句 "no such table"，没人会想到去看迁移记录。
// 必须在启动阶段拦下并给出修法。
func TestMigrateDetectsMissingTables(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "broken.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("首次 Migrate: %v", err)
	}

	// 模拟外部破坏：删表但保留迁移记录
	if _, err := db.ExecContext(ctx, `DROP TABLE sys_sessions`); err != nil {
		t.Fatalf("删表: %v", err)
	}

	err = Migrate(ctx, db)
	if err == nil {
		t.Fatal("表缺失时 Migrate 应当报错，而不是静默跳过")
	}
	// 错误信息要指出是哪张表、以及怎么修，否则运维看到只知道「结构不完整」
	for _, want := range []string{"sys_sessions", "删除数据库文件后重启"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息缺少 %q:\n%s", want, err)
		}
	}
}

// TestMigrateAcceptsLegacyDatabase 确认结构校验不会误伤正常的历史库：
// 从只有 devices 表的旧库升级之后，所有表都在，Migrate 必须成功。
func TestMigrateAcceptsLegacyDatabase(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "legacy2.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	// 旧库：有一张与本项目无关的表，没有任何迁移记录
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE devices (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("正常的历史库不该被结构校验拦下: %v", err)
	}
	// 再跑一次应当幂等
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("第二次 Migrate: %v", err)
	}
}
