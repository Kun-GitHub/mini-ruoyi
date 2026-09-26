package repository

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // 纯 Go 实现，免 CGO，交叉编译方便
)

// NewDB 打开 SQLite 连接并做低配服务器友好的关键配置：
//   - WAL 模式：读写不互相阻塞
//   - busy_timeout：写冲突时等待而不是立刻报错
//   - 连接池限制为 1（SQLite 本质单写者，多余连接只会增加锁竞争和内存）
func NewDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=NORMAL;", // WAL 下 NORMAL 已足够安全，比 FULL 快很多
		"PRAGMA foreign_keys=ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	// SQLite 是单写者模型，连接数开多了没意义，反而增加内存和锁等待
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS devices (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL,
		location   TEXT NOT NULL,
		enabled    INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_devices_location ON devices(location);
	`
	_, err := db.Exec(schema)
	return err
}
