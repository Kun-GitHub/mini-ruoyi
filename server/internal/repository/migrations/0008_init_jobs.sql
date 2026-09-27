-- 0008 定时任务
--
-- 任务本体在代码里（internal/job），这张表只存**可调参数**：
-- 启用/禁用、cron 表达式，以及最近一次执行结果。
--
-- 不建执行历史表：核心问题是「上次成功了吗」，job 行上存四个字段就能回答。
-- 要历史再加表，现在建了只会多一张需要清理的表。
--
-- job_key 的语义与 sys_role_perms.perm_code 相同：由代码声明，
-- 启动时按代码注册表 upsert，所以界面上不需要「新建任务」。

CREATE TABLE sys_jobs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status     varchar NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    job_key    varchar(64) NOT NULL UNIQUE,
    cron       varchar(64) NOT NULL,
    remark     varchar(255) NOT NULL DEFAULT '',

    -- 最近一次执行。NULL 表示从未执行过。
    last_run_at      datetime NULL,
    -- 空串表示从未执行；skipped 表示上一次因为「上一次还没跑完」被跳过
    last_status      varchar(16) NOT NULL DEFAULT ''
                     CHECK (last_status IN ('', 'success', 'failed', 'skipped')),
    last_error       varchar(255) NOT NULL DEFAULT '',
    last_duration_ms INTEGER NOT NULL DEFAULT 0
);
