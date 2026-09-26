package repository

import (
	"context"
	"path/filepath"
	"testing"
)

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
	if count != 1 {
		t.Errorf("schema_migrations 有 %d 条记录，期望 1（迁移不应重复执行）", count)
	}

	// 迁移建出来的表要真的可用
	if _, err := db.ExecContext(ctx,
		`INSERT INTO devices (name, location) VALUES (?, ?)`, "d1", "lab"); err != nil {
		t.Fatalf("写入 devices: %v", err)
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

	// 模拟旧版本留下的库：有 devices 表，没有 schema_migrations
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE devices (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL,
			location   TEXT NOT NULL,
			enabled    INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("构造旧库: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO devices (name, location) VALUES (?, ?)`, "legacy", "old"); err != nil {
		t.Fatalf("写入旧数据: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate 旧库: %v", err)
	}

	// 旧数据必须还在（迁移不能重建表）
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM devices`).Scan(&name); err != nil {
		t.Fatalf("读旧数据: %v", err)
	}
	if name != "legacy" {
		t.Errorf("旧数据被破坏，name = %q", name)
	}
}
