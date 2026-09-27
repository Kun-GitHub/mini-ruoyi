-- 0009 系统工具菜单
--
-- 与「系统管理」「系统监控」并列的第三个目录：
--   系统管理 = 改配置，系统监控 = 看运行状况，系统工具 = 主动操作点什么。
-- 受众与权限点都不同，所以不塞进已有的两个目录。
--
-- 兼容已有库：目录不存在时才插入。

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT NULL, 3, 'directory', 'menu.tool', '/tool', '', 'wrench'
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title_key = 'menu.tool');

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 1, 'menu', 'menu.tool.files', '/tool/files', 'tool/files', 'folder'
FROM sys_menus WHERE title_key = 'menu.tool';

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 2, 'menu', 'menu.tool.jobs', '/tool/jobs', 'tool/jobs', 'timer'
FROM sys_menus WHERE title_key = 'menu.tool';
