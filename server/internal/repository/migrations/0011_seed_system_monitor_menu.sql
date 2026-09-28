-- 0011 服务监控菜单
--
-- 挂在「系统监控」目录下，排在最前（sort=1），其余三页的 sort 依次后移。
-- 先插新行再整体重排，避免手工改多个现有行的编号。

INSERT INTO sys_menus (parent_id, sort, menu_type, title_key, path, component, icon)
SELECT id, 1, 'menu', 'menu.monitor.system', '/monitor/system', 'monitor/system', 'activity'
FROM sys_menus WHERE title_key = 'menu.monitor'
  AND NOT EXISTS (SELECT 1 FROM sys_menus WHERE title_key = 'menu.monitor.system');

UPDATE sys_menus SET sort = 2 WHERE title_key = 'menu.monitor.sessions';
UPDATE sys_menus SET sort = 3 WHERE title_key = 'menu.monitor.loginlogs';
UPDATE sys_menus SET sort = 4 WHERE title_key = 'menu.monitor.operlogs';
