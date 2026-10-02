package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"mini-ruoyi/internal/domain"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

const userColumns = `id, created_at, updated_at, status, username, password,
	nickname, mobile, email, login_ip, login_at`

func scanUser(s interface{ Scan(...any) error }) (domain.User, error) {
	var u domain.User
	err := s.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Status, &u.Username, &u.Password,
		&u.Nickname, &u.Mobile, &u.Email, &u.LoginIP, &u.LoginAt)
	return u, err
}

// UserFilter 是用户列表的筛选条件。空字符串（或 0）表示不限制。
type UserFilter struct {
	Username string // 模糊匹配
	Nickname string // 模糊匹配
	Mobile   string // 模糊匹配
	Status   string // 精确匹配
	// RoleID 按角色筛。0 表示不限制。
	RoleID int64
}

func userWhere(f UserFilter) (string, []any) {
	var b whereBuilder
	// 列名是代码里的常量，不接受外部输入，所以拼接是安全的
	b.like("username", f.Username)
	b.like("nickname", f.Nickname)
	b.like("mobile", f.Mobile)
	b.eq("status", f.Status)

	// 按角色筛用 EXISTS 而不是 JOIN：
	//   * WHERE 是 whereBuilder 单独拼的，JOIN 得改 FROM 那一侧，List 与 Count 都要跟着改
	//   * EXISTS 天然不会因为关联表命中多行而把结果行数翻倍——
	//     换成 JOIN 的话，将来若改成「多重角色」筛选就会错得很难查
	if f.RoleID > 0 {
		b.raw(`EXISTS (SELECT 1 FROM sys_user_roles ur
			WHERE ur.user_id = sys_users.id AND ur.role_id = ?)`, f.RoleID)
	}
	return b.clause(), b.args
}

func (r *UserRepository) List(ctx context.Context, f UserFilter, limit, offset int) ([]domain.User, error) {
	where, args := userWhere(f)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM sys_users`+where+` ORDER BY id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.User, 0, limit)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

func (r *UserRepository) Count(ctx context.Context, f UserFilter) (int64, error) {
	where, args := userWhere(f)

	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users`+where, args...).Scan(&n)
	return n, err
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (domain.User, error) {
	u, err := scanUser(r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM sys_users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (domain.User, error) {
	u, err := scanUser(r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM sys_users WHERE username = ?`, username))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

func (r *UserRepository) Create(ctx context.Context, u domain.User) (domain.User, error) {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO sys_users (status, username, password, nickname, mobile, email)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at`,
		u.Status, u.Username, u.Password, u.Nickname, u.Mobile, u.Email,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return domain.User{}, err
	}
	return u, nil
}

// UpdateProfile 只改资料字段。username 不可变，password 走独立方法。
func (r *UserRepository) UpdateProfile(ctx context.Context, u domain.User) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE sys_users
		SET status = ?, nickname = ?, mobile = ?, email = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		u.Status, u.Nickname, u.Mobile, u.Email, u.ID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (r *UserRepository) UpdatePassword(ctx context.Context, id int64, hash string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE sys_users SET password = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, hash, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sys_users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// CountOtherAdmins 返回除 excludeID 之外，仍持有内置 admin 角色且处于启用状态的用户数。
//
// 删除或停用用户前必须检查它：把最后一个管理员删掉之后，没有任何界面能恢复，
// 只能手工改库。停用同样要检查——被停用的管理员登不进来，效果和删除一样。
func (r *UserRepository) CountOtherAdmins(ctx context.Context, excludeID int64) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT u.id)
		FROM sys_users u
		JOIN sys_user_roles ur ON ur.user_id = u.id
		JOIN sys_roles r       ON r.id = ur.role_id
		WHERE r.code = ? AND u.status = ? AND u.id <> ?`,
		domain.RoleCodeAdmin, domain.StatusActive, excludeID).Scan(&n)
	return n, err
}

// HasRole 判断用户是否直接持有某个角色。
func (r *UserRepository) HasRole(ctx context.Context, userID int64, roleCode string) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM sys_user_roles ur
			JOIN sys_roles r ON r.id = ur.role_id
			WHERE ur.user_id = ? AND r.code = ?
		)`, userID, roleCode).Scan(&ok)
	return ok, err
}

// RoleIDs 返回用户当前持有的角色 id 列表。
func (r *UserRepository) RoleIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT role_id FROM sys_user_roles WHERE user_id = ? ORDER BY role_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ReplaceRoles 在单事务里重设用户的角色绑定。
//
// 先删后插必须同事务：中途失败若留下空绑定，该用户会变成无任何权限的账号。
func (r *UserRepository) ReplaceRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后这里是空操作

	if _, err := tx.ExecContext(ctx, `DELETE FROM sys_user_roles WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sys_user_roles (user_id, role_id) VALUES (?, ?)`, userID, roleID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ExistsUsername 判断用户名是否已被占用（excludeID 用于编辑时排除自己）。
func (r *UserRepository) ExistsUsername(ctx context.Context, username string, excludeID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM sys_users WHERE username = ? AND id <> ?)`,
		username, excludeID).Scan(&ok)
	return ok, err
}

// SetLoginInfo 记录最近一次成功登录。登录是低频操作，直接写库即可。
func (r *UserRepository) SetLoginInfo(ctx context.Context, userID int64, ip string, at time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sys_users SET login_ip = ?, login_at = ? WHERE id = ?`,
		ip, toDBTime(at), userID)
	return err
}
