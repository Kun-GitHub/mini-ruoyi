// 有问题欢迎联系 hot_kun@hotmail.com
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

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

// CreateWithinQuota 在单条语句里完成「总量 + 本次是否超配额」的判断并登记。
//
// 不能用「先 Usage 读一次、再 Create」：两个并发上传会读到同一个 used，
// 各自都算得下，一起写进去就超了配额。把判断搬进 INSERT 的 WHERE 里，
// SQLite 的单写者模型就替我们把这一步串行化了——要么这条插入看到前面已经落库的字节，
// 要么反过来，不会两个都看到旧值。
//
// quota <= 0 表示不限总量，此时不参与条件判断。
// 超配额时返回 domain.ErrQuotaExceeded。
func (r *FileRepository) CreateWithinQuota(ctx context.Context, f domain.File, quota int64) (domain.File, error) {
	query := `
		INSERT INTO sys_files (group_name, original_name, storage_path, size, content_type, uploader_id, uploader_name)
		SELECT ?, ?, ?, ?, ?, ?, ?
		WHERE ? <= 0 OR (SELECT COALESCE(SUM(size), 0) FROM sys_files) + ? <= ?
		RETURNING id, created_at`

	err := r.db.QueryRowContext(ctx, query,
		f.GroupName, f.OriginalName, f.StoragePath, f.Size, f.ContentType, f.UploaderID, f.UploaderName,
		quota, f.Size, quota,
	).Scan(&f.ID, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// 条件不满足，一条也没插进去
		return domain.File{}, domain.ErrQuotaExceeded
	}
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
// ExistsStoragePath 判断某个磁盘路径是否已有记录。
//
// 给孤儿清理的「删除前最后一次确认」用：从拿到清单到轮到某个文件，
// 中间隔了整个目录遍历的时间，足够一个并发上传把自己的登记写完。
func (r *FileRepository) ExistsStoragePath(ctx context.Context, storagePath string) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM sys_files WHERE storage_path = ?)`, storagePath).Scan(&ok)
	return ok, err
}

// List 返回磁盘上的全部文件相对路径，供孤儿清理比对。
//
// 先拿到磁盘清单再查库，是清理流程里唯一能缩窄竞态窗口的顺序：
// 反过来（先查库再遍历磁盘）会把「查库之后、遍历之前」上传的文件当成孤儿。
// 这个方向下，最坏情况是刚被删掉的文件本轮没被收，下轮再收——无害。
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

// StoragePathsCreatedBefore 返回创建时间早于阈值的文件路径集合。
//
// 只让「有年代」的文件可能被当孤儿清理：刚上传、元数据还没写完的文件
// 不会落入这个集合，于是不会被并发跑的清理任务误删。
func (r *FileRepository) StoragePathsCreatedBefore(ctx context.Context, before time.Time) (map[string]struct{}, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT storage_path FROM sys_files WHERE created_at < ?`, toDBTime(before))
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
