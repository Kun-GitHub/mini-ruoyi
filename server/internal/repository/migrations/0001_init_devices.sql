-- 0001 示例资源 devices。
--
-- 仅作为 CRUD / 分页 / 迁移机制的参考实现，正式的业务表（用户、角色、菜单、权限）
-- 后续以新的迁移文件追加。
--
-- 用 IF NOT EXISTS 是为了兼容仓库中随附的初始 data.db：该库由更早的
-- "CREATE TABLE IF NOT EXISTS" 版本创建，表已存在但没有任何迁移记录。
CREATE TABLE IF NOT EXISTS devices (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    location   TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_devices_location ON devices(location);
