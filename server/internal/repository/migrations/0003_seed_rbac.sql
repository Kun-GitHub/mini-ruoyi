-- 0003 内置角色、管理员账号与菜单树
--
-- 全部用子查询取 id，不写死自增值：迁移在 0002 之后立即执行，这几张表必然为空，
-- 但依赖自增值是脆的，万一将来调整执行顺序就会错位。
--
-- 管理员密码是 bcrypt('admin123')，明文弱密码，登录后应立即修改。
-- 该角色（code='admin'）的权限是隐式的：代码判定 code=='admin' 直接放行，
-- 因此这里不写 sys_role_perms / sys_role_menus。

INSERT INTO sys_roles (code, name, remark) VALUES
    ('admin', '超级管理员', '内置角色，拥有全部权限，不可删除');

INSERT INTO sys_users (username, password, nickname, email) VALUES
    ('admin', '$2a$10$O1UW/1SDGcHyLeYaD4jspeGy5S/oW/3NaqWz77eSa1gLpBzgtrwrC', '管理员', '');

INSERT INTO sys_user_roles (user_id, role_id)
SELECT u.id, r.id FROM sys_users u, sys_roles r
WHERE u.username = 'admin' AND r.code = 'admin';

-- 菜单树。title_key 由前端 i18n 字典翻译（web/src/lib/i18n/），
-- component 是相对 web/src/pages/ 的模块路径（不带扩展名），由前端 import.meta.glob 解析。
INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
VALUES (NULL, 1, 'directory', 'menu.system', '/system', '', 'settings');

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 1, 'menu', 'menu.system.users', '/system/users', 'system/users', 'users'
FROM sys_menus WHERE title_key = 'menu.system';

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 2, 'menu', 'menu.system.roles', '/system/roles', 'system/roles', 'shield'
FROM sys_menus WHERE title_key = 'menu.system';

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 3, 'menu', 'menu.system.menus', '/system/menus', 'system/menus', 'list'
FROM sys_menus WHERE title_key = 'menu.system';
