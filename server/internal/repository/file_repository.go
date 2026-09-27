package repository

import (
	"context"
	"database/sql"
	"errors"

	"mini-ruoyi/internal/domain"
)

type FileRepository struct {
	db *sql.DB
}

func NewFileRepository(db *sql.DB) *FileRepository {
	return &FileRepository{db: db}
}

const fileColumns = `id, created_at, group_name, original_name, storage_path,
	size, content_type, uploader_id, uploader_name`

func scanFile(s interface{ Scan(...any) error }) (domain.File, error) {
	var f domain.File
	err := s.Scan(&f.ID, &f.CreatedAt, &f.GroupName, &f.OriginalName, &f.StoragePath,
		&f.Size, &f.ContentType, &f.UploaderID, &f.UploaderName)
	return f, err
}

func (r *FileRepository) Create(ctx context.Context, f domain.File) (domain.File, error) {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO sys_files (group_name, original_name, storage_path, size, content_type, uploader_id, uploader_name)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id, created_at`,
		f.GroupName, f.OriginalName, f.StoragePath, f.Size, f.ContentType, f.UploaderID, f.UploaderName,
	).Scan(&f.ID, &f.CreatedAt)
	if err != nil {
		return domain.File{}, err
	}
	return f, nil
}

func (r *FileRepository) GetByID(ctx context.Context, id int64) (domain.File, error) {
	f, err := scanFile(r.db.QueryRowContext(ctx,
		`SELECT `+fileColumns+` FROM sys_files WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.File{}, domain.ErrNotFound
	}
	return f, err
}

// FileFilter 是文件列表的筛选条件。
type FileFilter struct {
	OriginalName string // 模糊
	GroupName    string // 精确
}

func fileWhere(f FileFilter) (string, []any) {
	var b whereBuilder
	b.like("original_name", f.OriginalName)
	b.eq("group_name", f.GroupName)
	return b.clause(), b.args
}

func (r *FileRepository) Count(ctx context.Context, f FileFilter) (int64, error) {
	where, args := fileWhere(f)

	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_files`+where, args...).Scan(&n)
	return n, err
}

func (r *FileRepository) List(ctx context.Context, f FileFilter, limit, offset int) ([]domain.File, error) {
	where, args := fileWhere(f)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx,
		`SELECT `+fileColumns+` FROM sys_files`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.File, 0, limit)
	for rows.Next() {
		item, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// Usage 返回总占用字节数与文件个数。
func (r *FileRepository) Usage(ctx context.Context) (int64, int64, error) {
	var used, count sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size), 0), COUNT(*) FROM sys_files`).Scan(&used, &count)
	if err != nil {
		return 0, 0, err
	}
	return used.Int64, count.Int64, nil
}

func (r *FileRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sys_files WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// AllStoragePaths 返回全部磁盘相对路径，供孤儿清理比对。
//
// 一次性载入内存：这是**删除孤儿文件**用的，只有清理任务会调，
// 而且路径很短。按十万个文件算也就几 MB，比逐条查库再比对简单得多。
func (r *FileRepository) AllStoragePaths(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT storage_path FROM sys_files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]struct{}{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = struct{}{}
	}
	return out, rows.Err()
}
