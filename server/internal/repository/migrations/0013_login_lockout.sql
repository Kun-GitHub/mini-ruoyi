-- 登录失败锁定：连续失败达到阈值后，在一段时间内拒绝该账号的登录尝试。
--
-- 状态落在**用户行**上，而不是单独建一张逐次记录的失败表：
-- 要回答的问题只有「这个账号现在能不能登录」，两个列就够了。
-- 逐条记录的表除了要多养一个清理任务，还会让「哪几次算连续」变成查询逻辑，
-- 而那套逻辑每次登录都要跑一遍。
--
-- failed_count 是连续失败次数（成功登录即清零）；
-- locked_until 为 NULL 表示未锁定，到点后由下一次登录尝试自动解锁并清零计数。
ALTER TABLE sys_users ADD COLUMN failed_count INTEGER NOT NULL DEFAULT 0;

-- 列类型必须写 datetime 而不是 text：驱动只把声明为 datetime/date/timestamp 的
-- 列解析成 time.Time，写 text 的话 Scan 到 *time.Time 直接报错
-- （0002_init_rbac.sql 的 login_at 同理）。
ALTER TABLE sys_users ADD COLUMN locked_until datetime NULL;
