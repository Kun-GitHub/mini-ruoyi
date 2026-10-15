// 有问题欢迎联系 hot_kun@hotmail.com
package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"mini-ruoyi/internal/domain"
	"mini-ruoyi/internal/job"
	"mini-ruoyi/internal/repository"
)

const (
	// jobTimeout 是单次执行的超时。没有它，一个卡住的任务会永久占着 goroutine。
	jobTimeout = 5 * time.Minute
	// jobErrorLimit 对应 sys_jobs.last_error 的声明宽度。
	jobErrorLimit = 255
)

// JobService 负责调度、执行与记录定时任务。
type JobService struct {
	repo     *repository.JobRepository
	registry *job.Registry
	sched    *cron.Cron

	// 每个任务一把锁，用 TryLock 实现「上一次没跑完就跳过」。
	//
	// 不用 cron 自带的 SkipIfStillRunning 链，是为了能**记录**「被跳过」
	// 这个事实——否则「为什么今天没跑」只能靠猜。
	locks map[string]*sync.Mutex

	// mu 保护 entrys，schMu 保护「取消旧调度 + 登记新调度」这个组合。
	//
	// 分成两把锁是因为两者要盖的范围不同：只保护 entrys 的读写，
	// 两个并发 Update 仍然会各自删掉对方的 entry、再各自 add 一个，
	// 结果就是同一条 cron 在调度器里存在两份，任务按两倍频率跑。
	mu     sync.Mutex
	entrys map[string]cron.EntryID
	schMu  sync.Mutex
}

func NewJobService(repo *repository.JobRepository, registry *job.Registry) *JobService {
	return &JobService{
		repo:     repo,
		registry: registry,
		// 用服务器本地时区解析 cron：「每天凌晨 3 点」是运维的直觉，
		// 按 UTC 解释会差好几个小时。执行时间落库时仍是 UTC。
		//
		// 不加 WithSeconds()：那是 6 段表达式（若依/Spring 那种）。
		// 标准 5 段 cron 更通用，粘贴现成的表达式不会踩坑。
		sched:  cron.New(cron.WithLocation(time.Local)),
		locks:  map[string]*sync.Mutex{},
		entrys: map[string]cron.EntryID{},
	}
}

// Start 为代码注册的任务补齐数据库记录、校验、然后启动调度。
func (s *JobService) Start(ctx context.Context) error {
	// 1) 按注册表补记录。DO NOTHING，不覆盖用户改过的参数。
	for _, j := range s.registry.All() {
		if err := s.repo.Ensure(ctx, j.Key, j.DefaultCron); err != nil {
			return fmt.Errorf("初始化任务 %s: %w", j.Key, err)
		}
		s.locks[j.Key] = &sync.Mutex{}
	}

	// 2) 库里多出来的 key 只告警，不拒绝启动。
	//    与权限码的处理相反：一条永远不会跑的任务记录是无害的，
	//    而一条无效的权限码会让授权静默失效。
	if keys, err := s.repo.Keys(ctx); err != nil {
		return fmt.Errorf("读取任务列表: %w", err)
	} else if unknown := s.registry.Unknown(keys); len(unknown) > 0 {
		log.Printf("数据库里存在代码未注册的任务，它们不会被执行: %v", unknown)
	}

	// 3) 调度
	scheduled, err := s.rescheduleAll(ctx)
	if err != nil {
		return err
	}
	s.sched.Start()
	log.Printf("定时任务已启动：%d 个已调度", scheduled)
	return nil
}

// Stop 停止调度并等待正在执行的任务结束。
func (s *JobService) Stop(ctx context.Context) error {
	done := s.sched.Stop()
	select {
	case <-done.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// List 返回全部已注册任务的状态。
//
// 以**代码注册表**为准而不是以数据库为准：数据库里可能残留已从代码删除的
// 任务记录，那些不该出现在界面上（它们永远不会被执行）。
func (s *JobService) List(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	byKey := make(map[string]domain.Job, len(rows))
	for _, r := range rows {
		byKey[r.Key] = r
	}

	next := s.nextRuns()

	out := make([]domain.Job, 0, len(s.registry.All()))
	for _, def := range s.registry.All() {
		item, ok := byKey[def.Key]
		if !ok {
			// 不该发生：Start 会补齐。真出现了说明有人手工删了行。
			item = domain.Job{Key: def.Key, Cron: def.DefaultCron, Status: domain.StatusActive}
		}
		item.DescriptionKey = def.DescriptionKey
		item.DefaultCron = def.DefaultCron
		if t, ok := next[def.Key]; ok {
			item.NextRunAt = &t
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *JobService) Get(ctx context.Context, key string) (domain.Job, error) {
	def, ok := s.registry.Get(key)
	if !ok {
		return domain.Job{}, domain.ErrNotFound
	}
	item, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		return domain.Job{}, err
	}
	item.DescriptionKey = def.DescriptionKey
	item.DefaultCron = def.DefaultCron
	if t, ok := s.nextRuns()[key]; ok {
		item.NextRunAt = &t
	}
	return item, nil
}

// Update 改 cron 表达式、开关与备注，并**立即重新调度**。
//
// 改完不重新调度的话，这个功能就没有意义——用户会以为改了，实际还在按旧的跑。
func (s *JobService) Update(ctx context.Context, key, expr, status, remark string) error {
	if _, ok := s.registry.Get(key); !ok {
		return domain.ErrNotFound
	}
	if status != domain.StatusActive && status != domain.StatusInactive {
		return fmt.Errorf("任务 %s 的状态 %q 非法: %w", key, status, domain.ErrInvalidJobCron)
	}
	// 先解析：非法表达式必须在这里被拒，而不是等到重新调度时静默失败
	if _, err := cron.ParseStandard(expr); err != nil {
		return fmt.Errorf("任务 %s 的 cron 表达式 %q 非法: %w", key, expr, domain.ErrInvalidJobCron)
	}

	// 从这里开始整段持 schMu：读旧值、重排、写库、失败回滚必须是一个整体。
	//
	// 只把重排本身锁住是不够的。两个并发保存仍然可以这样交错：
	//
	//	A 读旧值 → B 读旧值 → A 重排（调度器=A）→ B 重排（调度器=B）
	//	→ B 写库（库=B）→ A 写库（库=A）
	//
	// 结果是库里写着 A、而调度器实际按 B 在跑，重启后才会莫名其妙地变回来。
	// 把「读旧值→写库」整段收进同一把锁，这类交错就不存在了。
	//
	// 临界区内含两次数据库 IO，会让同时保存不同任务也变成串行。
	// 这是故意的：保存任务是人工操作、频率极低，换的是“库与调度器永远说同一件事”。
	s.schMu.Lock()
	defer s.schMu.Unlock()

	previous, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		return fmt.Errorf("读取任务 %s 原配置: %w", key, err)
	}

	next := domain.Job{Key: key, Cron: expr, Status: status, Remark: remark}
	if err := s.rescheduleLocked(next); err != nil {
		return err
	}
	if err := s.repo.Update(ctx, key, expr, status, remark); err != nil {
		// 库没改成，就把调度器退回去，不留下“库里旧的、调度器新的”这种分裂
		if restoreErr := s.rescheduleLocked(previous); restoreErr != nil {
			log.Printf("恢复任务 %s 的调度失败（库未更新，调度器现在是新配置）: %v", key, restoreErr)
		}
		return fmt.Errorf("更新任务 %s: %w", key, err)
	}
	return nil
}

// RunNow 立即执行一次。异步：任务的执行时间不可控，
// 同步等待会让 HTTP 请求挂在那里最多 5 分钟。
//
// 返回值只表示「已触发」。执行结果要刷新列表看。
func (s *JobService) RunNow(ctx context.Context, key string) error {
	if _, ok := s.registry.Get(key); !ok {
		return domain.ErrNotFound
	}
	if _, err := s.repo.GetByKey(ctx, key); err != nil {
		return err
	}
	// 标记为手动触发：把「定时跑的」和「人点的」区分开，
	// 才答得了「这个任务怎么一天跑了好几遍」
	go s.execute(context.Background(), key, domain.JobTriggerManual)
	return nil
}

// JobLogPage 与其它列表接口结构一致，前端分页逻辑可复用。
type JobLogPage = Page[domain.JobLog]

// ListLogs 翻某个任务的执行历史。
//
// 未注册的 key 返回 404 而不是空列表：与 List / Get 一样，
// 清单以代码注册表为准，库里残留的已删任务不该在界面上留一个入口。
func (s *JobService) ListLogs(ctx context.Context, key string, page, pageSize int) (JobLogPage, error) {
	if _, ok := s.registry.Get(key); !ok {
		return JobLogPage{}, domain.ErrNotFound
	}

	page, pageSize = normalizePage(page, pageSize)

	total, err := s.repo.CountRunLogs(ctx, key)
	if err != nil {
		return JobLogPage{}, fmt.Errorf("count job logs: %w", err)
	}
	page, offset := clampPage(page, pageSize, total)
	list, err := s.repo.ListRunLogs(ctx, key, pageSize, offset)
	if err != nil {
		return JobLogPage{}, fmt.Errorf("list job logs: %w", err)
	}
	return JobLogPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// rescheduleAll 按数据库里的状态重建全部调度。
func (s *JobService) rescheduleAll(ctx context.Context) (int, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("list jobs: %w", err)
	}

	count := 0
	for _, item := range rows {
		if !item.IsActive() {
			continue
		}
		if _, ok := s.registry.Get(item.Key); !ok {
			continue // 已从代码删除的任务，跳过
		}
		if _, err := s.schedule(item); err != nil {
			// 单条任务的表达式坏了不该拖垮整个启动：记录下来，其余照常调度。
			// 用户能在界面上看到它没有下次执行时间，从而发现并修正。
			log.Printf("任务 %s 调度失败（cron=%q）: %v", item.Key, item.Cron, err)
			continue
		}
		count++
	}
	return count, nil
}

// rescheduleLocked 按给定配置重建一个任务的调度。调用方必须持有 schMu。
//
// 这几步必须是一个整体：一旦两个 Update 并发执行，
// 「A 取消 → B 取消 → A 添加 → B 添加」这样的交错会让同一个任务
// 在调度器里留下两个 entry（A 添加的那个 B 不知道），任务按双倍频率触发。
// 把窗口收窄到一次锁内，就不存在能交错的缝隙。
//
// item 由调用方传入而不是从库里读：并发保存时，回读到的可能是别人刚写的值，
// 于是本次请求“保存成功”但调度器里装的是另一个人的表达式。
func (s *JobService) rescheduleLocked(item domain.Job) error {
	if !item.IsActive() {
		// 停用：没什么可加的，直接拿掉旧 entry。
		s.unscheduleLocked(item.Key)
		return nil
	}

	// 先加新的、成功了再拿掉旧的。
	//
	// 反过来（先取消、后添加）的话，一旦 AddFunc 失败，任务就停在那里、
	// 既不跑也没有下次执行时间——而库里的记录明明是启用的。
	// 这个顺序下最坏只是在极端情况下多留一个旧 entry，
	// 由下一次保存清掉；比「任务静静地死掉」好。
	//
	// 注意旧 entry 的 id 必须在 scheduleLocked 之前取：那个函数会把
	// entrys[key] 改写为新 id，再取就只能拿到新的。
	old, hadOld := s.entrysOf(item.Key)
	if _, err := s.scheduleLocked(item); err != nil {
		return err
	}
	if hadOld {
		s.sched.Remove(old)
	}
	return nil
}

// entrysOf 读一份当前 entry 快照。
func (s *JobService) entrysOf(key string) (cron.EntryID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.entrys[key]
	return id, ok
}

func (s *JobService) schedule(item domain.Job) (cron.EntryID, error) {
	s.schMu.Lock()
	defer s.schMu.Unlock()
	return s.scheduleLocked(item)
}

func (s *JobService) scheduleLocked(item domain.Job) (cron.EntryID, error) {
	key := item.Key
	id, err := s.sched.AddFunc(item.Cron, func() { s.execute(context.Background(), key, domain.JobTriggerCron) })
	if err != nil {
		return 0, fmt.Errorf("添加调度 %s（cron=%q）: %w", key, item.Cron, err)
	}
	s.mu.Lock()
	s.entrys[key] = id
	s.mu.Unlock()
	return id, nil
}

func (s *JobService) unschedule(key string) {
	s.schMu.Lock()
	defer s.schMu.Unlock()
	s.unscheduleLocked(key)
}

func (s *JobService) unscheduleLocked(key string) {
	s.mu.Lock()
	id, ok := s.entrys[key]
	delete(s.entrys, key)
	s.mu.Unlock()
	if ok {
		s.sched.Remove(id)
	}
}

// nextRuns 从调度器读每个任务的下次执行时间。
func (s *JobService) nextRuns() map[string]time.Time {
	out := map[string]time.Time{}

	s.mu.Lock()
	ids := make(map[string]cron.EntryID, len(s.entrys))
	for k, v := range s.entrys {
		ids[k] = v
	}
	s.mu.Unlock()

	for _, entry := range s.sched.Entries() {
		for key, id := range ids {
			if entry.ID == id && !entry.Next.IsZero() {
				out[key] = entry.Next
			}
		}
	}
	return out
}

// execute 执行一次任务并记录结果。
//
// 用 TryLock 而不是阻塞：上一次还没跑完时直接跳过并记录。
// 不这么做的话，一个耗时超过间隔的任务会不断堆积，把 1G 的内存吃光。
func (s *JobService) execute(ctx context.Context, key, trigger string) {
	def, ok := s.registry.Get(key)
	if !ok {
		return
	}

	lock, ok := s.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		s.mu.Lock()
		s.locks[key] = lock
		s.mu.Unlock()
	}

	if !lock.TryLock() {
		log.Printf("任务 %s 上一次还没跑完，本次跳过", key)
		s.record(context.Background(), key, trigger, domain.JobStatusSkipped, "上一次尚未结束", time.Now(), 0)
		return
	}
	defer lock.Unlock()

	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()

	err := def.Run(runCtx)
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("任务 %s 执行失败（%s）: %v", key, elapsed.Round(time.Millisecond), err)
		s.record(ctx, key, trigger, domain.JobStatusFailed, truncateBytes(err.Error(), jobErrorLimit), start, int(elapsed.Milliseconds()))
		return
	}
	log.Printf("任务 %s 执行成功（%s）", key, elapsed.Round(time.Millisecond))
	s.record(ctx, key, trigger, domain.JobStatusSuccess, "", start, int(elapsed.Milliseconds()))
}

func (s *JobService) record(ctx context.Context, key, trigger, status, errMsg string, ranAt time.Time, ms int) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.repo.RecordRun(ctx, key, trigger, status, errMsg, ranAt, ms); err != nil {
		// 记录失败不影响任务本身，但不能静默——否则界面上会一直显示旧结果
		log.Printf("记录任务 %s 的执行结果失败: %v", key, err)
	}
}
