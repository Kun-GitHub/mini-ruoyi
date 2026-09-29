package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"mini-ruoyi/internal/domain"
)

type JobRepository struct {
	db *sql.DB
}

func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

const jobColumns = `id, created_at, updated_at, status, job_key, cron, remark,
	last_run_at, last_status, last_error, last_duration_ms`

func scanJob(s interface{ Scan(...any) error }) (domain.Job, error) {
	var j domain.Job
	err := s.Scan(&j.ID, &j.CreatedAt, &j.UpdatedAt, &j.Status, &j.Key, &j.Cron, &j.Remark,
		&j.LastRunAt, &j.LastStatus, &j.LastError, &j.LastDurationMS)
	return j, err
}

// Ensure 为代码里注册的任务补上数据库记录。
//
// 用 DO NOTHING 而不是 DO UPDATE：数据库是「用户改过的参数」的存储，
// 启动时不能把用户改过的 cron 表达式或开关覆盖回默认值。
func (r *JobRepository) Ensure(ctx context.Context, key, defaultCron string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sys_jobs (job_key, cron) VALUES (?, ?)
		ON CONFLICT(job_key) DO NOTHING`, key, defaultCron)
	return err
}

func (r *JobRepository) List(ctx context.Context) ([]domain.Job, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM sys_jobs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.Job, 0)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

func (r *JobRepository) GetByKey(ctx context.Context, key string) (domain.Job, error) {
	j, err := scanJob(r.db.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM sys_jobs WHERE job_key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, domain.ErrNotFound
	}
	return j, err
}

// Keys 返回库里现存的任务 key，供启动时比对注册表。
func (r *JobRepository) Keys(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT job_key FROM sys_jobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Update 只改可调参数，不碰执行结果。
func (r *JobRepository) Update(ctx context.Context, key, cron, status, remark string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE sys_jobs SET cron = ?, status = ?, remark = ?, updated_at = CURRENT_TIMESTAMP
		WHERE job_key = ?`, cron, status, remark, key)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// RecordRun 记录一次执行：更新 sys_jobs 上的「最近一次」，并追加一条历史。
//
// 两条写在同一个事务里。分开写的话，进程恰好停在两次写中间，就会留下
// 「列表说上一次成功了、历史里查不到」这种自相矛盾的两份现实。
func (r *JobRepository) RecordRun(ctx context.Context, key, trigger, status, errMsg string, ranAt time.Time, durationMS int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后这里是空操作

	if _, err := tx.ExecContext(ctx, `
		UPDATE sys_jobs
		SET last_run_at = ?, last_status = ?, last_error = ?, last_duration_ms = ?
		WHERE job_key = ?`, toDBTime(ranAt), status, errMsg, durationMS, key); err != nil {
		return err
	}

	// created_at 用执行开始时间，与 last_run_at 保持一致——
	// 用 CURRENT_TIMESTAMP 的话，每次执行都会多一个几乎相同但不等的时刻
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sys_job_logs (created_at, job_key, trigger, status, error, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?)`,
		toDBTime(ranAt), key, trigger, status, errMsg, durationMS); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *JobRepository) CountRunLogs(ctx context.Context, key string) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sys_job_logs WHERE job_key = ?`, key).Scan(&n)
	return n, err
}

// ListRunLogs 按任务翻历史，最新一次在最前。
//
// 按 id 排序而不是 created_at：同一次执行的两个时刻毫秒级相邻，
// 而 id 是严格递增的，翻页不会因为时间相同而抖动。
func (r *JobRepository) ListRunLogs(ctx context.Context, key string, limit, offset int) ([]domain.JobLog, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, created_at, job_key, trigger, status, error, duration_ms
		FROM sys_job_logs WHERE job_key = ?
		ORDER BY id DESC LIMIT ? OFFSET ?`, key, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.JobLog, 0, limit)
	for rows.Next() {
		var l domain.JobLog
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.JobKey, &l.Trigger, &l.Status, &l.Error, &l.DurationMS); err != nil {
			return nil, err
		}
		list = append(list, l)
	}
	return list, rows.Err()
}
