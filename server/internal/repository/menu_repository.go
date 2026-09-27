package repository

import (
	"context"
	"database/sql"
	"errors"

	"mini-ruoyi/internal/domain"
)

type MenuRepository struct {
	db *sql.DB
}

func NewMenuRepository(db *sql.DB) *MenuRepository {
	return &MenuRepository{db: db}
}

const menuColumns = `id, created_at, updated_at, status, parent_id, sort,
	menu_type, title_key, path, component, icon`

func scanMenu(s interface{ Scan(...any) error }) (domain.Menu, error) {
	var m domain.Menu
	err := s.Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt, &m.Status, &m.ParentID, &m.Sort,
		&m.MenuType, &m.TitleKey, &m.Path, &m.Component, &m.Icon)
	return m, err
}

// List 返回菜单平铺列表。菜单数量少（几十条），不分页，由 service 组装成树。
// activeOnly 为 true 时只返回启用状态的菜单（给前端导航用）。
func (r *MenuRepository) List(ctx context.Context, activeOnly bool) ([]domain.Menu, error) {
	return r.query(ctx, `
		SELECT `+menuColumns+` FROM sys_menus
		WHERE ? = 0 OR status = 'active'
		ORDER BY sort, id`, boolToInt(activeOnly))
}

// ListForUser 返回某用户通过角色获得授权的启用菜单。
func (r *MenuRepository) ListForUser(ctx context.Context, userID int64) ([]domain.Menu, error) {
	return r.query(ctx, `
		SELECT DISTINCT `+menuColumns+`
		FROM sys_menus m
		JOIN sys_role_menus rm ON rm.menu_id = m.id
		JOIN sys_user_roles ur ON ur.role_id = rm.role_id
		WHERE ur.user_id = ? AND m.status = 'active'
		ORDER BY m.sort, m.id`, userID)
}

func (r *MenuRepository) query(ctx context.Context, q string, args ...any) ([]domain.Menu, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 用 make 保证空结果序列化成 [] 而不是 null
	list := make([]domain.Menu, 0)
	for rows.Next() {
		m, err := scanMenu(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (r *MenuRepository) GetByID(ctx context.Context, id int64) (domain.Menu, error) {
	m, err := scanMenu(r.db.QueryRowContext(ctx,
		`SELECT `+menuColumns+` FROM sys_menus WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Menu{}, domain.ErrNotFound
	}
	return m, err
}

func (r *MenuRepository) Create(ctx context.Context, m domain.Menu) (domain.Menu, error) {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO sys_menus (status, parent_id, sort, menu_type, title_key, path, component, icon)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, created_at, updated_at`,
		m.Status, m.ParentID, m.Sort, m.MenuType, m.TitleKey, m.Path, m.Component, m.Icon,
	).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return domain.Menu{}, err
	}
	return m, nil
}

// Update 全量覆盖可编辑字段。updated_at 由语句显式刷新。
func (r *MenuRepository) Update(ctx context.Context, m domain.Menu) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE sys_menus
		SET status = ?, parent_id = ?, sort = ?, menu_type = ?,
		    title_key = ?, path = ?, component = ?, icon = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		m.Status, m.ParentID, m.Sort, m.MenuType,
		m.TitleKey, m.Path, m.Component, m.Icon, m.ID,
	)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (r *MenuRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sys_menus WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// CountDescendants 返回后代菜单数量（不含自己）。
func (r *MenuRepository) CountDescendants(ctx context.Context, id int64) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `
		WITH RECURSIVE sub(id) AS (
			SELECT id FROM sys_menus WHERE id = ?
			UNION ALL
			SELECT m.id FROM sys_menus m JOIN sub ON m.parent_id = sub.id
		)
		SELECT COUNT(*) - 1 FROM sub`, id).Scan(&n)
	return n, err
}

// CountAffectedRoles 返回引用了该菜单或其任一后代的角色数量。
//
// 只数自己的直接引用是不够的：级联删除会把整棵子树带走，
// 那些子菜单的授权同样会失效，所以影响面必须算上后代。
func (r *MenuRepository) CountAffectedRoles(ctx context.Context, id int64) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx, `
		WITH RECURSIVE sub(id) AS (
			SELECT id FROM sys_menus WHERE id = ?
			UNION ALL
			SELECT m.id FROM sys_menus m JOIN sub ON m.parent_id = sub.id
		)
		SELECT COUNT(DISTINCT rm.role_id) FROM sys_role_menus rm
		WHERE rm.menu_id IN (SELECT id FROM sub)`, id).Scan(&n)
	return n, err
}

// requireAffected 把「影响 0 行」翻译成 ErrNotFound。
func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
