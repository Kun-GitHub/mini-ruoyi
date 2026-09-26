package repository

import (
	"context"
	"database/sql"
	"errors"

	"mini-ruoyi/internal/domain"
)

type DeviceRepository struct {
	db *sql.DB
}

func NewDeviceRepository(db *sql.DB) *DeviceRepository {
	return &DeviceRepository{db: db}
}

// Create 用 RETURNING 把库生成的 id 和 created_at 一次带回，
// 否则调用方拿到的 Device 里 created_at 会是零值。
func (r *DeviceRepository) Create(ctx context.Context, d domain.Device) (domain.Device, error) {
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO devices (name, location, enabled) VALUES (?, ?, ?)
		 RETURNING id, created_at`,
		d.Name, d.Location, d.Enabled,
	).Scan(&d.ID, &d.CreatedAt)
	if err != nil {
		return domain.Device{}, err
	}
	return d, nil
}

func (r *DeviceRepository) List(ctx context.Context, limit, offset int) ([]domain.Device, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, location, enabled, created_at FROM devices
		 ORDER BY id DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 用 make 而不是 var：确保空结果序列化成 [] 而不是 null，前端不必特判
	list := make([]domain.Device, 0, limit)
	for rows.Next() {
		var d domain.Device
		if err := rows.Scan(&d.ID, &d.Name, &d.Location, &d.Enabled, &d.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

func (r *DeviceRepository) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&n)
	return n, err
}

func (r *DeviceRepository) GetByID(ctx context.Context, id int64) (domain.Device, error) {
	var d domain.Device
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, location, enabled, created_at FROM devices WHERE id = ?`, id,
	).Scan(&d.ID, &d.Name, &d.Location, &d.Enabled, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Device{}, domain.ErrNotFound
	}
	return d, err
}

func (r *DeviceRepository) Update(ctx context.Context, id int64, enabled bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE devices SET enabled = ? WHERE id = ?`, enabled, id,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *DeviceRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
