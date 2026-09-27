package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mini-ruoyi/internal/domain"
)

type LogRepository struct {
	db *sql.DB
}

func NewLogRepository(db *sql.DB) *LogRepository {
	return &LogRepository{db: db}
}

// InsertLoginLogs 在一个事务里批量写入登录日志。
//
// 批量的意义在 SQLite 上尤其大：它是单写者，每条日志一次事务就是一次 fsync
// 和一次写锁获取。攒成一批只付一次代价。
func (r *LogRepository) InsertLoginLogs(ctx context.Context, logs []domain.LoginLog) error {
	if len(logs) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后这里是空操作

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO sys_login_logs (username, status, reason, ip, user_agent)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, l := range logs {
		if _, err := stmt.ExecContext(ctx,
			l.Username, l.Status, l.Reason, l.IP, l.UserAgent); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *LogRepository) InsertOperLogs(ctx context.Context, logs []domain.OperLog) error {
	if len(logs) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后这里是空操作

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO sys_oper_logs (user_id, username, method, path, status, result, duration_ms, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, l := range logs {
		if _, err := stmt.ExecContext(ctx,
			l.UserID, l.Username, l.Method, l.Path, l.Status, l.Result, l.DurationMS, l.IP, l.UserAgent,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoginLogFilter 是登录日志的筛选条件。
type LoginLogFilter struct {
	Username string // 模糊
	Status   string // 精确
}

func loginLogWhere(f LoginLogFilter) (string, []any) {
	var b whereBuilder
	b.like("username", f.Username)
	b.eq("status", f.Status)
	return b.clause(), b.args
}

func (r *LogRepository) CountLoginLogs(ctx context.Context, f LoginLogFilter) (int64, error) {
	where, args := loginLogWhere(f)

	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_login_logs`+where, args...).Scan(&n)
	return n, err
}

func (r *LogRepository) ListLoginLogs(ctx context.Context, f LoginLogFilter, limit, offset int) ([]domain.LoginLog, error) {
	where, args := loginLogWhere(f)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, created_at, username, status, reason, ip, user_agent
		FROM sys_login_logs`+where+`
		ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.LoginLog, 0, limit)
	for rows.Next() {
		var l domain.LoginLog
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.Username, &l.Status, &l.Reason, &l.IP, &l.UserAgent); err != nil {
			return nil, err
		}
		list = append(list, l)
	}
	return list, rows.Err()
}

// OperLogFilter 是操作日志的筛选条件。
type OperLogFilter struct {
	Username string // 模糊
	Method   string // 精确
	Path     string // 模糊
}

func operLogWhere(f OperLogFilter) (string, []any) {
	var b whereBuilder
	b.like("username", f.Username)
	b.eq("method", f.Method)
	b.like("path", f.Path)
	return b.clause(), b.args
}

func (r *LogRepository) CountOperLogs(ctx context.Context, f OperLogFilter) (int64, error) {
	where, args := operLogWhere(f)

	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_oper_logs`+where, args...).Scan(&n)
	return n, err
}

func (r *LogRepository) ListOperLogs(ctx context.Context, f OperLogFilter, limit, offset int) ([]domain.OperLog, error) {
	where, args := operLogWhere(f)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, created_at, user_id, username, method, path, status, result, duration_ms, ip, user_agent
		FROM sys_oper_logs`+where+`
		ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.OperLog, 0, limit)
	for rows.Next() {
		var l domain.OperLog
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.UserID, &l.Username, &l.Method, &l.Path,
			&l.Status, &l.Result, &l.DurationMS, &l.IP, &l.UserAgent); err != nil {
			return nil, err
		}
		list = append(list, l)
	}
	return list, rows.Err()
}

// DeleteLogsBefore 删除两张表里早于 cutoff 的记录，返回删除条数。
//
// 一次删一批（LIMIT）而不是全删：保留期到点时会一次性积累很多行，
// 一条 DELETE 长时间持有写锁会让所有请求排队。
func (r *LogRepository) DeleteLogsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	var total int64
	for _, table := range []string{"sys_login_logs", "sys_oper_logs"} {
		res, err := r.db.ExecContext(ctx,
			`DELETE FROM `+table+` WHERE id IN (
				SELECT id FROM `+table+` WHERE created_at < ? LIMIT ?
			)`, toDBTime(cutoff), limit)
		if err != nil {
			return total, fmt.Errorf("delete from %s: %w", table, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
