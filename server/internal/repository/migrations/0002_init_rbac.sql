-- 0002 RBAC 基础表
--
-- 字段定义与取舍理由见 docs/schema.md。要点：
--   * 主键一律 INTEGER PRIMARY KEY（写 BIGINT 会失去 rowid 别名）
--   * 时间字段用 datetime（timestamptz 无法被驱动解析成 time.Time）
--   * sys_menus.parent_id 用 NULL 表示根节点，配自引用外键级联删除，
--     这样删目录会自动带走整棵子树，不会留下树里不可见的孤儿行
--   * 不做多租户、不做软删除、不做字典表
--   * varchar(n) 的长度 SQLite 不强制，真正的校验在 Go 的 binding tag 上

CREATE TABLE sys_users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     varchar  NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    username   varchar(128) NOT NULL UNIQUE,
    password   varchar(255) NOT NULL,
    nickname   varchar(128) NOT NULL DEFAULT '',
    mobile     varchar(20)  NOT NULL DEFAULT '',
    email      varchar(64)  NOT NULL DEFAULT '',
    login_ip   varchar(64)  NOT NULL DEFAULT '',
    login_at   datetime     NULL
);

CREATE TABLE sys_roles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     varchar  NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    code       varchar(64)  NOT NULL UNIQUE,
    name       varchar(128) NOT NULL,
    remark     varchar(255) NOT NULL DEFAULT ''
);

CREATE TABLE sys_menus (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     varchar  NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    parent_id  INTEGER  NULL REFERENCES sys_menus(id) ON DELETE CASCADE,
    sort       INTEGER  NOT NULL DEFAULT 0,
    menu_type  varchar  NOT NULL DEFAULT 'directory' CHECK (menu_type IN ('directory','menu')),
    title_key  varchar(128) NOT NULL,
    path       varchar(255) NOT NULL DEFAULT '',
    component  varchar(255) NOT NULL DEFAULT '',
    icon       varchar(64)  NOT NULL DEFAULT ''
);

CREATE TABLE sys_user_roles (
    user_id INTEGER NOT NULL REFERENCES sys_users(id) ON DELETE CASCADE,
    role_id INTEGER NOT NULL REFERENCES sys_roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE sys_role_menus (
    role_id INTEGER NOT NULL REFERENCES sys_roles(id) ON DELETE CASCADE,
    menu_id INTEGER NOT NULL REFERENCES sys_menus(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, menu_id)
);

CREATE TABLE sys_role_perms (
    role_id   INTEGER NOT NULL REFERENCES sys_roles(id) ON DELETE CASCADE,
    perm_code varchar(64) NOT NULL,
    PRIMARY KEY (role_id, perm_code)
);

-- 复合主键的最左前缀覆盖正向查询，反向查询需要补索引
CREATE INDEX idx_menus_parent_sort ON sys_menus(parent_id, sort);
CREATE INDEX idx_user_roles_role   ON sys_user_roles(role_id);
CREATE INDEX idx_role_menus_menu   ON sys_role_menus(menu_id);
CREATE INDEX idx_role_perms_code   ON sys_role_perms(perm_code);
