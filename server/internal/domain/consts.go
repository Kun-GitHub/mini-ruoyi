package domain

// 状态与类型的取值。数据库里的 CHECK 约束与这里必须一致（见 docs/schema.md）。
const (
	StatusActive   = "active"
	StatusInactive = "inactive"

	MenuTypeDirectory = "directory"
	MenuTypeMenu      = "menu"
)

// 内置角色标识。代码据此判断超级管理员，该角色的权限是隐式放行的，
// 不写入 sys_role_perms / sys_role_menus（见 docs/schema.md §8.2）。
const RoleCodeAdmin = "admin"
