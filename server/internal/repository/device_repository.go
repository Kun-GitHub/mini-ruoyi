package repository

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("device not found")

type Device struct {
	ID        int64
	Name      string
	Location  string
	Enabled   bool
	CreatedAt string
}

type DeviceRepository struct {
	db *sql.DB
}

func NewDeviceRepository(db *sql.DB) *DeviceRepository {
	return &DeviceRepository{db: db}
}

func (r *DeviceRepository) Create(ctx context.Context, d Device) (Device, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO devices (name, location, enabled) VALUES (?, ?, ?)`,
		d.Name, d.Location, d.Enabled,
	)
	if err != nil {
		return Device{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Device{}, err
	}
	d.ID = id
	return d, nil
}

func (r *DeviceRepository) List(ctx context.Context, limit, offset int) ([]Device, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, location, enabled, created_at FROM devices
		 ORDER BY id DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Name, &d.Location, &d.Enabled, &d.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

func (r *DeviceRepository) GetByID(ctx context.Context, id int64) (Device, error) {
	var d Device
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, location, enabled, created_at FROM devices WHERE id = ?`, id,
	).Scan(&d.ID, &d.Name, &d.Location, &d.Enabled, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
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
		return ErrNotFound
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
		return ErrNotFound
	}
	return nil
}
