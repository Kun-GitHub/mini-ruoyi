package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestNewDBConfiguresPragmasOnEveryConn 是 A1 的回归用例。
//
// 修复前：用 db.Exec 执行 PRAGMA，只有连接池里恰好被命中的那条连接生效，
// 实测 4 条连接里 3 条 foreign_keys=0、busy_timeout=0。
func TestNewDBConfiguresPragmasOnEveryConn(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "pragma.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	// 同时占住 4 条连接，确保检查到的是池里的每一条，而不是复用同一条
	conns := make([]*sql.Conn, 0, 4)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < 4; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("get conn %d: %v", i, err)
		}
		conns = append(conns, c)
	}

	for i, c := range conns {
		var foreignKeys, busyTimeout, synchronous int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("conn %d foreign_keys: %v", i, err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("conn %d busy_timeout: %v", i, err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
			t.Fatalf("conn %d synchronous: %v", i, err)
		}
		// NORMAL 对应的值就是 1
		if foreignKeys != 1 || busyTimeout != 5000 || synchronous != 1 {
			t.Errorf("conn %d: foreign_keys=%d busy_timeout=%d synchronous=%d，期望 1/5000/1",
				i, foreignKeys, busyTimeout, synchronous)
		}
	}
}

func TestNewDBEnablesWAL(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "wal.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q，期望 wal", mode)
	}
}
