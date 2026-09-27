-- 0005 API 权限清单菜单
--
-- 这个页面是**只读**的：权限点由代码声明（internal/perm），界面不提供新增与修改。
-- 理由是新增一个权限点必须同时有「代码声明 + 路由引用 + 后端校验」三样东西，
-- 只落库的权限码永远不会被任何路由引用，勾了也不生效。
-- 详见 docs/schema.md §8。
--
-- 用子查询取父目录 id，不写死自增值。

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 4, 'menu', 'menu.system.apis', '/system/apis', 'system/apis', 'plug'
FROM sys_menus WHERE title_key = 'menu.system';
