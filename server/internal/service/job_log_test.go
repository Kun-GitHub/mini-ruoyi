package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/job"
	"mini-ruoyi/internal/repository"
)

// 这组用例盯的是「执行 → 落库」这条链路：sys_job_logs 里到底留下了什么，
// 以及它与 sys_jobs.last_* 是否始终说同一件事。
//
// 走真实库而不是 mock：要验证的正是事务、CHECK 约束、时间列这些只有
// 真 SQLite 才有的行为。

type jobFixture struct {
	t    *testing.T
	db   *sql.DB
	repo *repository.JobRepository
	svc  *JobService
	ctx  context.Context
}

func newJobFixture(t *testing.T, defs ...job.Job) *jobFixture {
	t.Helper()

	db, err := repository.NewDB(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := repository.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	registry := job.NewRegistry()
	for _, def := range defs {
		registry.Register(def)
	}

	repo := repository.NewJobRepository(db)
	svc := NewJobService(repo, registry)
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("启动定时任务: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(cctx)
	})

	return &jobFixture{t: t, db: db, repo: repo, svc: svc, ctx: ctx}
}

// manualJob 造一个 cron 永远不会触发的任务（凌晨 3 点），
// 于是用例里的每一条执行记录都必然来自「立即执行」。
func manualJob(key string, run func(ctx context.Context) error) job.Job {
	return job.Job{
		Key:            key,
		DescriptionKey: "job.cleanupOldLogs",
		DefaultCron:    "0 3 * * *",
		Run:            run,
	}
}

// waitForLogs 等日志攒够 want 条再返回。执行是异步的（RunNow 只是触发），
// 所以断言前必须先等，不能直接读一次就下结论。
func (f *jobFixture) waitForLogs(key string, want int64) []domain.JobLog {
	f.t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := f.svc.ListLogs(f.ctx, key, 1, 20)
		if err != nil {
			f.t.Fatalf("ListLogs: %v", err)
		}
		if page.Total >= want {
			return page.List
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("等任务 %s 的执行日志超时：只有 %d 条，期望至少 %d 条", key, page.Total, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestJobLogRecordsManualRun(t *testing.T) {
	var ran bool
	f := newJobFixture(t, manualJob("test:ok", func(context.Context) error {
		ran = true
		return nil
	}))

	if err := f.svc.RunNow(f.ctx, "test:ok"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}

	logs := f.waitForLogs("test:ok", 1)
	if !ran {
		t.Fatal("记了日志但任务本体没跑")
	}

	l := logs[0]
	if l.JobKey != "test:ok" {
		t.Errorf("job_key = %q，期望 test:ok", l.JobKey)
	}
	if l.Trigger != domain.JobTriggerManual {
		t.Errorf("trigger = %q，期望 manual（这是界面上点的）", l.Trigger)
	}
	if l.Status != domain.JobStatusSuccess {
		t.Errorf("status = %q，期望 success", l.Status)
	}
	if l.Error != "" {
		t.Errorf("成功时不该有错误信息，实际 %q", l.Error)
	}
	if l.CreatedAt.IsZero() {
		t.Error("created_at 是零值——界面上的「执行时间」会显示成 0001 年")
	}

	// 「最近一次」与历史必须是同一次写库留下的，否则列表页和日志页会各说各话
	item, err := f.repo.GetByKey(f.ctx, "test:ok")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if item.LastStatus != domain.JobStatusSuccess {
		t.Errorf("sys_jobs.last_status = %q，期望 success", item.LastStatus)
	}
	if item.LastRunAt == nil {
		t.Fatal("sys_jobs.last_run_at 还是空的")
	}
	if !item.LastRunAt.Equal(l.CreatedAt) {
		t.Errorf("last_run_at (%v) 与日志里的时间 (%v) 不是同一时刻",
			item.LastRunAt, l.CreatedAt)
	}
}

func TestJobLogRecordsFailedRun(t *testing.T) {
	f := newJobFixture(t, manualJob("test:fail", func(context.Context) error {
		return errors.New("磁盘满了")
	}))

	if err := f.svc.RunNow(f.ctx, "test:fail"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}

	logs := f.waitForLogs("test:fail", 1)
	if logs[0].Status != domain.JobStatusFailed {
		t.Fatalf("status = %q，期望 failed", logs[0].Status)
	}
	// 失败原因必须落在历史里：否则「昨晚那次为什么失败」还得去翻进程日志
	if logs[0].Error != "磁盘满了" {
		t.Errorf("error = %q，期望把任务的报错原样记下来", logs[0].Error)
	}

	item, err := f.repo.GetByKey(f.ctx, "test:fail")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if item.LastStatus != domain.JobStatusFailed || item.LastError != "磁盘满了" {
		t.Errorf("最近一次 = {%q, %q}，期望 {failed, 磁盘满了}", item.LastStatus, item.LastError)
	}
}

func TestJobLogRecordsSkippedRun(t *testing.T) {
	// 缓冲通道：任务可能被执行不止一次，用 close 会 panic
	started := make(chan struct{}, 4)
	release := make(chan struct{})

	f := newJobFixture(t, manualJob("test:slow", func(ctx context.Context) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))

	// 第一次：跑起来并且卡住不放
	if err := f.svc.RunNow(f.ctx, "test:slow"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	<-started

	// 第二次：此时锁还在，应当立刻记一条「已跳过」，而不是排队等下去
	if err := f.svc.RunNow(f.ctx, "test:slow"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	// 等到跳过那条真的落库再放行第一次——否则顺序就成了赌注
	skipped := f.waitForLogs("test:slow", 1)
	if skipped[0].Status != domain.JobStatusSkipped {
		t.Fatalf("先落库的应当是跳过记录，实际 status = %q", skipped[0].Status)
	}
	close(release)

	logs := f.waitForLogs("test:slow", 2)
	// 按 id 倒序：最后写入的是第一次执行的成功记录
	if logs[0].Status != domain.JobStatusSuccess {
		t.Errorf("最新一条 status = %q，期望 success", logs[0].Status)
	}
	if logs[1].Status != domain.JobStatusSkipped {
		t.Errorf("第二条 status = %q，期望 skipped", logs[1].Status)
	}
	// 跳过也要写原因，否则「为什么今天没跑」又变回靠猜
	if logs[1].Error == "" {
		t.Error("跳过记录没有写原因")
	}

	// 被跳过的那次不该污染「最近一次」：最近一次是那次真正跑完的
	item, err := f.repo.GetByKey(f.ctx, "test:slow")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if item.LastStatus != domain.JobStatusSuccess {
		t.Errorf("last_status = %q，期望 success（跳过发生在先，成功在后）", item.LastStatus)
	}
}

func TestJobLogRecordsCronTrigger(t *testing.T) {
	f := newJobFixture(t, manualJob("test:cron", func(context.Context) error { return nil }))

	// 直接调用调度器注册的那个函数：等真到点要一分钟，用例跑不动。
	// 验的正是「调度器发起的执行被记成 cron」这条路径。
	entries := f.svc.sched.Entries()
	if len(entries) != 1 {
		t.Fatalf("调度器里有 %d 个条目，期望 1 个", len(entries))
	}
	entries[0].WrappedJob.Run()

	logs := f.waitForLogs("test:cron", 1)
	if logs[0].Trigger != domain.JobTriggerCron {
		t.Errorf("trigger = %q，期望 cron（这是调度器发起的）", logs[0].Trigger)
	}
}

func TestJobLogWriteIsAtomic(t *testing.T) {
	f := newJobFixture(t, manualJob("test:tx", func(context.Context) error { return nil }))

	if err := f.svc.RunNow(f.ctx, "test:tx"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	f.waitForLogs("test:tx", 1)

	// 让第二条写失败：非法的触发方式会撞上 CHECK 约束。
	// 两条写如果不在同一个事务里，UPDATE 就已经生效了——那就留下
	// 「列表说刚失败、历史里查不到」的两份现实。
	err := f.repo.RecordRun(f.ctx, "test:tx", "bogus", domain.JobStatusFailed, "boom", time.Now(), 5)
	if err == nil {
		t.Fatal("非法触发方式应当被 CHECK 约束拦住")
	}

	item, err := f.repo.GetByKey(f.ctx, "test:tx")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if item.LastStatus != domain.JobStatusSuccess || item.LastError != "" {
		t.Errorf("事务没回滚：最近一次变成了 {%q, %q}", item.LastStatus, item.LastError)
	}

	page, err := f.svc.ListLogs(f.ctx, "test:tx", 1, 20)
	if err != nil {
		t.Fatalf("ListLogs: %v", err)
	}
	if page.Total != 1 {
		t.Errorf("回滚后历史应当仍是 1 条，实际 %d 条", page.Total)
	}
}

func TestJobLogsAreCleanedUpByRetention(t *testing.T) {
	f := newJobFixture(t, manualJob("test:keep", func(context.Context) error { return nil }))

	// 一条 40 天前的 + 一条刚刚的。保留期是 30 天，只有前者该被清掉。
	if err := f.repo.RecordRun(f.ctx, "test:keep", domain.JobTriggerCron,
		domain.JobStatusSuccess, "", time.Now().AddDate(0, 0, -40), 5); err != nil {
		t.Fatalf("写入过期记录: %v", err)
	}
	if err := f.repo.RecordRun(f.ctx, "test:keep", domain.JobTriggerCron,
		domain.JobStatusSuccess, "", time.Now(), 5); err != nil {
		t.Fatalf("写入新记录: %v", err)
	}

	// 走既有的清理通道：任务执行日志共用 APP_LOG_RETENTION_DAYS，
	// 不该为它再配一个保留期（多一处设置就多一处会漂移的地方）
	logs := NewLogService(repository.NewLogRepository(f.db), 30)
	n, err := logs.CleanupExpired(f.ctx)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("清理了 %d 条，期望只清掉过期的 1 条", n)
	}

	page, err := f.svc.ListLogs(f.ctx, "test:keep", 1, 20)
	if err != nil {
		t.Fatalf("ListLogs: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("清理后应当剩 1 条，实际 %d 条", page.Total)
	}
	if time.Since(page.List[0].CreatedAt) > 24*time.Hour {
		t.Error("剩下的是过期那条——清理把新记录也删了")
	}
}
