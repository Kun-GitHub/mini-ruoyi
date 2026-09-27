package repository

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate 按文件名数字前缀的顺序执行 migrations/ 下的 SQL，已执行的版本记录在
// schema_migrations 表里。
//
// 只做向前补齐，不提供回滚：SQLite 的 DDL 回滚脚本很容易和线上数据现状不一致，
// 收益不值这个复杂度。改错了一个迁移，正确的做法是再加一个修复迁移。
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names) // 文件名用零填充数字前缀，字典序即版本序

	var bodies []string
	for _, name := range names {
		base := path.Base(name)
		version, err := parseMigrationVersion(base)
		if err != nil {
			return err
		}

		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		bodies = append(bodies, string(body))

		if applied[version] {
			continue
		}
		if err := applyMigration(ctx, db, version, base, string(body)); err != nil {
			return err
		}
	}

	// 结构校验放在最后：只信 schema_migrations 是不够的，见 verifySchema 的说明
	return verifySchema(ctx, db, expectedTables(bodies))
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[int64]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[int64]bool{}
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// parseMigrationVersion 解析 "<版本号>_<描述>.sql" 里的版本号。
func parseMigrationVersion(filename string) (int64, error) {
	prefix, _, ok := strings.Cut(filename, "_")
	if !ok {
		return 0, fmt.Errorf("迁移文件 %q 不符合 <版本号>_<描述>.sql 命名", filename)
	}
	v, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("迁移文件 %q 的版本号不是数字: %w", filename, err)
	}
	return v, nil
}

// applyMigration 在单个事务里执行迁移体和版本记录，两者要么都生效要么都不生效。
// SQLite 的 DDL 是事务性的，所以 DDL 失败也不会留下半截结构。
func applyMigration(ctx context.Context, db *sql.DB, version int64, name, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后这里是空操作

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, version, name,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return tx.Commit()
}

var createTableRe = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)`)

// expectedTables 从所有迁移文件里提取应该存在的表名。
func expectedTables(bodies []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, body := range bodies {
		for _, m := range createTableRe.FindAllStringSubmatch(body, -1) {
			name := strings.ToLower(m[1])
			if name == "schema_migrations" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// verifySchema 确认迁移文件声明过的表都真实存在，缺失则报错。
//
// 为什么必须在启动时查：schema_migrations 只说明「迁移跑过」，不说明「表还在」。
// 库被外部改过的情形都真实存在——手工删了表、从半途中断的备份恢复、
// 拷来一个不完整的文件。这时版本号说「已应用」，迁移就不再执行，
// 而之后**每个**用到该表的接口都会返回 error.internal，
// 日志里只有一句 "no such table: xxx"，没人会想到去看迁移记录。
//
// 直接拒绝启动并给出修法，比让它在运行期零散地 500 好得多。
func verifySchema(ctx context.Context, db *sql.DB, want []string) error {
	var missing []string
	for _, table := range want {
		var exists bool
		err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`,
			table).Scan(&exists)
		if err != nil {
			return fmt.Errorf("检查表 %s 是否存在: %w", table, err)
		}
		if !exists {
			missing = append(missing, table)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf(
		"数据库结构不完整：迁移记录显示已应用，但以下表不存在：%s\n"+
			"  可能原因：库被手工改过、从半途中断的备份恢复、或拷贝了不完整的文件。\n"+
			"  修复方式：\n"+
			"    - 全新部署（没有要保留的数据）：删除数据库文件后重启，会自动重建\n"+
			"    - 已有数据：从备份恢复，或手工补齐缺失的表",
		strings.Join(missing, ", "))
}
