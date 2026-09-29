-- 0012 定时任务执行日志
--
-- 0008 里写着「不建执行历史表：核心问题是『上次成功了吗』，job 行上存四个
-- 字段就能回答。要历史再加表」。现在补上：sys_jobs 上的四个字段只留最近
-- 一次，问「这个任务最近十次跑了多久」「昨晚那次为什么失败」时答不上来。
--
-- 与 sys_login_logs / sys_oper_logs 同一套路：只追加、不修改，所以
--   * 没有 updated_at —— 日志被改过就失去审计价值
--   * job_key 不设外键 —— 任务从代码里删掉后，它跑过的记录必须留下
--
-- 保留期由应用层控制（APP_LOG_RETENTION_DAYS，默认 30 天，由 cleanup:old_logs
-- 任务执行），因此需要 created_at 上的索引来支撑删除，也需要
-- (job_key, id) 来支撑「按任务翻历史」这一唯一的查询姿势。

CREATE TABLE sys_job_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    job_key    varchar(64) NOT NULL,

    -- cron = 调度器触发，manual = 界面上点了「立即执行」。
    -- 分开记是因为「这个任务怎么跑了两遍」的答案通常就在这一列
    trigger    varchar(16) NOT NULL CHECK (trigger IN ('cron', 'manual')),

    -- 与 sys_jobs.last_status 同一套取值；这里没有空串，
    -- 因为只有真正执行过（或被跳过）才会写这条记录
    status     varchar(16) NOT NULL CHECK (status IN ('success', 'failed', 'skipped')),

    -- 失败原因或跳过原因，成功时为空
    error      varchar(255) NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_job_logs_key     ON sys_job_logs (job_key, id);
CREATE INDEX idx_job_logs_created ON sys_job_logs (created_at);