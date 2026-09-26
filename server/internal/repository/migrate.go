package repository

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
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

	for _, name := range names {
		base := path.Base(name)
		version, err := parseMigrationVersion(base)
		if err != nil {
			return err
		}
		if applied[version] {
			continue
		}
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, db, version, base, string(body)); err != nil {
			return err
		}
	}
	return nil
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
