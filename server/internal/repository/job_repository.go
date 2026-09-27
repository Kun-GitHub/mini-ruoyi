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

// RecordRun 记录一次执行结果。
func (r *JobRepository) RecordRun(ctx context.Context, key, status, errMsg string, ranAt time.Time, durationMS int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE sys_jobs
		SET last_run_at = ?, last_status = ?, last_error = ?, last_duration_ms = ?
		WHERE job_key = ?`, toDBTime(ranAt), status, errMsg, durationMS, key)
	return err
}
