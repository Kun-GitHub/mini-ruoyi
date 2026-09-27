-- 0006 操作日志与登录日志
--
-- 两张表都只做「追加 + 定期删旧」，不做修改，所以：
--   * 没有 updated_at —— 日志被改过就失去审计价值了
--   * user_id 不设外键 —— 用户被删除后日志必须留下，否则删号就能抹掉痕迹。
--     username 也冗余存一份，同理
--
-- 保留期由应用层控制（默认 30 天，见 APP_LOG_RETENTION_DAYS），
-- 因此两张表都需要 created_at 上的索引来支撑删除。

CREATE TABLE sys_login_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    username   varchar(128) NOT NULL,
    -- success / failed。失败原因单独一列，便于统计「密码错」还是「账号停用」
    status     varchar(16)  NOT NULL CHECK (status IN ('success','failed')),
    -- i18n 键，如 error.badCredentials
    reason     varchar(64)  NOT NULL DEFAULT '',
    ip         varchar(64)  NOT NULL DEFAULT '',
    user_agent varchar(255) NOT NULL DEFAULT ''
);

CREATE INDEX idx_login_logs_created ON sys_login_logs(created_at);
CREATE INDEX idx_login_logs_user    ON sys_login_logs(username, created_at);

CREATE TABLE sys_oper_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at  datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- 用户被删除后这里仍留着当时的 id 与用户名
    user_id     INTEGER     NOT NULL DEFAULT 0,
    username    varchar(128) NOT NULL DEFAULT '',
    method      varchar(10)  NOT NULL,
    path        varchar(255) NOT NULL,
    status      INTEGER      NOT NULL,
    -- 失败时的 i18n 键，如 error.forbidden
    result      varchar(64)  NOT NULL DEFAULT '',
    duration_ms INTEGER      NOT NULL DEFAULT 0,
    ip          varchar(64)  NOT NULL DEFAULT '',
    user_agent  varchar(255) NOT NULL DEFAULT ''
);

CREATE INDEX idx_oper_logs_created ON sys_oper_logs(created_at);
CREATE INDEX idx_oper_logs_user    ON sys_oper_logs(user_id, created_at);
