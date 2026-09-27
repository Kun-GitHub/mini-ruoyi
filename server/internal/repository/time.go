package repository

import "time"

// sqliteTimeLayout 是 datetime 列的统一存储格式。
//
// 必须与 SQLite 的 CURRENT_TIMESTAMP 输出完全一致（UTC，秒精度，空格分隔）：
//
//	SELECT CURRENT_TIMESTAMP;  -- 2026-09-27 08:44:22
//
// 为什么不能直接把 time.Time 交给驱动：驱动写的是 t.String()，形如
//
//	2026-10-04 16:44:22.094239 +0800 CST m=+604802.023486084
//
// 带本地时区，还把单调时钟读数漏了进去。后果不只是难看：
//
//  1. 同一列里混了两种格式（DEFAULT CURRENT_TIMESTAMP 写 UTC，Go 绑定写本地时间），
//     而 SQLite 对 TEXT 的比较是逐字节的——按时间过滤的语句会静默出错
//  2. 时区信息让"哪个更早"取决于服务器时区，换台机器部署行为就变了
//  3. `m=+...` 是进程内的单调时钟，没有任何持久化意义
//
// 统一用固定宽度、同一时区、秒精度的字符串后，字典序即时间序，
// SQL 侧的 CURRENT_TIMESTAMP / datetime('now', ...) 与 Go 绑定可以混用。
const sqliteTimeLayout = "2006-01-02 15:04:05"

// toDBTime 把 Go 时间转成 datetime 列的统一存储格式。
func toDBTime(t time.Time) string {
	return t.UTC().Format(sqliteTimeLayout)
}
