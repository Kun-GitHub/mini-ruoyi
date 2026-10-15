// 有问题欢迎联系 hot_kun@hotmail.com
package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"mini-ruoyi/internal/domain"
)

// countEntriesFor 数一数调度器里属于某个任务的 entry 有几个。
//
// 直接读调度器而不是读 map:map 只是我们的账本,真正的执行来自调度器里
// 的 entry。账本说「一条」而调度器里存了两条,任务就会按双倍频率跑。
func (f *jobFixture) countEntriesFor(key string) int {
	f.t.Helper()

	f.svc.mu.Lock()
	id, ok := f.svc.entrys[key]
	f.svc.mu.Unlock()
	if !ok {
		return 0
	}

	n := 0
	for _, entry := range f.svc.sched.Entries() {
		if entry.ID == id {
			n++
		}
	}
	return n
}

// TestConcurrentJobUpdateKeepsBookkeepingConsistent 验证并发保存后的不变量。
//
// 要防的缺陷是「取消旧调度 + 添加新调度」不是一个整体：两个并发保存交错成
// 「A 取消 → B 取消 → A 添加 → B 添加」时，A 添加的那条 entry B 不知道，
// 调度器里就留下两份同一条任务，任务按双倍频率跑。修法是把这几步收进 schMu。
//
// 这个用例抓到过真缺陷：改成「先加新、后去旧」时忘了先取旧 id，
// 导致新 entry 还没人引用就被从账本里删掉，报出「调度器里共有 5 条 entry，期望 1」。
// 也就是说它能证伪「账本与调度器不一致」这一类错误——无论是多留残留，
// 还是任务凭空消失。
//
// 但它**不能可靠复现**最初那个并发交错：Go 在单机上把这 8 个写者排得比较开，
// 把 schMu 整把去掉后它依然通过（实测过）。交错本身由代码结构保证：
// 「取消 + 重排」是一个受锁保护的组合操作，不存在可插入的缝隙。
func TestConcurrentJobUpdateKeepsBookkeepingConsistent(t *testing.T) {
	f := newJobFixture(t, manualJob("test:concurrent", func(ctx context.Context) error { return nil }))

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = f.svc.Update(f.ctx, "test:concurrent", "0 3 * * *", "active", "并发保存")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个写者保存失败: %v", i, err)
		}
	}

	// 账本里只能有一条
	if n := len(f.svc.entrys); n != 1 {
		t.Errorf("账本里有 %d 条记录,期望 1", n)
	}
	// 账本上那条必须真的在调度器里存在
	if n := f.countEntriesFor("test:concurrent"); n != 1 {
		t.Errorf("调度器里有 %d 条 entry,期望 1", n)
	}
	// 调度器里不该有账本之外的残留
	if n := len(f.svc.sched.Entries()); n != 1 {
		t.Errorf("调度器里共有 %d 条 entry,期望 1——有保存留下的残留", n)
	}
}

// TestConcurrentJobUpdateKeepsRepoAndSchedulerInSync 守住「库与调度器说同一件事」。
//
// 只锁住重排本身是不够的：读旧值、重排、写库不在同一个临界区时，
// 两个并发保存可以交错成
//
//	A 读旧值 → B 读旧值 → A 重排 → B 重排 → B 写库 → A 写库
//
// 结果是库里写着 A 的表达式，而调度器里装的是 B——重启之前没人发现，
// 并且「保存成功」的提示是真的，只是生效的是另一次保存。
//
// 修法是把「读旧值→重排→写库」整段收进 schMu。这个用例断言的就那个后果：
// 并发写完之后，库里的记录与调度器里的表达式必须是同一个。
//
// 这个用例能复现它要防的错误：把 schMu 收窄成只锁重排、不盖住读旧值与写库，
// 10 轮里会稳定地挂上几轮，报出「库里的 cron 是 0 4、调度器里跑的是 0 2」。
// 注意它是概率性的——交错窗口很小，不是每轮都失败，所以别把「偶尔通过」
// 当成修好了，跑 -count 多轮才看得出来。
func TestConcurrentJobUpdateKeepsRepoAndSchedulerInSync(t *testing.T) {
	f := newJobFixture(t, manualJob("test:sync", func(ctx context.Context) error { return nil }))

	exprs := []string{"0 1 * * *", "0 2 * * *", "0 3 * * *", "0 4 * * *",
		"0 5 * * *", "0 6 * * *", "0 7 * * *", "0 8 * * *"}

	var wg sync.WaitGroup
	for _, expr := range exprs {
		wg.Add(1)
		go func(expr string) {
			defer wg.Done()
			// 并发下必然有一些保存失败或被覆盖，这里只关心最终不变量，
			// 单次调用的返回值不重要
			_ = f.svc.Update(f.ctx, "test:sync", expr, domain.StatusActive, "并发")
		}(expr)
	}
	wg.Wait()

	fromDB, err := f.repo.GetByKey(f.ctx, "test:sync")
	if err != nil {
		t.Fatalf("读库: %v", err)
	}

	f.svc.mu.Lock()
	id, ok := f.svc.entrys["test:sync"]
	f.svc.mu.Unlock()
	if !ok {
		t.Fatal("调度器里没有 test:sync 的 entry")
	}

	var inSched cron.Schedule
	for _, entry := range f.svc.sched.Entries() {
		if entry.ID == id {
			inSched = entry.Schedule
		}
	}
	if inSched == nil {
		t.Fatal("找到 entry 但读不到它的 Schedule")
	}

	/*
		断言“两边说的是同一个表达式”。

		不比对字符串：cron.Schedule 没有 String()，而且即使有，
		不同写法的等价表达式也会误报。改为拿库里的 cron 重新解析一份，
		再让两者从同一时刻算下一次执行时间：同一个表达式必然算出同一结果，
		不同的表达式在分钟级密集的样例下几乎不可能每次都撞上。
	*/
	fromRepo, err := cron.ParseStandard(fromDB.Cron)
	if err != nil {
		t.Fatalf("库里的 cron=%q 解析失败: %v", fromDB.Cron, err)
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	if got, want := inSched.Next(at), fromRepo.Next(at); !got.Equal(want) {
		t.Errorf("库里的 cron=%q 下次执行 %v,调度器里跑的是 %v——保存生效了一半",
			fromDB.Cron, want, got)
	}
}

// TestJobUpdateToInactiveRemovesSchedule 确认原子化没有改变原有语义:
// 停用的任务不该留在调度器里,重新启用要能回来。
func TestJobUpdateToInactiveRemovesSchedule(t *testing.T) {
	f := newJobFixture(t, manualJob("test:inactive", func(ctx context.Context) error { return nil }))

	if err := f.svc.Update(f.ctx, "test:inactive", "0 3 * * *", "inactive", ""); err != nil {
		t.Fatal(err)
	}
	if n := f.countEntriesFor("test:inactive"); n != 0 {
		t.Errorf("停用后调度器里还有 %d 条 entry,期望 0", n)
	}

	if err := f.svc.Update(f.ctx, "test:inactive", "0 4 * * *", "active", ""); err != nil {
		t.Fatal(err)
	}
	if n := f.countEntriesFor("test:inactive"); n != 1 {
		t.Errorf("重新启用后调度器里有 %d 条 entry,期望 1", n)
	}
}

// TestJobUpdateRejectsInvalidCronKeepsSchedule 确认非法表达式在写库之前就被拒:
// 否则调度被取消、任务静默失联,而库里还显示它是启用的。
func TestJobUpdateRejectsInvalidCronKeepsSchedule(t *testing.T) {
	f := newJobFixture(t, manualJob("test:badcron", func(ctx context.Context) error { return nil }))

	if err := f.svc.Update(f.ctx, "test:badcron", "不是 cron", "active", ""); err == nil {
		t.Fatal("非法 cron 应被拒绝")
	}
	if n := f.countEntriesFor("test:badcron"); n != 1 {
		t.Errorf("非法保存不该动到已有调度,实际 entry 数 = %d,期望 1", n)
	}
}
