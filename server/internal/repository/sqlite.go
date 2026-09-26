package repository

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // 纯 Go 实现，免 CGO，交叉编译方便
)

// sqlitePragmas 是「连接级」PRAGMA 的 DSN 下发形式。
//
// busy_timeout / foreign_keys / synchronous 都是每条连接各自的设置：用 db.Exec 执行
// PRAGMA 只会命中连接池里的某一条连接，其余连接仍是默认值（实测 busy_timeout=0、
// foreign_keys=0）。所以必须走 DSN 的 _pragma，由驱动在每条连接建立时执行。
// journal_mode=WAL 本身是写入库文件的持久设置，一并放这里以集中行为。
var sqlitePragmas = func() string {
	v := url.Values{}
	v.Add("_pragma", "busy_timeout(5000)")  // 写冲突时等待而不是立刻报 SQLITE_BUSY
	v.Add("_pragma", "journal_mode(WAL)")   // 读写不互相阻塞
	v.Add("_pragma", "foreign_keys(1)")     // SQLite 默认关闭外键约束，必须显式打开
	v.Add("_pragma", "synchronous(NORMAL)") // WAL 下 NORMAL 已足够安全，比 FULL 快很多
	return v.Encode()
}()

// NewDB 打开 SQLite 连接，并按低配服务器场景配置连接池。
func NewDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?"+sqlitePragmas)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite 是单写者模型，写操作本身串行；多出的连接只服务并发读
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	// 提前 Ping，让配置错误在启动时就暴露，而不是等到第一个请求
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}
