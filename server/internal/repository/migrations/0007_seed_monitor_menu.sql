-- 0007 系统监控菜单
--
-- 三个页面挂在一个新的「系统监控」目录下，而不是塞进「系统管理」：
-- 系统管理是改配置，系统监控是看运行状况，受众与权限点都不同
-- （monitor:* 通常会给更宽的人看）。
--
-- 兼容已有库：只在目录不存在时插入，避免重复执行时产生第二份。

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT NULL, 2, 'directory', 'menu.monitor', '/monitor', '', 'activity'
WHERE NOT EXISTS (SELECT 1 FROM sys_menus WHERE title_key = 'menu.monitor');

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 1, 'menu', 'menu.monitor.sessions', '/monitor/sessions', 'monitor/sessions', 'users'
FROM sys_menus WHERE title_key = 'menu.monitor';

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 2, 'menu', 'menu.monitor.loginlogs', '/monitor/loginlogs', 'monitor/loginlogs', 'log-in'
FROM sys_menus WHERE title_key = 'menu.monitor';

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 3, 'menu', 'menu.monitor.operlogs', '/monitor/operlogs', 'monitor/operlogs', 'scroll-text'
FROM sys_menus WHERE title_key = 'menu.monitor';
