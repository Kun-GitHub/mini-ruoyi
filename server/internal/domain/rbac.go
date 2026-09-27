package domain

import "time"

// Role 对应 sys_roles 表。
type Role struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Status    string    `json:"status"`
	Code      string    `json:"code"` // 稳定标识，如 admin
	Name      string    `json:"name"`
	Remark    string    `json:"remark"`
}

func (r *Role) IsBuiltin() bool { return r.Code == RoleCodeAdmin }

// Menu 对应 sys_menus 表。ParentID 为 nil 表示根节点。
type Menu struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Status    string    `json:"status"`
	ParentID  *int64    `json:"parent_id"`
	Sort      int       `json:"sort"`
	MenuType  string    `json:"menu_type"`
	TitleKey  string    `json:"title_key"` // i18n 键，文案由前端渲染
	Path      string    `json:"path"`
	Component string    `json:"component"`
	Icon      string    `json:"icon"`
}

// MenuNode 是菜单树节点。children 为空时序列化成 []，前端不必特判 null。
type MenuNode struct {
	Menu
	Children []*MenuNode `json:"children"`
}
