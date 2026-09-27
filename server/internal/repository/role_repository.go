package repository

import (
	"context"
	"database/sql"
	"errors"

	"mini-ruoyi/internal/domain"
)

type RoleRepository struct {
	db *sql.DB
}

func NewRoleRepository(db *sql.DB) *RoleRepository {
	return &RoleRepository{db: db}
}

const roleColumns = `id, created_at, updated_at, status, code, name, remark`

func scanRole(s interface{ Scan(...any) error }) (domain.Role, error) {
	var r domain.Role
	err := s.Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.Status, &r.Code, &r.Name, &r.Remark)
	return r, err
}

// RoleFilter 是角色列表的筛选条件。空字符串表示不限制。
type RoleFilter struct {
	Code   string // 模糊匹配
	Name   string // 模糊匹配
	Status string // 精确匹配
}

func roleWhere(f RoleFilter) (string, []any) {
	var b whereBuilder
	b.like("code", f.Code)
	b.like("name", f.Name)
	b.eq("status", f.Status)
	return b.clause(), b.args
}

func (r *RoleRepository) List(ctx context.Context, f RoleFilter, limit, offset int) ([]domain.Role, error) {
	where, args := roleWhere(f)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx,
		`SELECT `+roleColumns+` FROM sys_roles`+where+` ORDER BY id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.Role, 0, limit)
	for rows.Next() {
		item, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (r *RoleRepository) Count(ctx context.Context, f RoleFilter) (int64, error) {
	where, args := roleWhere(f)

	var n int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_roles`+where, args...).Scan(&n)
	return n, err
}

func (r *RoleRepository) GetByID(ctx context.Context, id int64) (domain.Role, error) {
	item, err := scanRole(r.db.QueryRowContext(ctx,
		`SELECT `+roleColumns+` FROM sys_roles WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Role{}, domain.ErrNotFound
	}
	return item, err
}

func (r *RoleRepository) GetByCode(ctx context.Context, code string) (domain.Role, error) {
	item, err := scanRole(r.db.QueryRowContext(ctx,
		`SELECT `+roleColumns+` FROM sys_roles WHERE code = ?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Role{}, domain.ErrNotFound
	}
	return item, err
}

func (r *RoleRepository) Create(ctx context.Context, role domain.Role) (domain.Role, error) {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO sys_roles (status, code, name, remark) VALUES (?, ?, ?, ?)
		RETURNING id, created_at, updated_at`,
		role.Status, role.Code, role.Name, role.Remark,
	).Scan(&role.ID, &role.CreatedAt, &role.UpdatedAt)
	if err != nil {
		return domain.Role{}, err
	}
	return role, nil
}

func (r *RoleRepository) Update(ctx context.Context, role domain.Role) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE sys_roles SET status = ?, name = ?, remark = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		role.Status, role.Name, role.Remark, role.ID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (r *RoleRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sys_roles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// CountAffectedUsers 返回持有该角色的用户数量。删除角色会级联清空这些绑定，
// 从而改变这些用户的能力，所以属于需要确认的影响面。
func (r *RoleRepository) CountAffectedUsers(ctx context.Context, roleID int64) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sys_user_roles WHERE role_id = ?`, roleID).Scan(&n)
	return n, err
}

// ListPermCodes 返回全部角色已授予的权限码（去重），供启动校验使用。
func (r *RoleRepository) ListPermCodes(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT perm_code FROM sys_role_perms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// ListPermCodesOfUser 返回某用户通过角色获得的全部权限码（去重）。
//
// 注意：内置 admin 角色的权限是隐式放行的，不写 sys_role_perms，
// 所以这里查不出它的权限——调用方必须先判断是否内置管理员，见 AuthzService。
func (r *RoleRepository) ListPermCodesOfUser(ctx context.Context, userID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT rp.perm_code
		FROM sys_user_roles ur
		JOIN sys_role_perms rp ON rp.role_id = ur.role_id
		WHERE ur.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	codes := make([]string, 0)
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// ExistsCode 判断角色编码是否已被占用，供创建/改名前的友好校验使用。
//
// 唯一约束本身也会兜住（并发下仍需它），但走到约束错误时只能给出泛化的 409；
// 先查一次能告诉用户是哪个字段冲突。
func (r *RoleRepository) ExistsCode(ctx context.Context, code string, excludeID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM sys_roles WHERE code = ? AND id <> ?)`,
		code, excludeID).Scan(&ok)
	return ok, err
}

// MenuIDs 返回角色已授权的菜单 id 列表。
func (r *RoleRepository) MenuIDs(ctx context.Context, roleID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT menu_id FROM sys_role_menus WHERE role_id = ? ORDER BY menu_id`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PermCodesOfRole 返回角色已授予的权限码。
func (r *RoleRepository) PermCodesOfRole(ctx context.Context, roleID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT perm_code FROM sys_role_perms WHERE role_id = ? ORDER BY perm_code`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	codes := make([]string, 0)
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// ReplaceGrants 在单事务里重设角色的菜单授权与权限码。
//
// 两者必须同事务：菜单决定导航可见性、权限码决定接口可调用性，
// 中途失败会留下「菜单看得见但点进去 403」这种自相矛盾的权限状态。
func (r *RoleRepository) ReplaceGrants(ctx context.Context, roleID int64, menuIDs []int64, permCodes []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后这里是空操作

	for _, stmt := range []string{
		`DELETE FROM sys_role_menus WHERE role_id = ?`,
		`DELETE FROM sys_role_perms WHERE role_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, roleID); err != nil {
			return err
		}
	}

	for _, menuID := range menuIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sys_role_menus (role_id, menu_id) VALUES (?, ?)`, roleID, menuID); err != nil {
			return err
		}
	}
	for _, code := range permCodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sys_role_perms (role_id, perm_code) VALUES (?, ?)`, roleID, code); err != nil {
			return err
		}
	}
	return tx.Commit()
}
